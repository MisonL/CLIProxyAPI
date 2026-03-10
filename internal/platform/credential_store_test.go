package platform

import (
	"encoding/json"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestLoadAuthRawContentPreservesFlatMetadataPayload(t *testing.T) {
	auth := &coreauth.Auth{
		Provider: "codex",
		Label:    "demo",
		Prefix:   "team-a",
		ProxyURL: "http://proxy.local",
		Disabled: true,
		Metadata: map[string]any{
			"type":     "codex",
			"email":    "boss@example.com",
			"priority": 3,
		},
	}

	payload, err := loadAuthRawContent(auth)
	if err != nil {
		t.Fatalf("loadAuthRawContent() error = %v", err)
	}

	var decoded map[string]any
	if err = json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if decoded["type"] != "codex" {
		t.Fatalf("type = %v, want codex", decoded["type"])
	}
	if decoded["prefix"] != "team-a" {
		t.Fatalf("prefix = %v, want team-a", decoded["prefix"])
	}
	if decoded["proxy_url"] != "http://proxy.local" {
		t.Fatalf("proxy_url = %v, want http://proxy.local", decoded["proxy_url"])
	}
	if decoded["disabled"] != true {
		t.Fatalf("disabled = %v, want true", decoded["disabled"])
	}
	if _, exists := decoded["metadata"]; exists {
		t.Fatalf("unexpected nested metadata field in payload: %v", decoded)
	}
}
