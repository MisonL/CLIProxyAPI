package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientCredentialsAndMutations(t *testing.T) {
	var (
		deletePath string
		statusPath string
		statusBody map[string]any
		putPath    string
		putBody    map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"id":              "cred-1",
					"runtime_id":      "source-1",
					"credential_name": "demo.json",
					"provider":        "codex",
					"account_email":   "boss@example.com",
					"status":          "active",
					"status_message":  "",
					"disabled":        false,
					"selection_key":   "auth-1",
				}},
				"total": 1,
			})
		case r.URL.Path == "/v2/credentials/cred-1" && r.Method == http.MethodDelete:
			deletePath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		case r.URL.Path == "/v2/credentials/cred-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":              "cred-1",
				"runtime_id":      "source-1",
				"credential_name": "demo.json",
				"provider":        "codex",
				"metadata": map[string]any{
					"prefix":    "team-a",
					"proxy_url": "http://proxy.local",
					"attributes": map[string]any{
						"priority": "7",
					},
					"metadata": map[string]any{
						"project_id": "proj-1",
					},
				},
			})
		case r.URL.Path == "/v2/credentials/cred-1/download":
			_, _ = w.Write([]byte(`{"type":"codex","prefix":"team-a"}`))
		case r.URL.Path == "/v2/credentials/cred-1/status":
			statusPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&statusBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "disabled": true})
		case r.URL.Path == "/v2/credentials/cred-1/content":
			putPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &Client{baseURL: server.URL, http: server.Client()}
	files, err := client.GetCredentials()
	if err != nil {
		t.Fatalf("GetCredentials() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0]["name"] != "demo.json" || files[0]["credential_id"] != "cred-1" {
		t.Fatalf("unexpected credential mapping: %#v", files[0])
	}
	if _, ok := files[0]["file_name"]; ok {
		t.Fatalf("unexpected legacy field file_name in %#v", files[0])
	}
	if _, ok := files[0]["auth_index"]; ok {
		t.Fatalf("unexpected legacy field auth_index in %#v", files[0])
	}
	if _, ok := files[0]["source_auth_id"]; ok {
		t.Fatalf("unexpected legacy field source_auth_id in %#v", files[0])
	}

	detail, err := client.GetCredentialDetail(files[0])
	if err != nil {
		t.Fatalf("GetCredentialDetail() error = %v", err)
	}
	if detail["prefix"] != "team-a" || detail["project_id"] != "proj-1" {
		t.Fatalf("unexpected detail mapping: %#v", detail)
	}
	if _, ok := detail["file_name"]; ok {
		t.Fatalf("unexpected legacy detail field file_name in %#v", detail)
	}
	if _, ok := detail["auth_index"]; ok {
		t.Fatalf("unexpected legacy detail field auth_index in %#v", detail)
	}
	if _, ok := detail["source_auth_id"]; ok {
		t.Fatalf("unexpected legacy detail field source_auth_id in %#v", detail)
	}

	if err = client.ToggleCredential(files[0], true); err != nil {
		t.Fatalf("ToggleCredential() error = %v", err)
	}
	if statusPath != "/v2/credentials/cred-1/status" || statusBody["disabled"] != true {
		t.Fatalf("unexpected status mutation: path=%s body=%#v", statusPath, statusBody)
	}

	if err = client.PatchCredentialFields(files[0], map[string]any{"prefix": "team-b"}); err != nil {
		t.Fatalf("PatchCredentialFields() error = %v", err)
	}
	if putPath != "/v2/credentials/cred-1/content" || putBody["prefix"] != "team-b" || putBody["type"] != "codex" {
		t.Fatalf("unexpected content mutation: path=%s body=%#v", putPath, putBody)
	}

	if err = client.DeleteCredential(files[0]); err != nil {
		t.Fatalf("DeleteCredential() error = %v", err)
	}
	if deletePath != "/v2/credentials/cred-1" {
		t.Fatalf("unexpected delete path %s", deletePath)
	}
}

func TestClientRequiresPlatformCredentialID(t *testing.T) {
	client := &Client{}

	if _, err := client.GetCredentialDetail(map[string]any{"name": "demo.json"}); err == nil {
		t.Fatal("expected GetCredentialDetail to reject files without credential_id")
	}
	if err := client.ToggleCredential(map[string]any{"name": "demo.json"}, true); err == nil {
		t.Fatal("expected ToggleCredential to reject files without credential_id")
	}
	if err := client.PatchCredentialFields(map[string]any{"name": "demo.json"}, map[string]any{"prefix": "team-b"}); err == nil {
		t.Fatal("expected PatchCredentialFields to reject files without credential_id")
	}
	if err := client.DeleteCredential(map[string]any{"name": "demo.json"}); err == nil {
		t.Fatal("expected DeleteCredential to reject files without credential_id")
	}
}
