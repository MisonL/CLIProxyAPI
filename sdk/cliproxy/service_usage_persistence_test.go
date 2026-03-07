package cliproxy

import (
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func TestResolveUsagePersistencePath_DisabledByDefault(t *testing.T) {
	if got := resolveUsagePersistencePath("/tmp/config.yaml", nil); got != "" {
		t.Fatalf("resolveUsagePersistencePath(nil) = %q, want empty", got)
	}

	cfg := &config.Config{}
	if got := resolveUsagePersistencePath("/tmp/config.yaml", cfg); got != "" {
		t.Fatalf("resolveUsagePersistencePath(empty) = %q, want empty", got)
	}
}

func TestResolveUsagePersistencePath_ResolvesRelativePath(t *testing.T) {
	cfg := &config.Config{UsagePersistenceFile: "usage-backups/usage-statistics.json"}

	got := resolveUsagePersistencePath("/tmp/cliproxy/config.yaml", cfg)
	want := filepath.Clean("/tmp/cliproxy/usage-backups/usage-statistics.json")
	if got != want {
		t.Fatalf("resolveUsagePersistencePath(relative) = %q, want %q", got, want)
	}
}

func TestResolveUsagePersistencePath_KeepsAbsolutePath(t *testing.T) {
	cfg := &config.Config{UsagePersistenceFile: "/workspace/usage-backups/usage-statistics.json"}

	got := resolveUsagePersistencePath("/tmp/cliproxy/config.yaml", cfg)
	if got != cfg.UsagePersistenceFile {
		t.Fatalf("resolveUsagePersistencePath(absolute) = %q, want %q", got, cfg.UsagePersistenceFile)
	}
}
