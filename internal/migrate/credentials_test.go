package migrate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type fakeCredentialStore struct {
	exists map[string]string
	saved  []string
}

func (s *fakeCredentialStore) LookupCredentialIDByRuntimeID(_ context.Context, runtimeID string) (string, bool, error) {
	id, ok := s.exists[runtimeID]
	return id, ok, nil
}

func (s *fakeCredentialStore) Save(_ context.Context, auth *coreauth.Auth) (string, error) {
	s.saved = append(s.saved, auth.ID)
	if _, ok := s.exists[auth.ID]; ok {
		return s.exists[auth.ID], nil
	}
	id := "cred-" + auth.ID
	s.exists[auth.ID] = id
	return id, nil
}

func TestImportCredentialsFromDir_ImportsAndUpdatesRecursively(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.json"), `{"type":"codex"}`)
	mustWriteFile(t, filepath.Join(dir, "nested", "b.json"), `{"type":"claude"}`)
	mustWriteFile(t, filepath.Join(dir, "skip.txt"), `nope`)

	store := &fakeCredentialStore{exists: map[string]string{"nested/b.json": "cred-existing"}}
	var out bytes.Buffer
	summary, err := ImportCredentialsFromDir(context.Background(), store, CredentialImportOptions{
		Dir: dir,
		BuildRecord: func(credentialName, runtimeID string, _ []byte) (*coreauth.Auth, error) {
			return &coreauth.Auth{ID: runtimeID, FileName: credentialName, Provider: "codex", Metadata: map[string]any{"type": "codex"}}, nil
		},
		Out: &out,
	})
	if err != nil {
		t.Fatalf("ImportCredentialsFromDir() error = %v", err)
	}
	if summary.Scanned != 2 || summary.Imported != 1 || summary.Updated != 1 || summary.Skipped != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(store.saved) != 2 {
		t.Fatalf("expected 2 saves, got %d", len(store.saved))
	}
}

func TestImportCredentialsFromDir_DryRunDoesNotSave(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.json"), `{"type":"codex"}`)
	store := &fakeCredentialStore{exists: map[string]string{}}
	summary, err := ImportCredentialsFromDir(context.Background(), store, CredentialImportOptions{
		Dir:    dir,
		DryRun: true,
		BuildRecord: func(credentialName, runtimeID string, _ []byte) (*coreauth.Auth, error) {
			return &coreauth.Auth{ID: runtimeID, FileName: credentialName, Provider: "codex", Metadata: map[string]any{"type": "codex"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("dry run error = %v", err)
	}
	if summary.Imported != 1 || len(store.saved) != 0 {
		t.Fatalf("unexpected dry run result: summary=%+v saved=%v", summary, store.saved)
	}
}

func TestImportCredentialsFromDir_ContinuesAfterFailure(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "good.json"), `{"type":"codex"}`)
	mustWriteFile(t, filepath.Join(dir, "bad.json"), `not-json`)
	store := &fakeCredentialStore{exists: map[string]string{}}
	var errOut bytes.Buffer
	summary, err := ImportCredentialsFromDir(context.Background(), store, CredentialImportOptions{
		Dir: dir,
		BuildRecord: func(credentialName, runtimeID string, payload []byte) (*coreauth.Auth, error) {
			if string(payload) == `not-json` {
				return nil, errors.New("invalid credential file")
			}
			return &coreauth.Auth{ID: runtimeID, FileName: credentialName, Provider: "codex", Metadata: map[string]any{"type": "codex"}}, nil
		},
		ErrOut: &errOut,
	})
	if err == nil {
		t.Fatal("expected aggregate failure error")
	}
	if summary.Imported != 1 || summary.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
