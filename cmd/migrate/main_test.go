package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/platform"
)

func TestRunCredentialsImportCommandDryRun(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "team"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "team", "alpha.json"), []byte(`{"type":"codex","email":"a@example.com"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	originalImport := importCredentialsDir
	importCredentialsDir = func(_ context.Context, _ platform.Config, options platform.CredentialImportOptions) (platform.CredentialImportResult, error) {
		if options.Directory != root || !options.DryRun {
			t.Fatalf("unexpected options: %+v", options)
		}
		return platform.CredentialImportResult{Directory: root, DryRun: true, Scanned: 1, Imported: 1}, nil
	}
	defer func() { importCredentialsDir = originalImport }()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"credentials",
		"--dir", root,
		"--database-url", "postgres://example",
		"--database-schema", "controlplane",
		"--master-key", "secret",
		"--tenant-slug", "boss",
		"--tenant-name", "Boss",
		"--workspace-slug", "default",
		"--workspace-name", "Default",
		"--dry-run",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run dry-run: %v, stderr=%s", err, stderr.String())
	}
	var summary importSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if !summary.DryRun || summary.Scanned != 1 || summary.Imported != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestRunInvalidCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"unknown"}, &stdout, &stderr); err == nil {
		t.Fatal("expected invalid command error")
	}
	if stderr.Len() == 0 {
		t.Fatal("expected usage on stderr")
	}
}
