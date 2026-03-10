package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestNewWatcherStartsConfigOnly(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("debug: false\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	w, err := NewWatcher(cfgPath, func(*config.Config) {})
	if err != nil {
		t.Fatalf("NewWatcher() error = %v", err)
	}
	defer func() { _ = w.Stop() }()
	w.SetConfig(&config.Config{})

	ctx, cancel := contextWithTimeout(t, 3*time.Second)
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestDispatchRuntimeAuthUpdateQueuesEvents(t *testing.T) {
	w, err := NewWatcher(filepath.Join(t.TempDir(), "config.yaml"), nil)
	if err != nil {
		t.Fatalf("NewWatcher() error = %v", err)
	}
	defer func() { _ = w.Stop() }()
	queue := make(chan AuthUpdate, 4)
	w.SetAuthUpdateQueue(queue)
	w.SetConfig(&config.Config{})

	auth := &coreauth.Auth{ID: "cred-1", Provider: "codex"}
	if !w.DispatchRuntimeAuthUpdate(AuthUpdate{Action: AuthUpdateActionAdd, ID: auth.ID, Auth: auth}) {
		t.Fatalf("DispatchRuntimeAuthUpdate() = false, want true")
	}

	select {
	case update := <-queue:
		if update.Action != AuthUpdateActionAdd || update.ID != auth.ID {
			t.Fatalf("unexpected update: %+v", update)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for auth update")
	}
}

func TestReloadConfigIfChangedPublishesConfigBackedAuths(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("gemini-api-key:\n  - api-key: test-key\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	w, err := NewWatcher(cfgPath, nil)
	if err != nil {
		t.Fatalf("NewWatcher() error = %v", err)
	}
	defer func() { _ = w.Stop() }()
	queue := make(chan AuthUpdate, 8)
	w.SetAuthUpdateQueue(queue)
	w.SetConfig(&config.Config{})

	if !w.reloadConfig() {
		t.Fatal("reloadConfig() = false, want true")
	}

	select {
	case update := <-queue:
		if update.Action != AuthUpdateActionAdd {
			t.Fatalf("Action = %q, want add", update.Action)
		}
		if update.Auth == nil || update.Auth.Provider != "gemini" {
			t.Fatalf("unexpected auth payload: %+v", update.Auth)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for config-backed auth update")
	}
}

func TestBuildAPIKeyClientsCountsConfigEntries(t *testing.T) {
	cfg := &config.Config{
		GeminiKey:           []config.GeminiKey{{APIKey: "g1"}},
		VertexCompatAPIKey:  []config.VertexCompatKey{{APIKey: "v1"}},
		ClaudeKey:           []config.ClaudeKey{{APIKey: "c1"}},
		CodexKey:            []config.CodexKey{{APIKey: "x1"}},
		OpenAICompatibility: []config.OpenAICompatibility{{APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "o1"}, {APIKey: "o2"}}}},
	}

	g, v, c, x, o := BuildAPIKeyClients(cfg)
	if g != 1 || v != 1 || c != 1 || x != 1 || o != 2 {
		t.Fatalf("unexpected counts: gemini=%d vertex=%d claude=%d codex=%d openai=%d", g, v, c, x, o)
	}
}

func contextWithTimeout(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), timeout)
}
