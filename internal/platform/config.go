package platform

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultDatabaseSchema = "controlplane"
	defaultTenantSlug     = "default"
	defaultTenantName     = "Default Tenant"
	defaultWorkspaceSlug  = "default"
	defaultWorkspaceName  = "Default Workspace"
	defaultNATSStream     = "CPA_EVENTS"
	defaultCachePrefix    = "cpa:v2"
	defaultCacheTTL       = 30 * time.Second
)

type Config struct {
	Enabled        bool
	Role           string
	DatabaseURL    string
	DatabaseSchema string
	RedisURL       string
	NATSURL        string
	NATSStream     string
	MasterKey      string
	TenantSlug     string
	TenantName     string
	WorkspaceSlug  string
	WorkspaceName  string
	CachePrefix    string
	CacheTTL       time.Duration
}

func LoadConfigFromEnv() Config {
	cfg := Config{
		Role:           firstNonEmptyEnv(os.Getenv("CPA_PLATFORM_ROLE"), "server"),
		DatabaseURL:    strings.TrimSpace(os.Getenv("CPA_PLATFORM_DATABASE_URL")),
		DatabaseSchema: firstNonEmptyEnv(os.Getenv("CPA_PLATFORM_DATABASE_SCHEMA"), defaultDatabaseSchema),
		RedisURL:       strings.TrimSpace(os.Getenv("CPA_PLATFORM_REDIS_URL")),
		NATSURL:        strings.TrimSpace(os.Getenv("CPA_PLATFORM_NATS_URL")),
		NATSStream:     firstNonEmptyEnv(os.Getenv("CPA_PLATFORM_NATS_STREAM"), defaultNATSStream),
		MasterKey:      firstNonEmptyEnv(os.Getenv("CPA_MASTER_KEY"), os.Getenv("CPA_PLATFORM_MASTER_KEY")),
		TenantSlug:     firstNonEmptyEnv(os.Getenv("CPA_PLATFORM_TENANT_SLUG"), defaultTenantSlug),
		TenantName:     firstNonEmptyEnv(os.Getenv("CPA_PLATFORM_TENANT_NAME"), defaultTenantName),
		WorkspaceSlug:  firstNonEmptyEnv(os.Getenv("CPA_PLATFORM_WORKSPACE_SLUG"), defaultWorkspaceSlug),
		WorkspaceName:  firstNonEmptyEnv(os.Getenv("CPA_PLATFORM_WORKSPACE_NAME"), defaultWorkspaceName),
		CachePrefix:    firstNonEmptyEnv(os.Getenv("CPA_PLATFORM_CACHE_PREFIX"), defaultCachePrefix),
		CacheTTL:       defaultCacheTTL,
	}

	if raw := strings.TrimSpace(os.Getenv("CPA_PLATFORM_CACHE_TTL")); raw != "" {
		if ttl, err := time.ParseDuration(raw); err == nil && ttl > 0 {
			cfg.CacheTTL = ttl
		}
	}

	if raw := strings.TrimSpace(os.Getenv("CPA_PLATFORM_ENABLED")); raw != "" {
		cfg.Enabled = parseBool(raw)
	} else {
		cfg.Enabled = cfg.DatabaseURL != "" && cfg.RedisURL != "" && cfg.NATSURL != ""
	}

	return cfg
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	switch c.Role {
	case "", "server", "worker", "all":
	default:
		return fmt.Errorf("platform: unsupported role %q", c.Role)
	}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("platform: database URL is required")
	}
	if strings.TrimSpace(c.RedisURL) == "" {
		return fmt.Errorf("platform: redis URL is required")
	}
	if strings.TrimSpace(c.NATSURL) == "" {
		return fmt.Errorf("platform: NATS URL is required")
	}
	if strings.TrimSpace(c.MasterKey) == "" {
		return fmt.Errorf("platform: master key is required")
	}
	if strings.TrimSpace(c.DatabaseSchema) == "" {
		return fmt.Errorf("platform: database schema is required")
	}
	if strings.TrimSpace(c.TenantSlug) == "" || strings.TrimSpace(c.WorkspaceSlug) == "" {
		return fmt.Errorf("platform: tenant/workspace slug is required")
	}
	return nil
}

func (c Config) WorkerEnabled() bool {
	if !c.Enabled {
		return false
	}
	return c.Role == "worker" || c.Role == "all"
}

func (c Config) ServerEnabled() bool {
	if !c.Enabled {
		return false
	}
	return c.Role == "" || c.Role == "server" || c.Role == "all"
}

func firstNonEmptyEnv(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}
