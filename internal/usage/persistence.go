package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// SnapshotVersion identifies the on-disk and export/import JSON schema version.
	SnapshotVersion = 2

	defaultPersistenceFlushInterval = 30 * time.Second
)

var ErrUnsupportedSnapshotVersion = errors.New("unsupported version")

// ExportPayload is the canonical serialized usage snapshot format.
type ExportPayload struct {
	Version    int                `json:"version"`
	ExportedAt time.Time          `json:"exported_at"`
	Usage      StatisticsSnapshot `json:"usage"`
}

// ImportPayload accepts previously exported usage snapshots.
type ImportPayload struct {
	Version int                `json:"version"`
	Usage   StatisticsSnapshot `json:"usage"`
}

// NewExportPayload builds a fresh snapshot payload for API responses or local persistence.
func NewExportPayload(snapshot StatisticsSnapshot) ExportPayload {
	return ExportPayload{
		Version:    SnapshotVersion,
		ExportedAt: time.Now().UTC(),
		Usage:      snapshot,
	}
}

// ParseImportPayload validates a usage snapshot import payload.
func ParseImportPayload(data []byte) (ImportPayload, error) {
	var payload ImportPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return ImportPayload{}, err
	}
	if payload.Version != 0 && payload.Version != 1 && payload.Version != SnapshotVersion {
		return ImportPayload{}, fmt.Errorf("%w: %d", ErrUnsupportedSnapshotVersion, payload.Version)
	}
	return payload, nil
}

// PersistenceManager periodically flushes the in-memory usage snapshot to a JSON file.
type PersistenceManager struct {
	stats    *RequestStatistics
	interval time.Duration

	pathMu   sync.RWMutex
	filePath string

	changeSeq atomic.Uint64
	savedSeq  atomic.Uint64
	savedRev  atomic.Uint64
	started   atomic.Bool

	flushMu  sync.Mutex
	statusMu sync.RWMutex
	status   PersistenceRuntimeStatus
}

type PersistenceRuntimeStatus struct {
	LastFlushAt     time.Time `json:"last_flush_at"`
	LastLoadAt      time.Time `json:"last_load_at"`
	LastLoadAdded   int64     `json:"last_load_added"`
	LastLoadSkipped int64     `json:"last_load_skipped"`
	LastError       string    `json:"last_error"`
	LastErrorAt     time.Time `json:"last_error_at"`
}

// NewPersistenceManager creates a new snapshot persistence manager.
func NewPersistenceManager(stats *RequestStatistics, filePath string) *PersistenceManager {
	return &PersistenceManager{
		stats:     stats,
		interval:  defaultPersistenceFlushInterval,
		filePath:  strings.TrimSpace(filePath),
		flushMu:   sync.Mutex{},
		changeSeq: atomic.Uint64{},
		savedSeq:  atomic.Uint64{},
		started:   atomic.Bool{},
	}
}

func (m *PersistenceManager) Status() PersistenceRuntimeStatus {
	if m == nil {
		return PersistenceRuntimeStatus{}
	}
	m.statusMu.RLock()
	defer m.statusMu.RUnlock()
	return m.status
}

func (m *PersistenceManager) setLastError(err error) {
	if m == nil {
		return
	}
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	if err == nil {
		m.status.LastError = ""
		m.status.LastErrorAt = time.Time{}
		return
	}
	m.status.LastError = err.Error()
	m.status.LastErrorAt = time.Now().UTC()
}

// Enabled reports whether snapshot persistence has a writable target path.
func (m *PersistenceManager) Enabled() bool {
	if m == nil {
		return false
	}
	m.pathMu.RLock()
	defer m.pathMu.RUnlock()
	return strings.TrimSpace(m.filePath) != ""
}

// FilePath returns the current persistence target file path.
func (m *PersistenceManager) FilePath() string {
	if m == nil {
		return ""
	}
	m.pathMu.RLock()
	defer m.pathMu.RUnlock()
	return m.filePath
}

// SetFilePath updates the persistence target file path. Empty disables disk writes.
func (m *PersistenceManager) SetFilePath(path string) {
	if m == nil {
		return
	}
	path = strings.TrimSpace(path)
	m.pathMu.Lock()
	defer m.pathMu.Unlock()
	if m.filePath != path {
		m.changeSeq.Add(1)
	}
	m.filePath = path
}

// MarkDirty records that the in-memory snapshot changed and should be flushed again.
func (m *PersistenceManager) MarkDirty() {
	if m == nil || !m.Enabled() {
		return
	}
	m.changeSeq.Add(1)
}

