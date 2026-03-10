package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func TestGetSystemSelfCheckReturnsChecks(t *testing.T) {
	gin.SetMode(gin.TestMode)

	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	logDir := filepath.Join(baseDir, "logs")
	staticDir := filepath.Join(baseDir, "static")
	if err := os.WriteFile(configPath, []byte("debug: false\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	for _, dir := range []string{logDir, staticDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(staticDir, "management.html"), []byte("<html></html>"), 0o600); err != nil {
		t.Fatalf("WriteFile(management.html) error = %v", err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/system/self-check", nil)

	t.Setenv("MANAGEMENT_STATIC_PATH", staticDir)
	h := &Handler{
		cfg: &config.Config{
			RemoteManagement: config.RemoteManagement{
				AllowRemote: true,
			},
		},
		configFilePath: configPath,
		logDir:         logDir,
	}
	h.GetSystemSelfCheck(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		Summary map[string]int  `json:"summary"`
		Checks  []selfCheckItem `json:"checks"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload.Checks) < 5 {
		t.Fatalf("checks length = %d, want >= 5", len(payload.Checks))
	}
	if payload.Summary["ok"] == 0 {
		t.Fatalf("summary.ok = %d, want > 0", payload.Summary["ok"])
	}
}

func TestGetUsagePersistenceStatusReportsRuntimeState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	filePath := filepath.Join(t.TempDir(), "usage-statistics.json")
	manager := usage.DefaultPersistenceManager()
	manager.SetFilePath(filePath)
	usage.GetRequestStatistics().MergeSnapshot(usage.StatisticsSnapshot{
		APIs: map[string]usage.APISnapshot{
			"persist-check": {
				Models: map[string]usage.ModelSnapshot{
					"gpt-5": {
						Details: []usage.RequestDetail{
							{
								Timestamp: time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC),
								Tokens: usage.TokenStats{
									InputTokens:  3,
									OutputTokens: 2,
									TotalTokens:  5,
								},
							},
						},
					},
				},
			},
		},
	})
	usage.MarkDefaultPersistenceDirty()
	if err := usage.FlushDefaultPersistence(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	t.Cleanup(func() {
		manager.SetFilePath("")
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/usage/persistence-status", nil)

	h := &Handler{}
	h.GetUsagePersistenceStatus(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload persistenceStatusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !payload.Enabled {
		t.Fatalf("Enabled = false, want true")
	}
	if payload.FilePath != filePath {
		t.Fatalf("FilePath = %q, want %q", payload.FilePath, filePath)
	}
}

func TestCheckLogDirSkipsWhenLoggingToFileDisabled(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{
			LoggingToFile: false,
		},
		logDir: filepath.Join(t.TempDir(), "missing-logs"),
	}

	item := h.checkLogDir()
	if item.Status != selfCheckStatusOK {
		t.Fatalf("Status = %q, want %q", item.Status, selfCheckStatusOK)
	}
	if item.Message != "文件日志已关闭，已跳过目录检查" {
		t.Fatalf("Message = %q, want %q", item.Message, "文件日志已关闭，已跳过目录检查")
	}
}
