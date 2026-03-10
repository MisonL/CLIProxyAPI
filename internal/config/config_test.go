package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigRejectsRetiredCredentialsDir(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("credentials-dir: /tmp/legacy-auths\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := LoadConfig(configPath)
	if err == nil {
		t.Fatal("expected error for retired credentials-dir")
	}
	if !strings.Contains(err.Error(), "credentials-dir") || !strings.Contains(err.Error(), "CLIProxyAPI-migrate credentials") {
		t.Fatalf("unexpected error: %v", err)
	}
}
