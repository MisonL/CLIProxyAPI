package management

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteOAuthCallbackFileUsesScratchDirectory(t *testing.T) {
	t.Helper()
	legacyDir := filepath.Join(t.TempDir(), "credentials")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}

	path, err := WriteOAuthCallbackFile("codex", "state-123", "code-abc", "")
	if err != nil {
		t.Fatalf("WriteOAuthCallbackFile() error = %v", err)
	}
	if strings.HasPrefix(path, legacyDir) {
		t.Fatalf("expected oauth callback file outside legacy auth dir, got %s", path)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatalf("expected oauth callback file to exist, stat err=%v", err)
	}
}
