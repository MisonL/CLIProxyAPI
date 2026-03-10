package platformv2

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	appconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/platform"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type fakeService struct {
	lookup  map[string]string
	deleted []string
}

func (fakeService) Status() platform.RuntimeStatus {
	return platform.RuntimeStatus{Enabled: true, Role: "server"}
}

func (fakeService) ProviderOverview(ctx context.Context, provider string) (platform.ProviderOverview, error) {
	_ = ctx
	return platform.ProviderOverview{Provider: provider, Mode: "usage-only"}, nil
}

func (fakeService) GetHistogramBucketItems(ctx context.Context, provider string, datasetID string, bucketIndex int, page int, pageSize int) (platform.HistogramBucketItemsResponse, error) {
	_ = ctx
	return platform.HistogramBucketItemsResponse{
		Provider:    provider,
		DatasetID:   datasetID,
		BucketIndex: bucketIndex,
		Total:       1,
		Page:        page,
		PageSize:    pageSize,
		Items: []platform.HistogramBucketItemRow{{
			CredentialID:     "cred-1",
			CredentialName:   "demo.json",
			RemainingPercent: 95,
			Disabled:         false,
			Unavailable:      false,
			QuotaExceeded:    false,
		}},
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func (fakeService) ListProviderCredentials(ctx context.Context, provider string, query platform.CredentialListQuery) (platform.CredentialListResult, error) {
	_ = ctx
	_ = provider
	return platform.CredentialListResult{Page: query.Page, PageSize: query.PageSize}, nil
}

func (fakeService) ListCredentials(ctx context.Context, query platform.CredentialListQuery) (platform.CredentialListResult, error) {
	_ = ctx
	return platform.CredentialListResult{Page: query.Page, PageSize: query.PageSize}, nil
}

func (fakeService) GetCredentialDetail(ctx context.Context, credentialID string) (platform.CredentialDetail, error) {
	_ = ctx
	return platform.CredentialDetail{
		ProviderCredentialRow: platform.ProviderCredentialRow{
			ID:           credentialID,
			SourceAuthID: "auth-" + credentialID,
			FileName:     credentialID + ".json",
		},
	}, nil
}

func (fakeService) GetTraceByRequestID(ctx context.Context, requestID string) (platform.RequestTraceResult, error) {
	_ = ctx
	return platform.RequestTraceResult{
		RequestID: requestID,
		Items: []platform.RequestTraceEvent{{
			EventKey:          "evt-1",
			RequestID:         requestID,
			Provider:          "codex",
			Model:             "gpt-5",
			SourceDisplayName: "codex-a.json",
			SourceType:        "codex",
		}},
	}, nil
}

func (fakeService) DownloadCredentialContent(ctx context.Context, credentialID string) (string, string, []byte, error) {
	_ = ctx
	return credentialID + ".json", credentialID, []byte(`{}`), nil
}

func (fakeService) GetQuotaRefreshPolicies(ctx context.Context) (platform.QuotaRefreshPoliciesResponse, error) {
	_ = ctx
	return platform.QuotaRefreshPoliciesResponse{}, nil
}

func (fakeService) SetQuotaRefreshPolicies(ctx context.Context, policies []platform.QuotaRefreshPolicy) (platform.QuotaRefreshPoliciesResponse, error) {
	_ = ctx
	return platform.QuotaRefreshPoliciesResponse{Policies: policies}, nil
}

func (fakeService) RefreshProvider(ctx context.Context, provider string) error {
	_ = ctx
	_ = provider
	return nil
}

func (s fakeService) LookupCredentialID(ctx context.Context, sourceAuthID string) (string, error) {
	_ = ctx
	if s.lookup == nil {
		return "", nil
	}
	return s.lookup[sourceAuthID], nil
}

func (s *fakeService) Delete(ctx context.Context, id string) error {
	_ = ctx
	s.deleted = append(s.deleted, id)
	return nil
}

func TestProviderOverview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := New(&fakeService{}, coreauth.NewManager(nil, nil, nil), &appconfig.Config{})
	router.GET("/v2/providers/:provider/overview", handler.ProviderOverview)

	req := httptest.NewRequest(http.MethodGet, "/v2/providers/codex/overview", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestGetHistogramBucketItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := New(&fakeService{}, coreauth.NewManager(nil, nil, nil), &appconfig.Config{})
	router.GET("/v2/providers/:provider/histogram-bucket-items", handler.GetHistogramBucketItems)

	req := httptest.NewRequest(http.MethodGet, "/v2/providers/codex/histogram-bucket-items?dataset_id=quota-5&bucket_index=0&page=2&page_size=50", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "quota-5") {
		t.Fatalf("expected dataset id in response, got %s", rec.Body.String())
	}
}

func TestGetCredentialModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "auth-cred-1",
		FileName: "cred-1.json",
		Provider: "codex",
		Status:   coreauth.StatusActive,
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	reg := registry.GetGlobalRegistry()
	reg.UnregisterClient(auth.ID)
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})
	reg.RegisterClient(auth.ID, "codex", []*registry.ModelInfo{{ID: "gpt-4.1"}})

	handler := New(&fakeService{}, manager, &appconfig.Config{})
	router.GET("/v2/credentials/:credentialID/models", handler.GetCredentialModels)

	req := httptest.NewRequest(http.MethodGet, "/v2/credentials/cred-1/models", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "gpt-4.1") {
		t.Fatalf("expected model payload, got %s", rec.Body.String())
	}
}

func TestGetTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := New(&fakeService{}, coreauth.NewManager(nil, nil, nil), &appconfig.Config{})
	router.GET("/v2/traces/:requestID", handler.GetTrace)

	req := httptest.NewRequest(http.MethodGet, "/v2/traces/abcd1234", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "abcd1234") || !strings.Contains(rec.Body.String(), "codex-a.json") {
		t.Fatalf("expected trace payload, got %s", rec.Body.String())
	}
}

func TestImportCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	manager := coreauth.NewManager(nil, nil, nil)
	handler := New(&fakeService{
		lookup: map[string]string{"demo.json": "cred-demo"},
	}, manager, &appconfig.Config{})
	router.POST("/v2/credentials/import", handler.ImportCredential)

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "demo.json")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err = part.Write([]byte(`{"type":"codex","email":"boss@example.com"}`)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/v2/credentials/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := manager.GetByID("demo.json"); !ok {
		t.Fatal("expected imported auth to be registered in runtime manager")
	}
	if !strings.Contains(rec.Body.String(), "cred-demo") {
		t.Fatalf("expected credential id in response, got %s", rec.Body.String())
	}
}

func TestPatchCredentialStatusAndDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "auth-cred-1",
		FileName: "cred-1.json",
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{"type": "codex"},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	service := &fakeService{}
	handler := New(service, manager, &appconfig.Config{})
	router.PATCH("/v2/credentials/:credentialID/status", handler.PatchCredentialStatus)
	router.DELETE("/v2/credentials/:credentialID", handler.DeleteCredential)

	statusReq := httptest.NewRequest(http.MethodPatch, "/v2/credentials/cred-1/status", strings.NewReader(`{"disabled":true}`))
	statusReq.Header.Set("Content-Type", "application/json")
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", statusRec.Code, statusRec.Body.String())
	}
	updated, ok := manager.GetByID("auth-cred-1")
	if !ok || updated == nil || !updated.Disabled {
		t.Fatalf("expected disabled runtime auth, got %+v", updated)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/v2/credentials/cred-1", nil)
	deleteRec := httptest.NewRecorder()
	router.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	if len(service.deleted) != 1 || service.deleted[0] != "auth-cred-1" {
		t.Fatalf("expected source auth id delete call, got %#v", service.deleted)
	}
}
