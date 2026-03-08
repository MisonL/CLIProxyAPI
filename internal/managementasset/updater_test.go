package managementasset

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureLatestManagementHTMLSkipsRemoteSyncWhenLocalOverrideExists(t *testing.T) {
	lastUpdateCheckMu.Lock()
	lastUpdateCheckTime = time.Time{}
	lastUpdateCheckMu.Unlock()

	staticDir := t.TempDir()
	localPath := filepath.Join(staticDir, ManagementFileName)
	if err := os.WriteFile(localPath, []byte("<html>local override</html>"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("MANAGEMENT_STATIC_PATH", localPath)

	if ok := EnsureLatestManagementHTML(context.Background(), staticDir, "", "https://github.com/example/missing"); !ok {
		t.Fatalf("EnsureLatestManagementHTML() = false, want true")
	}

	lastUpdateCheckMu.Lock()
	defer lastUpdateCheckMu.Unlock()
	if !lastUpdateCheckTime.IsZero() {
		t.Fatalf("lastUpdateCheckTime = %v, want zero when local override short-circuits remote sync", lastUpdateCheckTime)
	}
}

func TestEnsureLatestManagementHTMLSkipsRemoteSyncWhenLocalAssetExists(t *testing.T) {
	lastUpdateCheckMu.Lock()
	lastUpdateCheckTime = time.Time{}
	lastUpdateCheckMu.Unlock()

	staticDir := t.TempDir()
	localPath := filepath.Join(staticDir, ManagementFileName)
	if err := os.WriteFile(localPath, []byte("<html>local asset</html>"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if ok := EnsureLatestManagementHTML(context.Background(), staticDir, "", "https://github.com/example/missing"); !ok {
		t.Fatalf("EnsureLatestManagementHTML() = false, want true")
	}

	lastUpdateCheckMu.Lock()
	defer lastUpdateCheckMu.Unlock()
	if !lastUpdateCheckTime.IsZero() {
		t.Fatalf("lastUpdateCheckTime = %v, want zero when local asset short-circuits remote sync", lastUpdateCheckTime)
	}
}

func TestShouldSkipAutoUpdaterWhenLocalAssetExists(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("debug: false\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	staticDir := filepath.Join(configDir, "static")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, ManagementFileName), []byte("<html>local asset</html>"), 0o600); err != nil {
		t.Fatalf("WriteFile(management) error = %v", err)
	}

	if !shouldSkipAutoUpdater(configPath) {
		t.Fatal("shouldSkipAutoUpdater() = false, want true")
	}
}
