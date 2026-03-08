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
