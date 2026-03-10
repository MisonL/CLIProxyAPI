package platformv2

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	appconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/platform"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type QueryService interface {
	Status() platform.RuntimeStatus
	ProviderOverview(ctx context.Context, provider string) (platform.ProviderOverview, error)
	GetHistogramBucketItems(ctx context.Context, provider string, datasetID string, bucketIndex int, page int, pageSize int) (platform.HistogramBucketItemsResponse, error)
	ListProviderCredentials(ctx context.Context, provider string, query platform.CredentialListQuery) (platform.CredentialListResult, error)
	ListCredentials(ctx context.Context, query platform.CredentialListQuery) (platform.CredentialListResult, error)
	GetCredentialDetail(ctx context.Context, credentialID string) (platform.CredentialDetail, error)
	GetTraceByRequestID(ctx context.Context, requestID string) (platform.RequestTraceResult, error)
	DownloadCredentialContent(ctx context.Context, credentialID string) (string, string, []byte, error)
	GetQuotaRefreshPolicies(ctx context.Context) (platform.QuotaRefreshPoliciesResponse, error)
	SetQuotaRefreshPolicies(ctx context.Context, policies []platform.QuotaRefreshPolicy) (platform.QuotaRefreshPoliciesResponse, error)
	RefreshProvider(ctx context.Context, provider string) error
	LookupCredentialID(ctx context.Context, sourceAuthID string) (string, error)
	Delete(ctx context.Context, id string) error
}

type Handler struct {
	service     QueryService
	authManager *coreauth.Manager
	cfg         *appconfig.Config
}

func New(service QueryService, authManager *coreauth.Manager, cfg *appconfig.Config) *Handler {
	return &Handler{service: service, authManager: authManager, cfg: cfg}
}

func (h *Handler) Status(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	c.JSON(http.StatusOK, h.service.Status())
}

