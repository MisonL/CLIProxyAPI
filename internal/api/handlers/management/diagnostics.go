package management

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"context"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/managementasset"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

type selfCheckStatus string

const (
	selfCheckStatusOK    selfCheckStatus = "ok"
	selfCheckStatusWarn  selfCheckStatus = "warn"
	selfCheckStatusError selfCheckStatus = "error"
)

type selfCheckItem struct {
	ID         string          `json:"id"`
	Status     selfCheckStatus `json:"status"`
	Title      string          `json:"title"`
	Message    string          `json:"message"`
	Details    string          `json:"details,omitempty"`
	Suggestion string          `json:"suggestion,omitempty"`
}

type persistenceStatusResponse struct {
	Enabled         bool      `json:"enabled"`
	FilePath        string    `json:"file_path"`
	FileExists      bool      `json:"file_exists"`
	FileSizeBytes   int64     `json:"file_size_bytes"`
	LastModifiedAt  time.Time `json:"last_modified_at,omitempty"`
	LastFlushAt     time.Time `json:"last_flush_at,omitempty"`
	LastLoadAt      time.Time `json:"last_load_at,omitempty"`
	LastLoadAdded   int64     `json:"last_load_added"`
	LastLoadSkipped int64     `json:"last_load_skipped"`
	LastError       string    `json:"last_error,omitempty"`
	LastErrorAt     time.Time `json:"last_error_at,omitempty"`
}

func (h *Handler) GetSystemSelfCheck(c *gin.Context) {
	items := h.buildSelfCheckItems()

	summary := gin.H{"ok": 0, "warn": 0, "error": 0}
	for _, item := range items {
		switch item.Status {
		case selfCheckStatusOK:
			summary["ok"] = summary["ok"].(int) + 1
		case selfCheckStatusWarn:
			summary["warn"] = summary["warn"].(int) + 1
		case selfCheckStatusError:
			summary["error"] = summary["error"].(int) + 1
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"summary": summary,
		"checks":  items,
	})
}

func (h *Handler) GetUsagePersistenceStatus(c *gin.Context) {
	manager := usage.DefaultPersistenceManager()
	status := persistenceStatusResponse{}
	if manager == nil {
		c.JSON(http.StatusOK, status)
		return
	}

	status.Enabled = manager.Enabled()
	status.FilePath = manager.FilePath()
	runtimeStatus := manager.Status()
	status.LastFlushAt = runtimeStatus.LastFlushAt
	status.LastLoadAt = runtimeStatus.LastLoadAt
	status.LastLoadAdded = runtimeStatus.LastLoadAdded
	status.LastLoadSkipped = runtimeStatus.LastLoadSkipped
	status.LastError = runtimeStatus.LastError
	status.LastErrorAt = runtimeStatus.LastErrorAt

	if status.FilePath != "" {
		if info, err := os.Stat(status.FilePath); err == nil {
			status.FileExists = true
			status.FileSizeBytes = info.Size()
			status.LastModifiedAt = info.ModTime().UTC()
		}
	}

	c.JSON(http.StatusOK, status)
}

func (h *Handler) buildSelfCheckItems() []selfCheckItem {
	items := []selfCheckItem{
		h.checkConfigFile(),
		h.checkPlatformRuntime(),
		h.checkLogDir(),
		h.checkManagementAsset(),
		h.checkUsagePersistence(),
		h.checkRemoteManagement(),
	}
	return items
}

