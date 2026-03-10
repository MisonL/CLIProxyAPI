package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func TestExportUsageStatisticsReturnsSnapshotPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stats := usage.NewRequestStatistics()
	stats.MergeSnapshot(usage.StatisticsSnapshot{
		APIs: map[string]usage.APISnapshot{
			"test-key": {
				Models: map[string]usage.ModelSnapshot{
					"gpt-5": {
						Details: []usage.RequestDetail{
							{
								Timestamp:    time.Date(2026, 3, 7, 21, 30, 0, 0, time.UTC),
								Source:       "codex",
								SelectionKey: "0",
								Tokens: usage.TokenStats{
									InputTokens:  10,
									OutputTokens: 5,
									TotalTokens:  15,
								},
							},
						},
					},
				},
			},
		},
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/usage/export", nil)

	h := &Handler{usageStats: stats}
	h.ExportUsageStatistics(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload usage.ExportPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Version != usage.SnapshotVersion {
		t.Fatalf("payload.Version = %d, want %d", payload.Version, usage.SnapshotVersion)
	}
	if payload.Usage.TotalRequests != 1 {
		t.Fatalf("payload.Usage.TotalRequests = %d, want 1", payload.Usage.TotalRequests)
	}
	if payload.Usage.TotalTokens != 15 {
		t.Fatalf("payload.Usage.TotalTokens = %d, want 15", payload.Usage.TotalTokens)
	}
}

func TestImportUsageStatisticsMergesSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	body := bytes.NewBufferString(`{
  "version": 2,
  "usage": {
    "apis": {
      "test-key": {
        "models": {
          "gpt-5": {
            "details": [
              {
                "timestamp": "2026-03-07T22:00:00Z",
                "source": "codex",
                "selection_key": "1",
                "tokens": {
                  "input_tokens": 8,
                  "output_tokens": 4,
                  "total_tokens": 12
                },
                "failed": false
              }
            ]
          }
        }
      }
    }
  }
}`)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/usage/import", body)
	ctx.Request.Header.Set("Content-Type", "application/json")

	stats := usage.NewRequestStatistics()
	h := &Handler{usageStats: stats}
	h.ImportUsageStatistics(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := int(response["added"].(float64)); got != 1 {
		t.Fatalf("added = %d, want 1", got)
	}

	snapshot := stats.Snapshot()
	if snapshot.TotalRequests != 1 {
		t.Fatalf("TotalRequests = %d, want 1", snapshot.TotalRequests)
	}
	if snapshot.TotalTokens != 12 {
		t.Fatalf("TotalTokens = %d, want 12", snapshot.TotalTokens)
	}
}

func TestImportUsageStatisticsAcceptsLegacyV1Snapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	body := bytes.NewBufferString(`{
  "version": 1,
  "usage": {
    "apis": {
      "test-key": {
        "models": {
          "gpt-5": {
            "details": [
              {
                "timestamp": "2026-03-07T22:00:00Z",
                "source": "codex",
                "auth_index": "legacy-1",
                "tokens": {
                  "input_tokens": 8,
                  "output_tokens": 4,
                  "total_tokens": 12
                },
                "failed": false
              }
            ]
          }
        }
      }
    }
  }
}`)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/usage/import", body)
	ctx.Request.Header.Set("Content-Type", "application/json")

	stats := usage.NewRequestStatistics()
	h := &Handler{usageStats: stats}
	h.ImportUsageStatistics(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	snapshot := stats.Snapshot()
	modelSnapshot := snapshot.APIs["test-key"].Models["gpt-5"]
	if len(modelSnapshot.Details) != 1 {
		t.Fatalf("details length = %d, want 1", len(modelSnapshot.Details))
	}
	if modelSnapshot.Details[0].SelectionKey != "legacy-1" {
		t.Fatalf("SelectionKey = %q, want %q", modelSnapshot.Details[0].SelectionKey, "legacy-1")
	}
}

func TestImportUsageStatisticsRejectsUnsupportedVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/usage/import",
		bytes.NewBufferString(`{"version":99,"usage":{}}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	h := &Handler{usageStats: usage.NewRequestStatistics()}
	h.ImportUsageStatistics(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if response["error"] != "unsupported version" {
		t.Fatalf("error = %q, want %q", response["error"], "unsupported version")
	}
}