// Start launches the background flush loop. Calling Start multiple times is safe.
func (m *PersistenceManager) Start(ctx context.Context) {
	if m == nil || !m.Enabled() {
		return
	}
	if !m.started.CompareAndSwap(false, true) {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go m.run(ctx)
}

func (m *PersistenceManager) run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = m.Flush()
			return
		case <-ticker.C:
			_ = m.Flush()
		}
	}
}

// Load restores a persisted usage snapshot into the current in-memory statistics store.
func (m *PersistenceManager) Load() (MergeResult, error) {
	result := MergeResult{}
	if m == nil || m.stats == nil || !m.Enabled() {
		return result, nil
	}

	path := m.FilePath()
	if path == "" {
		return result, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return result, nil
		}
		m.setLastError(err)
		return result, err
	}

	payload, err := ParseImportPayload(data)
	if err != nil {
		m.setLastError(err)
		return result, err
	}

	result = m.stats.mergeSnapshot(payload.Usage, false)
	_, revision := m.stats.SnapshotWithRevision()
	m.savedRev.Store(revision)
	m.savedSeq.Store(m.changeSeq.Load())
	m.statusMu.Lock()
	m.status.LastLoadAt = time.Now().UTC()
	m.status.LastLoadAdded = result.Added
	m.status.LastLoadSkipped = result.Skipped
	m.status.LastError = ""
	m.status.LastErrorAt = time.Time{}
	m.statusMu.Unlock()
	return result, nil
}

// Flush writes the current in-memory snapshot to disk when changes are pending.
func (m *PersistenceManager) Flush() error {
	if m == nil || m.stats == nil || !m.Enabled() {
		return nil
	}

	m.flushMu.Lock()
	defer m.flushMu.Unlock()

	targetSeq := m.changeSeq.Load()
	path := m.FilePath()
	if path == "" {
		return nil
	}

	snapshot, targetRevision := m.stats.SnapshotWithRevision()
	if targetSeq == m.savedSeq.Load() && targetRevision == m.savedRev.Load() {
		if !snapshotHasData(snapshot) {
			return nil
		}
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	payload := NewExportPayload(snapshot)
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		m.setLastError(err)
		return err
	}
	if err = atomicWriteFile(path, data); err != nil {
		m.setLastError(err)
		return err
	}
	m.savedSeq.Store(targetSeq)
	m.savedRev.Store(targetRevision)
	m.statusMu.Lock()
	m.status.LastFlushAt = time.Now().UTC()
	m.status.LastError = ""
	m.status.LastErrorAt = time.Time{}
	m.statusMu.Unlock()
	return nil
}

func snapshotHasData(snapshot StatisticsSnapshot) bool {
	return snapshot.TotalRequests > 0 ||
		snapshot.SuccessCount > 0 ||
		snapshot.FailureCount > 0 ||
		snapshot.TotalTokens > 0 ||
		len(snapshot.APIs) > 0 ||
		len(snapshot.RequestsByDay) > 0 ||
		len(snapshot.RequestsByHour) > 0 ||
		len(snapshot.TokensByDay) > 0 ||
		len(snapshot.TokensByHour) > 0
}

func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, "usage-*.json")
	if err != nil {
		return err
	}

	tmpName := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err = tmpFile.Write(data); err != nil {
		return err
	}
	if err = tmpFile.Chmod(0o600); err != nil {
		return err
	}
	if err = tmpFile.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}

var defaultPersistenceManager = NewPersistenceManager(defaultRequestStatistics, "")

// DefaultPersistenceManager returns the shared usage persistence manager.
func DefaultPersistenceManager() *PersistenceManager { return defaultPersistenceManager }

// SetDefaultPersistencePath updates the snapshot file used by the shared usage statistics store.
func SetDefaultPersistencePath(path string) { defaultPersistenceManager.SetFilePath(path) }

// StartDefaultPersistence starts the shared background flush loop.
func StartDefaultPersistence(ctx context.Context) { defaultPersistenceManager.Start(ctx) }

// LoadDefaultPersistence restores the shared usage statistics snapshot from disk.
func LoadDefaultPersistence() (MergeResult, error) { return defaultPersistenceManager.Load() }

// FlushDefaultPersistence forces an immediate flush of the shared usage statistics snapshot.
func FlushDefaultPersistence() error { return defaultPersistenceManager.Flush() }

// MarkDefaultPersistenceDirty marks the shared usage statistics snapshot as changed.
func MarkDefaultPersistenceDirty() { defaultPersistenceManager.MarkDirty() }
