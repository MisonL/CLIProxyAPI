package usage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestParseImportPayloadAcceptsLegacyV1Snapshot(t *testing.T) {
	data := []byte(`{"version":1,"usage":{}}`)

	payload, err := ParseImportPayload(data)
	if err != nil {
		t.Fatalf("ParseImportPayload() error = %v", err)
	}
	if payload.Version != 1 {
		t.Fatalf("payload.Version = %d, want 1", payload.Version)
	}
}

func TestParseImportPayloadRejectsUnsupportedVersion(t *testing.T) {
	data := []byte(`{"version":99,"usage":{}}`)

	_, err := ParseImportPayload(data)
	if !errors.Is(err, ErrUnsupportedSnapshotVersion) {
		t.Fatalf("ParseImportPayload() error = %v, want ErrUnsupportedSnapshotVersion", err)
	}
}

func TestPersistenceManagerLoadLegacyV1Snapshot(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "usage-statistics.json")
	data := []byte(`{
  "version": 1,
  "usage": {
    "apis": {
      "legacy-key": {
        "models": {
          "gpt-5": {
            "details": [
              {
                "timestamp": "2026-03-07T21:00:00Z",
                "source": "legacy",
                "auth_index": "legacy-1",
                "tokens": {
                  "input_tokens": 2,
                  "output_tokens": 3,
                  "total_tokens": 5
                },
                "failed": false
              }
            ]
          }
        }
      }
    }
  }
}`)
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	stats := NewRequestStatistics()
	manager := NewPersistenceManager(stats, filePath)
	result, err := manager.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("Load() added = %d, want 1", result.Added)
	}
	detail := stats.Snapshot().APIs["legacy-key"].Models["gpt-5"].Details[0]
	if detail.SelectionKey != "legacy-1" {
		t.Fatalf("SelectionKey = %q, want %q", detail.SelectionKey, "legacy-1")
	}
}

func TestPersistenceManagerFlushAndLoad(t *testing.T) {
	backupDir := t.TempDir()
	filePath := filepath.Join(backupDir, "usage-statistics.json")

	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		Provider:    "codex",
		Model:       "gpt-5",
		APIKey:      "test-key",
		RequestedAt: time.Date(2026, 3, 7, 21, 0, 0, 0, time.UTC),
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	})

	manager := NewPersistenceManager(stats, filePath)
	if err := manager.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	flushStatus := manager.Status()
	if flushStatus.LastFlushAt.IsZero() {
		t.Fatalf("LastFlushAt is zero, want populated")
	}
	if flushStatus.LastError != "" {
		t.Fatalf("LastError = %q, want empty", flushStatus.LastError)
	}

	if _, err := os.Stat(manager.FilePath()); err != nil {
		t.Fatalf("expected snapshot file to exist: %v", err)
	}

	restoredStats := NewRequestStatistics()
	restoredManager := NewPersistenceManager(restoredStats, filePath)
	result, err := restoredManager.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if result.Added != 1 {
		t.Fatalf("Load() added = %d, want 1", result.Added)
	}
	loadStatus := restoredManager.Status()
	if loadStatus.LastLoadAt.IsZero() {
		t.Fatalf("LastLoadAt is zero, want populated")
	}
	if loadStatus.LastLoadAdded != 1 {
		t.Fatalf("LastLoadAdded = %d, want 1", loadStatus.LastLoadAdded)
	}
	if loadStatus.LastLoadSkipped != 0 {
		t.Fatalf("LastLoadSkipped = %d, want 0", loadStatus.LastLoadSkipped)
	}
	if loadStatus.LastError != "" {
		t.Fatalf("LastError = %q, want empty", loadStatus.LastError)
	}

	snapshot := restoredStats.Snapshot()
	if snapshot.TotalRequests != 1 {
		t.Fatalf("TotalRequests = %d, want 1", snapshot.TotalRequests)
	}
	if snapshot.TotalTokens != 15 {
		t.Fatalf("TotalTokens = %d, want 15", snapshot.TotalTokens)
	}

	apiSnapshot, ok := snapshot.APIs["test-key"]
	if !ok {
		t.Fatalf("expected API snapshot for test-key")
	}
	modelSnapshot, ok := apiSnapshot.Models["gpt-5"]
	if !ok {
		t.Fatalf("expected model snapshot for gpt-5")
	}
	if len(modelSnapshot.Details) != 1 {
		t.Fatalf("details length = %d, want 1", len(modelSnapshot.Details))
	}
}

func TestPersistenceManagerFlushWritesExportPayload(t *testing.T) {
	backupDir := t.TempDir()
	filePath := filepath.Join(backupDir, "usage-statistics.json")

	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		Provider:    "codex",
		Model:       "gpt-5-mini",
		APIKey:      "api-key",
		RequestedAt: time.Date(2026, 3, 7, 22, 0, 0, 0, time.UTC),
		Detail: coreusage.Detail{
			InputTokens:  2,
			OutputTokens: 3,
			TotalTokens:  5,
		},
	})

	manager := NewPersistenceManager(stats, filePath)
	if err := manager.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	data, err := os.ReadFile(manager.FilePath())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var payload ExportPayload
	if err = json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Version != SnapshotVersion {
		t.Fatalf("payload.Version = %d, want %d", payload.Version, SnapshotVersion)
	}
	if payload.Usage.TotalRequests != 1 {
		t.Fatalf("payload.Usage.TotalRequests = %d, want 1", payload.Usage.TotalRequests)
	}
}

func TestPersistenceManagerLoadTracksLastError(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "usage-statistics.json")
	if err := os.WriteFile(filePath, []byte(`{"version":99,"usage":{}}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager := NewPersistenceManager(NewRequestStatistics(), filePath)
	if _, err := manager.Load(); !errors.Is(err, ErrUnsupportedSnapshotVersion) {
		t.Fatalf("Load() error = %v, want ErrUnsupportedSnapshotVersion", err)
	}

	status := manager.Status()
	if status.LastError == "" {
		t.Fatalf("LastError = empty, want populated")
	}
	if status.LastErrorAt.IsZero() {
		t.Fatalf("LastErrorAt is zero, want populated")
	}
}