func (h *Handler) ProviderOverview(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	overview, err := h.service.ProviderOverview(c.Request.Context(), strings.TrimSpace(c.Param("provider")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, overview)
}

func (h *Handler) GetHistogramBucketItems(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	provider := strings.TrimSpace(c.Param("provider"))
	datasetID := strings.TrimSpace(c.Query("dataset_id"))
	if datasetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dataset_id is required"})
		return
	}
	bucketIndex, err := strconv.Atoi(strings.TrimSpace(c.Query("bucket_index")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bucket_index is required"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "200"))

	result, err := h.service.GetHistogramBucketItems(c.Request.Context(), provider, datasetID, bucketIndex, page, pageSize)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) ListProviderCredentials(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	result, err := h.service.ListProviderCredentials(
		c.Request.Context(),
		strings.TrimSpace(c.Param("provider")),
		platform.CredentialListQuery{
			Page:     page,
			PageSize: pageSize,
			Search:   strings.TrimSpace(c.Query("search")),
		},
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) ListCredentials(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	result, err := h.service.ListCredentials(c.Request.Context(), platform.CredentialListQuery{
		Page:          page,
		PageSize:      pageSize,
		Search:        strings.TrimSpace(c.Query("search")),
		Provider:      strings.TrimSpace(c.Query("provider")),
		Status:        strings.TrimSpace(c.Query("status")),
		ActivityRange: strings.TrimSpace(c.Query("activity")),
		SortBy:        strings.TrimSpace(c.Query("sort")),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetCredential(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	result, err := h.service.GetCredentialDetail(c.Request.Context(), strings.TrimSpace(c.Param("credentialID")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetTrace(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	requestID := strings.TrimSpace(c.Param("requestID"))
	if requestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request_id is required"})
		return
	}
	result, err := h.service.GetTraceByRequestID(c.Request.Context(), requestID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) DownloadCredential(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	fileName, _, payload, err := h.service.DownloadCredentialContent(c.Request.Context(), strings.TrimSpace(c.Param("credentialID")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+fileName+`"`)
	c.Data(http.StatusOK, "application/json", payload)
}

func (h *Handler) ImportCredential(c *gin.Context) {
	if h == nil || h.service == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	fileName := filepath.Base(strings.TrimSpace(fileHeader.Filename))
	if !strings.HasSuffix(strings.ToLower(fileName), ".json") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file must be .json"})
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("failed to read file: %v", err)})
		return
	}
	defer file.Close()

	payload, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("failed to read file: %v", err)})
		return
	}

	ctx := c.Request.Context()
	existing, _ := h.authManager.GetByID(fileName)
	record, err := h.buildAuthRecord(fileName, fileName, payload, existing)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if existing != nil {
		if _, err = h.authManager.Update(ctx, record); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		if _, err = h.authManager.Register(ctx, record); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	credentialID, err := h.service.LookupCredentialID(ctx, record.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":          "ok",
		"credential_id":   credentialID,
		"credential_name": record.FileName,
		"runtime_id":      record.ID,
	})
}

func (h *Handler) UpdateCredentialContent(c *gin.Context) {
	if h == nil || h.service == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	credentialID := strings.TrimSpace(c.Param("credentialID"))
	detail, err := h.service.GetCredentialDetail(c.Request.Context(), credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}

	existing := h.findRuntimeAuth(detail.SourceAuthID, detail.FileName)
	record, err := h.buildAuthRecord(detail.FileName, detail.SourceAuthID, payload, existing)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if existing != nil {
		if _, err = h.authManager.Update(c.Request.Context(), record); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		if _, err = h.authManager.Register(c.Request.Context(), record); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) PatchCredentialStatus(c *gin.Context) {
	if h == nil || h.service == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	var body struct {
		Disabled *bool `json:"disabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Disabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	credentialID := strings.TrimSpace(c.Param("credentialID"))
	detail, err := h.service.GetCredentialDetail(c.Request.Context(), credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing := h.findRuntimeAuth(detail.SourceAuthID, detail.FileName)
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credential runtime auth not found"})
		return
	}
	next := existing.Clone()
	next.Disabled = *body.Disabled
	if *body.Disabled {
		next.Status = coreauth.StatusDisabled
		next.StatusMessage = "disabled via platform API"
	} else {
		next.Status = coreauth.StatusActive
		next.StatusMessage = ""
	}
	next.UpdatedAt = time.Now().UTC()
	if _, err = h.authManager.Update(c.Request.Context(), next); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "disabled": next.Disabled})
}

func (h *Handler) DeleteCredential(c *gin.Context) {
	if h == nil || h.service == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	credentialID := strings.TrimSpace(c.Param("credentialID"))
	detail, err := h.service.GetCredentialDetail(c.Request.Context(), credentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if existing := h.findRuntimeAuth(detail.SourceAuthID, detail.FileName); existing != nil {
		next := existing.Clone()
		next.Disabled = true
		next.Status = coreauth.StatusDisabled
		next.StatusMessage = "removed via platform API"
		next.UpdatedAt = time.Now().UTC()
		if _, err = h.authManager.Update(c.Request.Context(), next); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if err = h.service.Delete(c.Request.Context(), detail.SourceAuthID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) GetCredentialModels(c *gin.Context) {
	if h == nil || h.service == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	detail, err := h.service.GetCredentialDetail(c.Request.Context(), strings.TrimSpace(c.Param("credentialID")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var authID string
	if detail.SourceAuthID != "" {
		authID = strings.TrimSpace(detail.SourceAuthID)
	}
	if authID == "" {
		authID = strings.TrimSpace(detail.FileName)
	}
	targetAuth, ok := h.authManager.GetByID(authID)
	if !ok && strings.TrimSpace(detail.FileName) != "" {
		for _, auth := range h.authManager.List() {
			if auth == nil {
				continue
			}
			if strings.TrimSpace(auth.FileName) == strings.TrimSpace(detail.FileName) {
				targetAuth = auth
				ok = true
				break
			}
		}
	}
	if !ok || targetAuth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "credential runtime auth not found"})
		return
	}

	models := registry.GetGlobalRegistry().GetModelsForClient(targetAuth.ID)
	result := make([]gin.H, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		entry := gin.H{"id": model.ID}
		if model.DisplayName != "" {
			entry["display_name"] = model.DisplayName
		}
		if model.Type != "" {
			entry["type"] = model.Type
		}
		if model.OwnedBy != "" {
			entry["owned_by"] = model.OwnedBy
		}
		result = append(result, entry)
	}
	c.JSON(http.StatusOK, gin.H{"models": result})
}

func (h *Handler) GetQuotaRefreshPolicies(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	result, err := h.service.GetQuotaRefreshPolicies(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) PutQuotaRefreshPolicies(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	var body struct {
		Policies []platform.QuotaRefreshPolicy `json:"policies"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	result, err := h.service.SetQuotaRefreshPolicies(c.Request.Context(), body.Policies)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) RefreshProvider(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "platform runtime unavailable"})
		return
	}
	if err := h.service.RefreshProvider(c.Request.Context(), strings.TrimSpace(c.Param("provider"))); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func (h *Handler) findRuntimeAuth(sourceAuthID, fileName string) *coreauth.Auth {
	if h == nil || h.authManager == nil {
		return nil
	}
	sourceAuthID = strings.TrimSpace(sourceAuthID)
	if sourceAuthID != "" {
		if auth, ok := h.authManager.GetByID(sourceAuthID); ok {
			return auth
		}
	}
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return nil
	}
	for _, auth := range h.authManager.List() {
		if auth == nil {
			continue
		}
		if strings.TrimSpace(auth.FileName) == fileName {
			return auth
		}
	}
	return nil
}

func (h *Handler) buildAuthRecord(fileName, sourceAuthID string, payload []byte, existing *coreauth.Auth) (*coreauth.Auth, error) {
	return platform.BuildCredentialRecord(h.cfg, filepath.Base(strings.TrimSpace(fileName)), strings.TrimSpace(sourceAuthID), payload, existing)
}
