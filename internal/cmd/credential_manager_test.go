package cmd

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestNewCommandAuthStore_RequiresPlatformMode(t *testing.T) {
	t.Setenv("CPA_PLATFORM_ENABLED", "false")
	t.Setenv("CPA_PLATFORM_DATABASE_URL", "")
	t.Setenv("CPA_PLATFORM_REDIS_URL", "")
	t.Setenv("CPA_PLATFORM_NATS_URL", "")

	_, err := newCommandAuthStore(&config.Config{CredentialsDir: t.TempDir()})
	if err == nil {
		t.Fatalf("expected platform mode requirement error")
	}
}

func TestNewCommandAuthStore_PlatformModeDoesNotSilentlyFallback(t *testing.T) {
	t.Setenv("CPA_PLATFORM_ENABLED", "true")
	t.Setenv("CPA_PLATFORM_DATABASE_URL", "")
	t.Setenv("CPA_PLATFORM_REDIS_URL", "")
	t.Setenv("CPA_PLATFORM_NATS_URL", "")
	t.Setenv("CPA_MASTER_KEY", "test-master-key")

	_, err := newCommandAuthStore(&config.Config{CredentialsDir: t.TempDir()})
	if err == nil {
		t.Fatalf("expected platform config validation error")
	}
}

func TestFindExistingIFlowCredentialByBXAuth(t *testing.T) {
	store := &memoryCommandStore{
		items: []*coreauth.Auth{
			{
				ID:       "iflow-a.json",
				FileName: "iflow-a.json",
				Provider: "iflow",
				Metadata: map[string]any{
					"cookie": "BXAuth=match-me;",
				},
			},
			{
				ID:       "codex-a.json",
				FileName: "codex-a.json",
				Provider: "codex",
			},
		},
	}

	ref, err := findExistingIFlowCredentialByBXAuth(context.Background(), store, "match-me")
	if err != nil {
		t.Fatalf("findExistingIFlowCredentialByBXAuth() error = %v", err)
	}
	if ref != "iflow-a.json" {
		t.Fatalf("expected iflow-a.json, got %q", ref)
	}

	missing, err := findExistingIFlowCredentialByBXAuth(context.Background(), store, "missing")
	if err != nil {
		t.Fatalf("findExistingIFlowCredentialByBXAuth() missing error = %v", err)
	}
	if missing != "" {
		t.Fatalf("expected empty result for missing BXAuth, got %q", missing)
	}
}

type memoryCommandStore struct {
	items []*coreauth.Auth
}

func (s *memoryCommandStore) List(context.Context) ([]*coreauth.Auth, error) {
	return s.items, nil
}

func (s *memoryCommandStore) Save(context.Context, *coreauth.Auth) (string, error) {
	return "", nil
}

func (s *memoryCommandStore) Delete(context.Context, string) error {
	return nil
}
