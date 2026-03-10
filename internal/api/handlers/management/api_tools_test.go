package management

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type memoryAuthStore struct {
	mu    sync.Mutex
	items map[string]*coreauth.Auth
}

func (s *memoryAuthStore) List(ctx context.Context) ([]*coreauth.Auth, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*coreauth.Auth, 0, len(s.items))
	for _, a := range s.items {
		out = append(out, a.Clone())
	}
	return out, nil
}

func (s *memoryAuthStore) Save(ctx context.Context, auth *coreauth.Auth) (string, error) {
	_ = ctx
	if auth == nil {
		return "", nil
	}
	s.mu.Lock()
	if s.items == nil {
		s.items = make(map[string]*coreauth.Auth)
	}
	s.items[auth.ID] = auth.Clone()
	s.mu.Unlock()
	return auth.ID, nil
}

func (s *memoryAuthStore) Delete(ctx context.Context, id string) error {
	_ = ctx
	s.mu.Lock()
	delete(s.items, id)
	s.mu.Unlock()
	return nil
}

func TestResolveTokenForAuth_Antigravity_RefreshesExpiredToken(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
			t.Fatalf("unexpected content-type: %s", ct)
		}
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		values, err := url.ParseQuery(string(bodyBytes))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if values.Get("grant_type") != "refresh_token" {
			t.Fatalf("unexpected grant_type: %s", values.Get("grant_type"))
		}
		if values.Get("refresh_token") != "rt" {
			t.Fatalf("unexpected refresh_token: %s", values.Get("refresh_token"))
		}
		if values.Get("client_id") != antigravityOAuthClientID {
			t.Fatalf("unexpected client_id: %s", values.Get("client_id"))
		}
		if values.Get("client_secret") != antigravityOAuthClientSecret {
			t.Fatalf("unexpected client_secret")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-token",
			"refresh_token": "rt2",
			"expires_in":    int64(3600),
			"token_type":    "Bearer",
		})
	}))
	t.Cleanup(srv.Close)

	originalURL := antigravityOAuthTokenURL
	antigravityOAuthTokenURL = srv.URL
	t.Cleanup(func() { antigravityOAuthTokenURL = originalURL })

	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)

	auth := &coreauth.Auth{
		ID:       "antigravity-test.json",
		FileName: "antigravity-test.json",
		Provider: "antigravity",
		Metadata: map[string]any{
			"type":          "antigravity",
			"access_token":  "old-token",
			"refresh_token": "rt",
			"expires_in":    int64(3600),
			"timestamp":     time.Now().Add(-2 * time.Hour).UnixMilli(),
			"expired":       time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := &Handler{authManager: manager}
	token, err := h.resolveTokenForAuth(context.Background(), auth)
	if err != nil {
		t.Fatalf("resolveTokenForAuth: %v", err)
	}
	if token != "new-token" {
		t.Fatalf("expected refreshed token, got %q", token)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 refresh call, got %d", callCount)
	}

	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatalf("expected auth in manager after update")
	}
	if got := tokenValueFromMetadata(updated.Metadata); got != "new-token" {
		t.Fatalf("expected manager metadata updated, got %q", got)
	}
}

func TestResolveTokenForAuth_Antigravity_SkipsRefreshWhenTokenValid(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	originalURL := antigravityOAuthTokenURL
	antigravityOAuthTokenURL = srv.URL
	t.Cleanup(func() { antigravityOAuthTokenURL = originalURL })

	auth := &coreauth.Auth{
		ID:       "antigravity-valid.json",
		FileName: "antigravity-valid.json",
		Provider: "antigravity",
		Metadata: map[string]any{
			"type":         "antigravity",
			"access_token": "ok-token",
			"expired":      time.Now().Add(30 * time.Minute).Format(time.RFC3339),
		},
	}
	h := &Handler{}
	token, err := h.resolveTokenForAuth(context.Background(), auth)
	if err != nil {
		t.Fatalf("resolveTokenForAuth: %v", err)
	}
	if token != "ok-token" {
		t.Fatalf("expected existing token, got %q", token)
	}
	if callCount != 0 {
		t.Fatalf("expected no refresh calls, got %d", callCount)
	}
}

func TestAPICall_ResolvesCredentialBySelectionKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var authHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`ok`))
	}))
	t.Cleanup(upstream.Close)

	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	auth := &coreauth.Auth{
		ID:       "selection-key-test.json",
		FileName: "selection-key-test.json",
		Provider: "gemini",
		Metadata: map[string]any{"access_token": "token-123"},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	selectionKey := auth.EnsureSelectionKey()

	handler := &Handler{authManager: manager}
	router := gin.New()
	router.POST("/api-call", handler.APICall)

	body := `{"selection_key":"` + selectionKey + `","method":"GET","url":"` + upstream.URL + `","header":{"Authorization":"Bearer $TOKEN$"}}`
	req := httptest.NewRequest(http.MethodPost, "/api-call", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if authHeader != "Bearer token-123" {
		t.Fatalf("Authorization header = %q, want %q", authHeader, "Bearer token-123")
	}
}

func TestAPICallBatch_ReturnsPerItemResults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alpha":
			w.Header().Set("X-Test", "alpha")
			_, _ = w.Write([]byte(`{"name":"alpha"}`))
		case "/beta":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"name":"beta"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	handler := &Handler{}
	router := gin.New()
	router.POST("/api-call/batch", handler.APICallBatch)

	body := `{
		"items": [
			{"key":"first","method":"GET","url":"` + upstream.URL + `/alpha"},
			{"key":"second","method":"GET","url":"` + upstream.URL + `/beta"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api-call/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Items []struct {
			Key        string              `json:"key"`
			StatusCode int                 `json:"status_code"`
			Header     map[string][]string `json:"header"`
			Body       string              `json:"body"`
		} `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal batch response: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(payload.Items))
	}
	if payload.Items[0].Key != "first" || payload.Items[0].StatusCode != http.StatusOK {
		t.Fatalf("unexpected first item: %#v", payload.Items[0])
	}
	if !strings.Contains(payload.Items[0].Body, `"alpha"`) {
		t.Fatalf("expected alpha body, got %q", payload.Items[0].Body)
	}
	if payload.Items[1].Key != "second" || payload.Items[1].StatusCode != http.StatusCreated {
		t.Fatalf("unexpected second item: %#v", payload.Items[1])
	}
	if !strings.Contains(payload.Items[1].Body, `"beta"`) {
		t.Fatalf("expected beta body, got %q", payload.Items[1].Body)
	}
}

func TestAPICallBatch_CollectsItemErrorsWithoutFailingWholeBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`ok`))
	}))
	t.Cleanup(upstream.Close)

	handler := &Handler{}
	router := gin.New()
	router.POST("/api-call/batch", handler.APICallBatch)

	body := `{
		"items": [
			{"key":"ok","method":"GET","url":"` + upstream.URL + `"},
			{"key":"bad","url":"` + upstream.URL + `"}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/api-call/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Items []struct {
			Key        string              `json:"key"`
			StatusCode int                 `json:"status_code"`
			Header     map[string][]string `json:"header"`
			Body       any                 `json:"body"`
		} `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal batch response: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(payload.Items))
	}
	if payload.Items[0].Key != "ok" || payload.Items[0].StatusCode != http.StatusOK {
		t.Fatalf("unexpected first item: %#v", payload.Items[0])
	}
	if payload.Items[1].Key != "bad" || payload.Items[1].StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected second item: %#v", payload.Items[1])
	}
	bodyMap, ok := payload.Items[1].Body.(map[string]any)
	if !ok {
		t.Fatalf("expected error body object, got %#v", payload.Items[1].Body)
	}
	if bodyMap["error"] != "missing method" {
		t.Fatalf("expected missing method error, got %#v", bodyMap)
	}
}

func TestAPICall_ResolvesTokenBySelectionKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want %q", got, "Bearer test-token")
		}
		_, _ = w.Write([]byte(`ok`))
	}))
	t.Cleanup(upstream.Close)

	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	auth := &coreauth.Auth{
		ID:       "demo.json",
		FileName: "demo.json",
		Provider: "vertex",
		Metadata: map[string]any{"access_token": "test-token"},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	handler := &Handler{authManager: manager}
	result := handler.executeAPICall(context.Background(), apiCallRequest{
		SelectionKeySnake: stringPtr(auth.EnsureSelectionKey()),
		Method:            http.MethodGet,
		URL:               upstream.URL,
		Header:            map[string]string{"Authorization": "Bearer $TOKEN$"},
	})
	if result.ErrorStatus != 0 {
		t.Fatalf("ErrorStatus = %d, ErrorText = %q", result.ErrorStatus, result.ErrorText)
	}
	if result.Response.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", result.Response.StatusCode, http.StatusOK)
	}
}

func stringPtr(v string) *string { return &v }
