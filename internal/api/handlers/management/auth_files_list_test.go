package management

import (
	"testing"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestBuildAuthFileEntry_IncludesQuotaRuntimeFields(t *testing.T) {
	now := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	h := &Handler{}
	auth := &coreauth.Auth{
		ID:          "quota-auth",
		FileName:    "quota-auth.json",
		Provider:    "qwen",
		Status:      coreauth.StatusActive,
		Unavailable: true,
		Attributes: map[string]string{
			"path": "/tmp/quota-auth.json",
		},
		Quota: coreauth.QuotaState{
			Exceeded:      true,
			Reason:        "quota",
			NextRecoverAt: now.Add(3 * time.Hour),
			BackoffLevel:  2,
		},
	}

	entry := h.buildAuthFileEntry(auth)
	if entry == nil {
		t.Fatal("expected auth entry")
	}

	if exceeded, ok := entry["quota_exceeded"].(bool); !ok || !exceeded {
		t.Fatalf("expected quota_exceeded=true, got %#v", entry["quota_exceeded"])
	}
	if reason, ok := entry["quota_reason"].(string); !ok || reason != "quota" {
		t.Fatalf("expected quota_reason=quota, got %#v", entry["quota_reason"])
	}
	if recoverAt, ok := entry["quota_next_recover_at"].(time.Time); !ok || !recoverAt.Equal(now.Add(3*time.Hour)) {
		t.Fatalf("expected quota_next_recover_at to be preserved, got %#v", entry["quota_next_recover_at"])
	}
	if backoff, ok := entry["quota_backoff_level"].(int); !ok || backoff != 2 {
		t.Fatalf("expected quota_backoff_level=2, got %#v", entry["quota_backoff_level"])
	}
}