func (h *Handler) checkConfigFile() selfCheckItem {
	path := strings.TrimSpace(h.configFilePath)
	if path == "" {
		return selfCheckItem{
			ID:      "config-file",
			Status:  selfCheckStatusWarn,
			Title:   "配置文件",
			Message: "当前未记录 config.yaml 路径",
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return selfCheckItem{
			ID:         "config-file",
			Status:     selfCheckStatusError,
			Title:      "配置文件",
			Message:    "未找到配置文件",
			Details:    path,
			Suggestion: "检查 Compose 挂载和 config.yaml 路径",
		}
	}
	if info.IsDir() {
		return selfCheckItem{
			ID:         "config-file",
			Status:     selfCheckStatusError,
			Title:      "配置文件",
			Message:    "配置路径指向目录而不是文件",
			Details:    path,
			Suggestion: "确认 /CLIProxyAPI/config.yaml 挂载的是文件",
		}
	}
	return selfCheckItem{
		ID:      "config-file",
		Status:  selfCheckStatusOK,
		Title:   "配置文件",
		Message: "配置文件可访问",
		Details: path,
	}
}

func (h *Handler) checkPlatformRuntime() selfCheckItem {
	if h == nil || h.platformRuntime == nil {
		return selfCheckItem{
			ID:         "platform-runtime",
			Status:     selfCheckStatusError,
			Title:      "平台运行时",
			Message:    "平台运行时未启用",
			Suggestion: "检查 CPA_PLATFORM_* 环境变量和平台依赖服务",
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.platformRuntime.HealthCheck(ctx); err != nil {
		return selfCheckItem{
			ID:         "platform-runtime",
			Status:     selfCheckStatusError,
			Title:      "平台运行时",
			Message:    "平台运行时健康检查失败",
			Details:    err.Error(),
			Suggestion: "检查 PostgreSQL、Redis、NATS 连接状态",
		}
	}
	status := h.platformRuntime.Status()
	return selfCheckItem{
		ID:      "platform-runtime",
		Status:  selfCheckStatusOK,
		Title:   "平台运行时",
		Message: "平台运行时健康",
		Details: strings.TrimSpace(status.Role) + " | " + strings.TrimSpace(status.DatabaseSchema) + " | " + strings.TrimSpace(status.TenantSlug) + "/" + strings.TrimSpace(status.WorkspaceSlug),
	}
}

func (h *Handler) checkLogDir() selfCheckItem {
	if h != nil && h.cfg != nil && !h.cfg.LoggingToFile {
		return selfCheckItem{
			ID:      "log-dir",
			Status:  selfCheckStatusOK,
			Title:   "日志目录",
			Message: "文件日志已关闭，已跳过目录检查",
		}
	}
	logDir := strings.TrimSpace(h.logDir)
	if logDir == "" && h.configFilePath != "" {
		logDir = filepath.Join(filepath.Dir(h.configFilePath), "logs")
	}
	if logDir == "" {
		return selfCheckItem{
			ID:      "log-dir",
			Status:  selfCheckStatusWarn,
			Title:   "日志目录",
			Message: "未能解析日志目录",
		}
	}
	return buildDirectoryCheckItem("log-dir", "日志目录", logDir, false)
}

func (h *Handler) checkManagementAsset() selfCheckItem {
	path := managementasset.FilePath(h.configFilePath)
	if path == "" {
		return selfCheckItem{
			ID:      "management-asset",
			Status:  selfCheckStatusWarn,
			Title:   "控制面板静态页",
			Message: "未配置控制面板静态页路径",
		}
	}

	override := strings.TrimSpace(os.Getenv("MANAGEMENT_STATIC_PATH"))
	info, err := os.Stat(path)
	if err != nil {
		message := "控制面板静态页不存在"
		if override != "" {
			message = "本地控制面板静态页不存在，将回退到 release 下载逻辑"
		}
		return selfCheckItem{
			ID:         "management-asset",
			Status:     selfCheckStatusWarn,
			Title:      "控制面板静态页",
			Message:    message,
			Details:    path,
			Suggestion: "确认 management.html 已构建或允许后端自动下载",
		}
	}
	if info.IsDir() {
		return selfCheckItem{
			ID:         "management-asset",
			Status:     selfCheckStatusError,
			Title:      "控制面板静态页",
			Message:    "控制面板路径指向目录而不是文件",
			Details:    path,
			Suggestion: "确认 MANAGEMENT_STATIC_PATH 或 static 目录结构",
		}
	}
	message := "控制面板静态页可访问"
	if override != "" {
		message = "本地控制面板静态页覆盖已生效"
	}
	return selfCheckItem{
		ID:      "management-asset",
		Status:  selfCheckStatusOK,
		Title:   "控制面板静态页",
		Message: message,
		Details: path,
	}
}

func (h *Handler) checkUsagePersistence() selfCheckItem {
	manager := usage.DefaultPersistenceManager()
	if manager == nil || !manager.Enabled() {
		return selfCheckItem{
			ID:      "usage-persistence",
			Status:  selfCheckStatusWarn,
			Title:   "Usage 持久化",
			Message: "当前为仅内存模式，未启用持久化",
		}
	}

	path := manager.FilePath()
	dir := filepath.Dir(path)
	item := buildDirectoryCheckItem("usage-persistence", "Usage 持久化", dir, false)
	if item.Status == selfCheckStatusOK {
		item.Message = "持久化目录可写"
		item.Details = path
	}
	return item
}

func (h *Handler) checkRemoteManagement() selfCheckItem {
	allowRemote := false
	disableControlPanel := false
	if h.cfg != nil {
		allowRemote = h.cfg.RemoteManagement.AllowRemote
		disableControlPanel = h.cfg.RemoteManagement.DisableControlPanel
	}

	status := selfCheckStatusOK
	message := "允许远程管理访问"
	if !allowRemote {
		status = selfCheckStatusWarn
		message = "仅允许本地访问管理接口"
	}
	if disableControlPanel {
		status = selfCheckStatusWarn
		message = "控制面板已禁用"
	}
	return selfCheckItem{
		ID:      "remote-management",
		Status:  status,
		Title:   "远程管理",
		Message: message,
	}
}

func buildDirectoryCheckItem(id, title, path string, requireExisting bool) selfCheckItem {
	info, err := os.Stat(path)
	if err != nil {
		status := selfCheckStatusWarn
		message := "目录不存在"
		if requireExisting {
			status = selfCheckStatusError
			message = "目录不存在，当前功能不可用"
		}
		return selfCheckItem{
			ID:         id,
			Status:     status,
			Title:      title,
			Message:    message,
			Details:    path,
			Suggestion: "确认宿主机挂载目录已创建",
		}
	}
	if !info.IsDir() {
		return selfCheckItem{
			ID:         id,
			Status:     selfCheckStatusError,
			Title:      title,
			Message:    "路径存在但不是目录",
			Details:    path,
			Suggestion: "确认挂载目标类型正确",
		}
	}
	if err := checkDirWritable(path); err != nil {
		return selfCheckItem{
			ID:         id,
			Status:     selfCheckStatusError,
			Title:      title,
			Message:    "目录不可写",
			Details:    path,
			Suggestion: "检查目录权限或容器挂载方式",
		}
	}
	return selfCheckItem{
		ID:      id,
		Status:  selfCheckStatusOK,
		Title:   title,
		Message: "目录存在且可写",
		Details: path,
	}
}

func checkDirWritable(dir string) error {
	file, err := os.CreateTemp(dir, ".self-check-*")
	if err != nil {
		return err
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return nil
}
