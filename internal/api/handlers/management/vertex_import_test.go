package management

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/platform"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type fixedRefAuthStore struct {
	ref  string
	auth *coreauth.Auth
}

func (s *fixedRefAuthStore) List(context.Context) ([]*coreauth.Auth, error) { return nil, nil }

func (s *fixedRefAuthStore) Save(_ context.Context, auth *coreauth.Auth) (string, error) {
	s.auth = auth.Clone()
	return s.ref, nil
}

func (s *fixedRefAuthStore) Delete(context.Context, string) error { return nil }

func TestImportVertexCredential_ReturnsUnifiedCredentialFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name            string
		savedRef        string
		platformEnabled bool
	}{
		{
			name:            "unified response without platform runtime",
			savedRef:        "/tmp/credentials/vertex-demo-project.json",
			platformEnabled: false,
		},
		{
			name:            "unified response with platform runtime",
			savedRef:        "cred-vertex-1",
			platformEnabled: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fixedRefAuthStore{ref: tc.savedRef}
			h := NewHandlerWithoutConfigFilePath(&config.Config{CredentialsDir: t.TempDir()}, coreauth.NewManager(nil, nil, nil))
			h.SetTokenStore(store)
			if tc.platformEnabled {
				h.SetPlatformRuntime(&platform.Runtime{})
			}

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = newVertexImportRequest(t)

			h.ImportVertexCredential(ctx)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
			}

			var payload map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if payload["credential_ref"] != tc.savedRef {
				t.Fatalf("expected credential_ref %q, got %#v", tc.savedRef, payload["credential_ref"])
			}
			if payload["credential_name"] != "vertex-demo-project.json" {
				t.Fatalf("unexpected credential_name: %#v", payload["credential_name"])
			}
			if payload["runtime_id"] != "vertex-demo-project.json" {
				t.Fatalf("unexpected runtime_id: %#v", payload["runtime_id"])
			}
			if payload["project_id"] != "demo-project" {
				t.Fatalf("unexpected project_id: %#v", payload["project_id"])
			}
			if payload["email"] != "service-account@example.com" {
				t.Fatalf("unexpected email: %#v", payload["email"])
			}
			if payload["location"] != "asia-east1" {
				t.Fatalf("unexpected location: %#v", payload["location"])
			}

			if payload["credential_id"] != tc.savedRef {
				t.Fatalf("expected credential_id %q, got %#v", tc.savedRef, payload["credential_id"])
			}
			if _, exists := payload["auth-file"]; exists {
				t.Fatalf("did not expect legacy auth-file in unified response: %#v", payload["auth-file"])
			}
		})
	}
}

func newVertexImportRequest(t *testing.T) *http.Request {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	serviceAccount := map[string]any{
		"type":         "service_account",
		"project_id":   "demo-project",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})),
		"client_email": "service-account@example.com",
	}

	rawJSON, err := json.Marshal(serviceAccount)
	if err != nil {
		t.Fatalf("marshal service account: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("file", "vertex.json")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err = fileWriter.Write(rawJSON); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err = writer.WriteField("location", "asia-east1"); err != nil {
		t.Fatalf("write location: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v0/management/vertex/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}
