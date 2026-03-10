package platform

import (
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestBuildUsageEventKeyStable(t *testing.T) {
	event := UsageEvent{
		Provider:     "codex",
		Model:        "gpt-5",
		AuthID:       "auth-1",
		SelectionKey: "idx-1",
		Source:       "alpha",
		RequestID:    "abcd1234",
		RequestedAt:  time.Unix(100, 0).UTC(),
		TotalTokens:  42,
	}
	left := buildUsageEventKey(event)
	right := buildUsageEventKey(event)
	if left != right {
		t.Fatalf("buildUsageEventKey() unstable: %q != %q", left, right)
	}
}

func TestBuildUsageEvent_CopiesRequestID(t *testing.T) {
	record := coreusage.Record{
		Provider:     "codex",
		Model:        "gpt-5",
		AuthID:       "auth-1",
		SelectionKey: "idx-1",
		Source:       "alpha",
		RequestID:    "abcd1234",
		RequestedAt:  time.Unix(100, 0).UTC(),
	}

	event := BuildUsageEvent(record)
	if event.RequestID != "abcd1234" {
		t.Fatalf("BuildUsageEvent() request_id = %q, want %q", event.RequestID, "abcd1234")
	}
}
