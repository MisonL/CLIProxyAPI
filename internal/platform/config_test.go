package platform

import "testing"

func TestParseBool(t *testing.T) {
	if !parseBool("true") {
		t.Fatal("expected true")
	}
	if parseBool("no") {
		t.Fatal("expected false")
	}
}

func TestLoadConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("CPA_PLATFORM_ENABLED", "")
	t.Setenv("CPA_PLATFORM_ROLE", "")
	t.Setenv("CPA_PLATFORM_DATABASE_URL", "postgresql://postgres:postgres@localhost:5432/cliproxy?sslmode=disable")
	t.Setenv("CPA_PLATFORM_REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("CPA_PLATFORM_NATS_URL", "nats://localhost:4222")
	t.Setenv("CPA_MASTER_KEY", "test-master-key")

	cfg := LoadConfigFromEnv()
	if !cfg.Enabled {
		t.Fatalf("expected Enabled=true")
	}
	if cfg.Role != "server" {
		t.Fatalf("expected role=server, got %q", cfg.Role)
	}
	if cfg.DatabaseSchema != defaultDatabaseSchema {
		t.Fatalf("expected schema=%q, got %q", defaultDatabaseSchema, cfg.DatabaseSchema)
	}
	if cfg.NATSStream != defaultNATSStream {
		t.Fatalf("expected nats stream=%q, got %q", defaultNATSStream, cfg.NATSStream)
	}
	if cfg.CachePrefix != defaultCachePrefix {
		t.Fatalf("expected cache prefix=%q, got %q", defaultCachePrefix, cfg.CachePrefix)
	}
	if cfg.TenantSlug != defaultTenantSlug || cfg.WorkspaceSlug != defaultWorkspaceSlug {
		t.Fatalf("unexpected tenant/workspace defaults: %q/%q", cfg.TenantSlug, cfg.WorkspaceSlug)
	}
	if cfg.MasterKey != "test-master-key" {
		t.Fatalf("expected master key from CPA_MASTER_KEY, got %q", cfg.MasterKey)
	}
}

func TestLoadConfigFromEnv_ReadsEnvValues(t *testing.T) {
	t.Setenv("CPA_PLATFORM_ENABLED", "true")
	t.Setenv("CPA_PLATFORM_ROLE", "worker")
	t.Setenv("CPA_PLATFORM_DATABASE_URL", "postgresql://example")
	t.Setenv("CPA_PLATFORM_DATABASE_SCHEMA", "custom")
	t.Setenv("CPA_PLATFORM_REDIS_URL", "redis://example")
	t.Setenv("CPA_PLATFORM_NATS_URL", "nats://example")
	t.Setenv("CPA_PLATFORM_NATS_STREAM", "CUSTOM_STREAM")
	t.Setenv("CPA_PLATFORM_CACHE_PREFIX", "cpa:test")
	t.Setenv("CPA_MASTER_KEY", "")
	t.Setenv("CPA_PLATFORM_MASTER_KEY", "fallback-key")
	t.Setenv("CPA_PLATFORM_TENANT_SLUG", "tenant-x")
	t.Setenv("CPA_PLATFORM_TENANT_NAME", "Tenant X")
	t.Setenv("CPA_PLATFORM_WORKSPACE_SLUG", "workspace-x")
	t.Setenv("CPA_PLATFORM_WORKSPACE_NAME", "Workspace X")

	cfg := LoadConfigFromEnv()
	if !cfg.Enabled {
		t.Fatalf("expected Enabled=true")
	}
	if cfg.Role != "worker" {
		t.Fatalf("expected role=worker, got %q", cfg.Role)
	}
	if cfg.DatabaseSchema != "custom" {
		t.Fatalf("expected schema=custom, got %q", cfg.DatabaseSchema)
	}
	if cfg.NATSStream != "CUSTOM_STREAM" {
		t.Fatalf("expected nats stream=CUSTOM_STREAM, got %q", cfg.NATSStream)
	}
	if cfg.CachePrefix != "cpa:test" {
		t.Fatalf("expected cache prefix=cpa:test, got %q", cfg.CachePrefix)
	}
	if cfg.MasterKey != "fallback-key" {
		t.Fatalf("expected master key from CPA_PLATFORM_MASTER_KEY, got %q", cfg.MasterKey)
	}
	if cfg.TenantSlug != "tenant-x" || cfg.TenantName != "Tenant X" {
		t.Fatalf("unexpected tenant values: %q/%q", cfg.TenantSlug, cfg.TenantName)
	}
	if cfg.WorkspaceSlug != "workspace-x" || cfg.WorkspaceName != "Workspace X" {
		t.Fatalf("unexpected workspace values: %q/%q", cfg.WorkspaceSlug, cfg.WorkspaceName)
	}
}
