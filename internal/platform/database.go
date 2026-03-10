package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var overviewColors = []string{"#8b8680", "#c65746", "#22c55e", "#d97706", "#2563eb", "#0f766e", "#be185d"}

type tenantWorkspace struct {
	tenantID    string
	workspaceID string
}

type store struct {
	cfg         Config
	pool        *pgxpool.Pool
	masterKey   []byte
	tenantID    string
	workspaceID string
}

func newStore(ctx context.Context, cfg Config) (*store, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("platform: parse database url: %w", err)
	}
	poolCfg.MaxConns = 12
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("platform: create pg pool: %w", err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("platform: ping database: %w", err)
	}
	s := &store{
		cfg:       cfg,
		pool:      pool,
		masterKey: []byte(cfg.MasterKey),
	}
	if err = s.ensureSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	ids, err := s.ensureTenantWorkspace(ctx)
	if err != nil {
		pool.Close()
		return nil, err
	}
	s.tenantID = ids.tenantID
	s.workspaceID = ids.workspaceID
	return s, nil
}

func (s *store) close() {
	if s == nil || s.pool == nil {
		return
	}
	s.pool.Close()
}

func (s *store) schemaName() string {
	return quoteIdentifier(s.cfg.DatabaseSchema)
}

func quoteIdentifier(value string) string {
	if !identifierPattern.MatchString(value) {
		return `"controlplane"`
	}
	return `"` + value + `"`
}

func (s *store) qualifiedTableName(name string) string {
	return s.schemaName() + "." + quoteIdentifier(name)
}

func (s *store) columnExists(ctx context.Context, tableName, columnName string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
	)`, strings.TrimSpace(s.cfg.DatabaseSchema), tableName, columnName).Scan(&exists)
	return exists, err
}

func (s *store) renameColumnIfExists(ctx context.Context, tableName, oldColumn, newColumn string) error {
	oldExists, err := s.columnExists(ctx, tableName, oldColumn)
	if err != nil {
		return err
	}
	newExists, err := s.columnExists(ctx, tableName, newColumn)
	if err != nil {
		return err
	}
	if !oldExists {
		return nil
	}
	if newExists {
		return fmt.Errorf("platform: schema migration conflict on %s.%s -> %s", tableName, oldColumn, newColumn)
	}
	_, err = s.pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE %s RENAME COLUMN %s TO %s`, s.qualifiedTableName(tableName), quoteIdentifier(oldColumn), quoteIdentifier(newColumn)))
	return err
}

func (s *store) migrateLegacyColumnNames(ctx context.Context) error {
	renames := []struct {
		tableName string
		oldColumn string
		newColumn string
	}{
		{tableName: "credentials", oldColumn: "source_auth_id", newColumn: "runtime_id"},
		{tableName: "credentials", oldColumn: "file_name", newColumn: "credential_name"},
		{tableName: "credentials", oldColumn: "auth_index", newColumn: "selection_key"},
		{tableName: "usage_events_raw", oldColumn: "auth_id", newColumn: "runtime_id"},
		{tableName: "usage_events_raw", oldColumn: "auth_index", newColumn: "selection_key"},
	}
	for _, item := range renames {
		if err := s.renameColumnIfExists(ctx, item.tableName, item.oldColumn, item.newColumn); err != nil {
			return fmt.Errorf("platform: migrate legacy column %s.%s: %w", item.tableName, item.oldColumn, err)
		}
	}
	return nil
}

func (s *store) ensureSchema(ctx context.Context) error {
	schema := strings.TrimSpace(s.cfg.DatabaseSchema)
	if !identifierPattern.MatchString(schema) {
		return fmt.Errorf("platform: invalid schema %q", schema)
	}
	if _, err := s.pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, s.schemaName())); err != nil {
		return fmt.Errorf("platform: ensure schema failed: %w", err)
	}
	if err := s.migrateLegacyColumnNames(ctx); err != nil {
		return err
	}
	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.tenants (
			id UUID PRIMARY KEY,
			slug TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.workspaces (
			id UUID PRIMARY KEY,
			tenant_id UUID NOT NULL REFERENCES %s.tenants(id),
			slug TEXT NOT NULL,
			name TEXT NOT NULL,
			is_default BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (tenant_id, slug)
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.users (
			id UUID PRIMARY KEY,
			tenant_id UUID NOT NULL REFERENCES %s.tenants(id),
			email TEXT NOT NULL,
			display_name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (tenant_id, email)
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.memberships (
			id UUID PRIMARY KEY,
			tenant_id UUID NOT NULL REFERENCES %s.tenants(id),
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			user_id UUID NOT NULL REFERENCES %s.users(id),
			role TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (workspace_id, user_id)
		)`, s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.api_keys (
			id UUID PRIMARY KEY,
			tenant_id UUID NOT NULL REFERENCES %s.tenants(id),
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL,
			last_used_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.schemaName(), s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.settings (
			id UUID PRIMARY KEY,
			tenant_id UUID NOT NULL REFERENCES %s.tenants(id),
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			name TEXT NOT NULL,
			value JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (workspace_id, name)
		)`, s.schemaName(), s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.provider_accounts (
			id UUID PRIMARY KEY,
			tenant_id UUID NOT NULL REFERENCES %s.tenants(id),
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			provider TEXT NOT NULL,
			account_key TEXT NOT NULL,
			account_email TEXT NOT NULL DEFAULT '',
			label TEXT NOT NULL DEFAULT '',
			metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (workspace_id, provider, account_key)
		)`, s.schemaName(), s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.credentials (
			id UUID PRIMARY KEY,
			tenant_id UUID NOT NULL REFERENCES %s.tenants(id),
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			provider_account_id UUID REFERENCES %s.provider_accounts(id),
			runtime_id TEXT NOT NULL,
			credential_name TEXT NOT NULL,
			selection_key TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			status_message TEXT NOT NULL DEFAULT '',
			disabled BOOLEAN NOT NULL DEFAULT FALSE,
			unavailable BOOLEAN NOT NULL DEFAULT FALSE,
			runtime_only BOOLEAN NOT NULL DEFAULT FALSE,
			last_refresh_at TIMESTAMPTZ,
			secret_sha256 TEXT NOT NULL DEFAULT '',
			active_secret_version_id UUID,
			metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMPTZ,
			UNIQUE (workspace_id, runtime_id)
		)`, s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.credential_secret_versions (
			id UUID PRIMARY KEY,
			tenant_id UUID NOT NULL REFERENCES %s.tenants(id),
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			credential_id UUID NOT NULL REFERENCES %s.credentials(id),
			version INTEGER NOT NULL,
			content_sha256 TEXT NOT NULL,
			ciphertext BYTEA NOT NULL,
			cipher_nonce BYTEA NOT NULL,
			encrypted_dek BYTEA NOT NULL,
			dek_nonce BYTEA NOT NULL,
			algorithm TEXT NOT NULL,
			key_version TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (credential_id, version)
		)`, s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.usage_events_raw (
			id BIGSERIAL PRIMARY KEY,
			tenant_id UUID NOT NULL REFERENCES %s.tenants(id),
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			credential_id UUID REFERENCES %s.credentials(id),
			event_key TEXT NOT NULL,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			runtime_id TEXT NOT NULL DEFAULT '',
			selection_key TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			requested_at TIMESTAMPTZ NOT NULL,
			failed BOOLEAN NOT NULL,
			input_tokens BIGINT NOT NULL DEFAULT 0,
			output_tokens BIGINT NOT NULL DEFAULT 0,
			reasoning_tokens BIGINT NOT NULL DEFAULT 0,
			cached_tokens BIGINT NOT NULL DEFAULT 0,
			total_tokens BIGINT NOT NULL DEFAULT 0,
			meta JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (workspace_id, event_key)
		)`, s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.quota_observations_raw (
			id BIGSERIAL PRIMARY KEY,
			tenant_id UUID NOT NULL REFERENCES %s.tenants(id),
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			credential_id UUID NOT NULL REFERENCES %s.credentials(id),
			provider TEXT NOT NULL,
			dimension_id TEXT NOT NULL,
			dimension_label TEXT NOT NULL,
			remaining_ratio DOUBLE PRECISION,
			reset_at TIMESTAMPTZ,
			window_seconds INTEGER,
			state TEXT NOT NULL DEFAULT 'ok',
			raw_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.quota_dimensions_latest (
			credential_id UUID NOT NULL REFERENCES %s.credentials(id),
			provider TEXT NOT NULL,
			dimension_id TEXT NOT NULL,
			dimension_label TEXT NOT NULL,
			remaining_ratio DOUBLE PRECISION,
			reset_at TIMESTAMPTZ,
			window_seconds INTEGER,
			state TEXT NOT NULL DEFAULT 'ok',
			raw_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (credential_id, dimension_id)
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.usage_rollup_5m (
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			provider TEXT NOT NULL,
			credential_key TEXT NOT NULL DEFAULT '',
			bucket_start TIMESTAMPTZ NOT NULL,
			request_count BIGINT NOT NULL DEFAULT 0,
			failure_count BIGINT NOT NULL DEFAULT 0,
			token_count BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (workspace_id, provider, credential_key, bucket_start)
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.usage_rollup_1h (
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			provider TEXT NOT NULL,
			credential_key TEXT NOT NULL DEFAULT '',
			bucket_start TIMESTAMPTZ NOT NULL,
			request_count BIGINT NOT NULL DEFAULT 0,
			failure_count BIGINT NOT NULL DEFAULT 0,
			token_count BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (workspace_id, provider, credential_key, bucket_start)
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.usage_rollup_1d (
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			provider TEXT NOT NULL,
			credential_key TEXT NOT NULL DEFAULT '',
			bucket_start TIMESTAMPTZ NOT NULL,
			request_count BIGINT NOT NULL DEFAULT 0,
			failure_count BIGINT NOT NULL DEFAULT 0,
			token_count BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (workspace_id, provider, credential_key, bucket_start)
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.provider_health_snapshots (
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			provider TEXT NOT NULL,
			mode TEXT NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (workspace_id, provider)
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.provider_histogram_buckets (
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			provider TEXT NOT NULL,
			dataset_id TEXT NOT NULL,
			bucket_index INTEGER NOT NULL,
			count BIGINT NOT NULL DEFAULT 0,
			average_remaining DOUBLE PRECISION,
			generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (workspace_id, provider, dataset_id, bucket_index)
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.credential_activity_snapshots (
			credential_id UUID PRIMARY KEY REFERENCES %s.credentials(id),
			last_seen_at TIMESTAMPTZ,
			requests_24h BIGINT NOT NULL DEFAULT 0,
			requests_7d BIGINT NOT NULL DEFAULT 0,
			failures_24h BIGINT NOT NULL DEFAULT 0,
			failure_rate_24h DOUBLE PRECISION NOT NULL DEFAULT 0,
			total_tokens_24h BIGINT NOT NULL DEFAULT 0,
			total_tokens_7d BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.credential_quota_snapshots (
			credential_id UUID PRIMARY KEY REFERENCES %s.credentials(id),
			provider TEXT NOT NULL,
			mode TEXT NOT NULL DEFAULT 'usage-only',
			health_percent DOUBLE PRECISION,
			conservative_risk_days DOUBLE PRECISION,
			avg_daily_quota_burn_percent DOUBLE PRECISION,
			quota_exceeded BOOLEAN NOT NULL DEFAULT FALSE,
			summary JSONB NOT NULL DEFAULT '{}'::jsonb,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.jobs (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			kind TEXT NOT NULL,
			state TEXT NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			result JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.imports (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			source TEXT NOT NULL,
			state TEXT NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.exports (
			id UUID PRIMARY KEY,
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			kind TEXT NOT NULL,
			state TEXT NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.audit_logs (
			id BIGSERIAL PRIMARY KEY,
			tenant_id UUID NOT NULL REFERENCES %s.tenants(id),
			workspace_id UUID NOT NULL REFERENCES %s.workspaces(id),
			action TEXT NOT NULL,
			actor TEXT NOT NULL,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.schemaName(), s.schemaName(), s.schemaName()),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_usage_rollup_1h_provider_bucket ON %s.usage_rollup_1h (workspace_id, provider, bucket_start DESC)`, schema, s.schemaName()),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_usage_rollup_5m_provider_bucket ON %s.usage_rollup_5m (workspace_id, provider, bucket_start DESC)`, schema, s.schemaName()),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_credentials_provider_active ON %s.credentials (workspace_id, provider, deleted_at)`, schema, s.schemaName()),
		fmt.Sprintf(`ALTER TABLE %s.credential_activity_snapshots ADD COLUMN IF NOT EXISTS failures_24h BIGINT NOT NULL DEFAULT 0`, s.schemaName()),
		fmt.Sprintf(`ALTER TABLE %s.usage_events_raw ADD COLUMN IF NOT EXISTS request_id TEXT NOT NULL DEFAULT ''`, s.schemaName()),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_usage_events_request_id ON %s.usage_events_raw (workspace_id, request_id, requested_at DESC)`, schema, s.schemaName()),
	}
	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("platform: ensure schema failed: %w", err)
		}
	}
	return nil
}

func (s *store) ensureTenantWorkspace(ctx context.Context) (tenantWorkspace, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return tenantWorkspace{}, err
	}
	defer tx.Rollback(ctx)

	tenantID := uuid.NewString()
	if _, err = tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.tenants (id, slug, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()
	`, s.schemaName()), tenantID, s.cfg.TenantSlug, s.cfg.TenantName); err != nil {
		return tenantWorkspace{}, err
	}
	if err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT id FROM %s.tenants WHERE slug = $1`, s.schemaName()), s.cfg.TenantSlug).Scan(&tenantID); err != nil {
		return tenantWorkspace{}, err
	}

	workspaceID := uuid.NewString()
	if _, err = tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.workspaces (id, tenant_id, slug, name, is_default)
		VALUES ($1, $2, $3, $4, TRUE)
		ON CONFLICT (tenant_id, slug) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()
	`, s.schemaName()), workspaceID, tenantID, s.cfg.WorkspaceSlug, s.cfg.WorkspaceName); err != nil {
		return tenantWorkspace{}, err
	}
	if err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT id FROM %s.workspaces WHERE tenant_id = $1 AND slug = $2`, s.schemaName()), tenantID, s.cfg.WorkspaceSlug).Scan(&workspaceID); err != nil {
		return tenantWorkspace{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return tenantWorkspace{}, err
	}
	return tenantWorkspace{tenantID: tenantID, workspaceID: workspaceID}, nil
}

func (s *store) upsertAuth(ctx context.Context, auth *coreauth.Auth) error {
	_, err := s.upsertAuthWithID(ctx, auth)
	return err
}

func (s *store) upsertAuthWithID(ctx context.Context, auth *coreauth.Auth) (string, error) {
	if auth == nil {
		return "", nil
	}
	accountKey, accountEmail := authAccountInfo(auth)
	rawContent, err := loadAuthRawContent(auth)
	if err != nil {
		return "", err
	}
	sealed, err := SealSecret(s.masterKey, rawContent)
	if err != nil {
		return "", err
	}
	metadataJSON, err := json.Marshal(map[string]any{
		"attributes": auth.Attributes,
		"metadata":   auth.Metadata,
		"proxy_url":  auth.ProxyURL,
		"prefix":     auth.Prefix,
	})
	if err != nil {
		return "", err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	accountID := uuid.NewString()
	if _, err = tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.provider_accounts (id, tenant_id, workspace_id, provider, account_key, account_email, label, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, '{}'::jsonb)
		ON CONFLICT (workspace_id, provider, account_key)
		DO UPDATE SET account_email = EXCLUDED.account_email, label = EXCLUDED.label, updated_at = NOW()
	`, s.schemaName()),
		accountID, s.tenantID, s.workspaceID, normalizeProviderOrUnknown(auth.Provider), accountKey, accountEmail, strings.TrimSpace(auth.Label),
	); err != nil {
		return "", err
	}
	if err = tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT id FROM %s.provider_accounts
		WHERE workspace_id = $1 AND provider = $2 AND account_key = $3
	`, s.schemaName()), s.workspaceID, normalizeProviderOrUnknown(auth.Provider), accountKey).Scan(&accountID); err != nil {
		return "", err
	}

	credentialID := uuid.NewString()
	var currentSecretHash string
	var currentVersion int
	if err = tx.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO %s.credentials (
			id, tenant_id, workspace_id, provider_account_id, runtime_id, credential_name, selection_key, provider,
			label, status, status_message, disabled, unavailable, runtime_only, last_refresh_at, metadata, deleted_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15, $16::jsonb, NULL
		)
		ON CONFLICT (workspace_id, runtime_id)
		DO UPDATE SET
			provider_account_id = EXCLUDED.provider_account_id,
			credential_name = EXCLUDED.credential_name,
			selection_key = EXCLUDED.selection_key,
			provider = EXCLUDED.provider,
			label = EXCLUDED.label,
			status = EXCLUDED.status,
			status_message = EXCLUDED.status_message,
			disabled = EXCLUDED.disabled,
			unavailable = EXCLUDED.unavailable,
			runtime_only = EXCLUDED.runtime_only,
			last_refresh_at = EXCLUDED.last_refresh_at,
			metadata = EXCLUDED.metadata,
			updated_at = NOW(),
			deleted_at = NULL
		RETURNING id, secret_sha256, COALESCE((SELECT MAX(version) FROM %s.credential_secret_versions WHERE credential_id = %s.credentials.id), 0)
	`, s.schemaName(), s.schemaName(), s.schemaName()),
		credentialID, s.tenantID, s.workspaceID, accountID, auth.ID, authFileName(auth), auth.EnsureSelectionKey(),
		normalizeProviderOrUnknown(auth.Provider), strings.TrimSpace(auth.Label), string(auth.Status), strings.TrimSpace(auth.StatusMessage),
		auth.Disabled, auth.Unavailable, isRuntimeOnlyAuth(auth), nullableTime(auth.LastRefreshedAt), string(metadataJSON),
	).Scan(&credentialID, &currentSecretHash, &currentVersion); err != nil {
		return "", err
	}

	if currentSecretHash != sealed.ContentSHA256 {
		secretVersionID := uuid.NewString()
		nextVersion := currentVersion + 1
		if _, err = tx.Exec(ctx, fmt.Sprintf(`
			INSERT INTO %s.credential_secret_versions (
				id, tenant_id, workspace_id, credential_id, version, content_sha256,
				ciphertext, cipher_nonce, encrypted_dek, dek_nonce, algorithm, key_version
			) VALUES (
				$1, $2, $3, $4, $5, $6,
				$7, $8, $9, $10, $11, $12
			)
		`, s.schemaName()),
			secretVersionID, s.tenantID, s.workspaceID, credentialID, nextVersion, sealed.ContentSHA256,
			sealed.Ciphertext, sealed.CipherNonce, sealed.EncryptedDEK, sealed.DEKNonce, sealed.Algorithm, sealed.KeyVersion,
		); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, fmt.Sprintf(`
			UPDATE %s.credentials
			SET active_secret_version_id = $2, secret_sha256 = $3, updated_at = NOW()
			WHERE id = $1
		`, s.schemaName()), credentialID, secretVersionID, sealed.ContentSHA256); err != nil {
			return "", err
		}
	}

	if err = s.upsertQuotaSnapshotTx(ctx, tx, credentialID, auth); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return credentialID, nil
}

func (s *store) markAuthDeleted(ctx context.Context, authID string) error {
	if strings.TrimSpace(authID) == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, fmt.Sprintf(`
		UPDATE %s.credentials
		SET deleted_at = NOW(), status = 'deleted', updated_at = NOW()
		WHERE workspace_id = $1 AND runtime_id = $2
	`, s.schemaName()), s.workspaceID, authID)
	return err
}

func (s *store) upsertQuotaSnapshotTx(ctx context.Context, tx pgx.Tx, credentialID string, auth *coreauth.Auth) error {
	mode := "usage-only"
	var health *float64
	var risk *float64
	var burn *float64
	quotaExceeded := auth.Quota.Exceeded

	if auth.Disabled {
		zero := 0.0
		health = &zero
	} else if quotaExceeded {
		zero := 0.0
		health = &zero
		mode = "quota"
		if !auth.Quota.NextRecoverAt.IsZero() {
			days := math.Max(0, auth.Quota.NextRecoverAt.Sub(time.Now().UTC()).Hours()/24)
			risk = &days
		}
	} else if auth.Unavailable {
		value := 20.0
		health = &value
	}

	summary := map[string]any{
		"status":         auth.Status,
		"status_message": auth.StatusMessage,
		"quota_reason":   auth.Quota.Reason,
		"backoff_level":  auth.Quota.BackoffLevel,
		"unavailable":    auth.Unavailable,
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return err
	}

	if _, err = tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.credential_quota_snapshots (
			credential_id, provider, mode, health_percent, conservative_risk_days,
			avg_daily_quota_burn_percent, quota_exceeded, summary, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, NOW())
		ON CONFLICT (credential_id)
		DO UPDATE SET
			provider = EXCLUDED.provider,
			mode = EXCLUDED.mode,
			health_percent = EXCLUDED.health_percent,
			conservative_risk_days = EXCLUDED.conservative_risk_days,
			avg_daily_quota_burn_percent = EXCLUDED.avg_daily_quota_burn_percent,
			quota_exceeded = EXCLUDED.quota_exceeded,
			summary = EXCLUDED.summary,
			updated_at = NOW()
	`, s.schemaName()), credentialID, normalizeProviderOrUnknown(auth.Provider), mode, health, risk, burn, quotaExceeded, string(summaryJSON)); err != nil {
		return err
	}

	if quotaExceeded {
		rawPayload, errMarshal := json.Marshal(map[string]any{
			"reason":          auth.Quota.Reason,
			"next_recover_at": auth.Quota.NextRecoverAt,
			"backoff_level":   auth.Quota.BackoffLevel,
		})
		if errMarshal != nil {
			return errMarshal
		}
		if _, err = tx.Exec(ctx, fmt.Sprintf(`
			INSERT INTO %s.quota_dimensions_latest (
				credential_id, provider, dimension_id, dimension_label, remaining_ratio,
				reset_at, window_seconds, state, raw_payload, observed_at
			) VALUES ($1, $2, 'cooldown', 'Cooldown', 0, $3, NULL, 'cooldown', $4::jsonb, NOW())
			ON CONFLICT (credential_id, dimension_id)
			DO UPDATE SET
				provider = EXCLUDED.provider,
				remaining_ratio = EXCLUDED.remaining_ratio,
				reset_at = EXCLUDED.reset_at,
				window_seconds = EXCLUDED.window_seconds,
				state = EXCLUDED.state,
				raw_payload = EXCLUDED.raw_payload,
				observed_at = NOW()
		`, s.schemaName()), credentialID, normalizeProviderOrUnknown(auth.Provider), nullableTime(auth.Quota.NextRecoverAt), string(rawPayload)); err != nil {
			return err
		}
		return nil
	}

	_, err = tx.Exec(ctx, fmt.Sprintf(`
		DELETE FROM %s.quota_dimensions_latest
		WHERE credential_id = $1 AND dimension_id = 'cooldown'
	`, s.schemaName()), credentialID)
	return err
}

func (s *store) ingestUsageEvent(ctx context.Context, event UsageEvent) error {
	credentialID, _ := s.lookupCredentialID(ctx, event.AuthID, event.SelectionKey, event.Provider)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.usage_events_raw (
			tenant_id, workspace_id, credential_id, event_key, provider, model, runtime_id, selection_key, source, request_id,
			requested_at, failed, input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17
		) ON CONFLICT (workspace_id, event_key) DO NOTHING
	`, s.schemaName()),
		s.tenantID, s.workspaceID, nullableString(credentialID), event.EventKey, normalizeProviderOrUnknown(event.Provider), strings.TrimSpace(event.Model),
		strings.TrimSpace(event.AuthID), strings.TrimSpace(event.SelectionKey), strings.TrimSpace(event.Source), strings.TrimSpace(event.RequestID), event.RequestedAt.UTC(), event.Failed,
		event.InputTokens, event.OutputTokens, event.ReasoningTokens, event.CachedTokens, event.TotalTokens,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}

	if err = s.upsertRollupTx(ctx, tx, "usage_rollup_5m", bucketTime(event.RequestedAt, 5*time.Minute), event, credentialID); err != nil {
		return err
	}
	if err = s.upsertRollupTx(ctx, tx, "usage_rollup_1h", bucketTime(event.RequestedAt, time.Hour), event, credentialID); err != nil {
		return err
	}
	if err = s.upsertRollupTx(ctx, tx, "usage_rollup_1d", bucketTime(event.RequestedAt, 24*time.Hour), event, credentialID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *store) getTraceByRequestID(ctx context.Context, requestID string) (RequestTraceResult, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return RequestTraceResult{}, fmt.Errorf("platform: request_id is required")
	}

	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			u.event_key,
			u.request_id,
			u.provider,
			u.model,
			u.runtime_id,
			u.selection_key,
			u.source,
			COALESCE(c.id::text, ''),
			COALESCE(c.runtime_id, ''),
			COALESCE(c.credential_name, ''),
			COALESCE(c.label, ''),
			u.requested_at,
			u.failed,
			u.input_tokens,
			u.output_tokens,
			u.reasoning_tokens,
			u.cached_tokens,
			u.total_tokens
		FROM %s.usage_events_raw u
		LEFT JOIN %s.credentials c ON c.id = u.credential_id
		WHERE u.workspace_id = $1
		  AND u.request_id = $2
		ORDER BY u.requested_at DESC, u.id DESC
	`, s.schemaName(), s.schemaName()), s.workspaceID, requestID)
	if err != nil {
		return RequestTraceResult{}, err
	}
	defer rows.Close()

	result := RequestTraceResult{
		RequestID: requestID,
		Items:     make([]RequestTraceEvent, 0, 4),
	}
	for rows.Next() {
		var item RequestTraceEvent
		if err = rows.Scan(
			&item.EventKey,
			&item.RequestID,
			&item.Provider,
			&item.Model,
			&item.AuthID,
			&item.SelectionKey,
			&item.Source,
			&item.CredentialID,
			&item.SourceAuthID,
			&item.FileName,
			&item.Label,
			&item.RequestedAt,
			&item.Failed,
			&item.InputTokens,
			&item.OutputTokens,
			&item.ReasoningTokens,
			&item.CachedTokens,
			&item.TotalTokens,
		); err != nil {
			return RequestTraceResult{}, err
		}
		item.SourceDisplayName = strings.TrimSpace(item.FileName)
		if item.SourceDisplayName == "" {
			item.SourceDisplayName = strings.TrimSpace(item.Label)
		}
		if item.SourceDisplayName == "" {
			item.SourceDisplayName = strings.TrimSpace(item.Source)
		}
		if item.SourceDisplayName == "" {
			item.SourceDisplayName = "-"
		}
		item.SourceType = strings.TrimSpace(item.Provider)
		result.Items = append(result.Items, item)
	}
	if err = rows.Err(); err != nil {
		return RequestTraceResult{}, err
	}
	return result, nil
}

func (s *store) upsertRollupTx(ctx context.Context, tx pgx.Tx, table string, bucketStart time.Time, event UsageEvent, credentialID string) error {
	_, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.%s (workspace_id, provider, credential_key, bucket_start, request_count, failure_count, token_count)
		VALUES ($1, $2, $3, $4, 1, $5, $6)
		ON CONFLICT (workspace_id, provider, credential_key, bucket_start)
		DO UPDATE SET
			request_count = %s.%s.request_count + 1,
			failure_count = %s.%s.failure_count + EXCLUDED.failure_count,
			token_count = %s.%s.token_count + EXCLUDED.token_count
	`, s.schemaName(), quoteIdentifier(table), s.schemaName(), quoteIdentifier(table), s.schemaName(), quoteIdentifier(table), s.schemaName(), quoteIdentifier(table)),
		s.workspaceID, normalizeProviderOrUnknown(event.Provider), normalizeRollupCredentialKey(credentialID), bucketStart.UTC(),
		boolToInt64(event.Failed), event.TotalTokens,
	)
	return err
}

func (s *store) lookupCredentialID(ctx context.Context, authID, selectionKey, provider string) (string, error) {
	var credentialID string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT id
		FROM %s.credentials
		WHERE workspace_id = $1
		  AND provider = $2
		  AND deleted_at IS NULL
		  AND (runtime_id = $3 OR selection_key = $4)
		LIMIT 1
	`, s.schemaName()), s.workspaceID, normalizeProviderOrUnknown(provider), strings.TrimSpace(authID), strings.TrimSpace(selectionKey)).Scan(&credentialID)
	if err != nil {
		return "", err
	}
	return credentialID, nil
}

func (s *store) lookupProviderBySelectionKey(ctx context.Context, selectionKey string) (string, error) {
	selectionKey = strings.TrimSpace(selectionKey)
	if selectionKey == "" {
		return "", pgx.ErrNoRows
	}
	var provider string
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT provider
		FROM %s.credentials
		WHERE workspace_id = $1 AND selection_key = $2 AND deleted_at IS NULL
		LIMIT 1
	`, s.schemaName()), s.workspaceID, selectionKey).Scan(&provider)
	return provider, err
}

func (s *store) backfillUsageSnapshot(ctx context.Context, snapshot map[string]map[string][]UsageEvent) (int64, error) {
	var inserted int64
	for _, byModel := range snapshot {
		for _, events := range byModel {
			for _, event := range events {
				if err := s.ingestUsageEvent(ctx, event); err != nil {
					return inserted, err
				}
				inserted++
			}
		}
	}
	return inserted, nil
}

func (s *store) providerOverview(ctx context.Context, provider string) (ProviderOverview, error) {
	provider = normalizeProvider(provider)
	if provider == "" {
		return ProviderOverview{}, fmt.Errorf("platform: provider is required")
	}

	total, disabled, unavailable, loaded, failedQuota, err := s.loadCredentialCounts(ctx, provider)
	if err != nil {
		return ProviderOverview{}, err
	}
	active := total - disabled
	if active < 0 {
		active = 0
	}
	windows, activePoolPercent7d, err := s.loadWindowStats(ctx, provider, total)
	if err != nil {
		return ProviderOverview{}, err
	}
	datasets, err := s.loadHistogramDatasets(ctx, provider)
	if err != nil {
		return ProviderOverview{}, err
	}
	mode := "usage-only"
	var conservativeHealth *float64
	var averageHealth *float64
	var operationalHealth *float64
	var conservativeRisk *float64
	var averageRisk *float64
	var burn *float64
	if len(datasets) > 0 {
		mode = "quota"
		conservativeHealth, averageHealth, conservativeRisk, averageRisk, burn, err = s.loadQuotaSummary(ctx, provider)
		if err != nil {
			return ProviderOverview{}, err
		}
	} else {
		value := 100.0
		if len(windows) > 0 {
			value = 100.0 - windows[len(windows)-1].FailureRate
		}
		availabilityRatio := 100.0
		if active > 0 {
			availabilityRatio = float64(active-unavailable) / float64(active) * 100
		}
		operational := math.Max(0, math.Min(100, availabilityRatio*0.55+value*0.45))
		operationalHealth = &operational
	}

	note := "usage rollups active"
	if mode == "quota" {
		note = "quota snapshots available"
	}
	overview := ProviderOverview{
		Provider:                 provider,
		Mode:                     mode,
		TotalCredentials:         total,
		ActiveCredentials:        active,
		DisabledCredentials:      disabled,
		UnavailableCredentials:   unavailable,
		LoadedCredentials:        loaded,
		FailedQuotaCredentials:   failedQuota,
		HistogramLabels:          []string{"90-100%", "80-90%", "70-80%", "60-70%", "50-60%", "40-50%", "30-40%", "20-30%", "10-20%", "0-10%"},
		HistogramDatasets:        datasets,
		WindowStats:              windows,
		ConservativeHealth:       conservativeHealth,
		AverageHealth:            averageHealth,
		OperationalHealth:        operationalHealth,
		ConservativeRiskDays:     conservativeRisk,
		AverageRiskDays:          averageRisk,
		AvgDailyQuotaBurnPercent: burn,
		ActivePoolPercent7d:      activePoolPercent7d,
		Note:                     note,
		Warnings:                 nil,
		GeneratedAt:              time.Now().UTC(),
	}
	return overview, nil
}

func (s *store) listProviderCredentials(ctx context.Context, provider string, query CredentialListQuery) (CredentialListResult, error) {
	provider = normalizeProvider(provider)
	if provider == "" {
		return CredentialListResult{}, fmt.Errorf("platform: provider is required")
	}
	page := query.Page
	if page <= 0 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	search := "%" + strings.ToLower(strings.TrimSpace(query.Search)) + "%"

	var total int64
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %s.credentials
		WHERE workspace_id = $1 AND provider = $2 AND deleted_at IS NULL
		  AND ($3 = '%%' OR LOWER(credential_name) LIKE $3 OR LOWER(label) LIKE $3)
	`, s.schemaName()), s.workspaceID, provider, search).Scan(&total); err != nil {
		return CredentialListResult{}, err
	}

	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		WITH rollup AS (
			SELECT
				credential_key,
				SUM(CASE WHEN bucket_start >= NOW() - INTERVAL '24 hours' THEN request_count ELSE 0 END) AS requests_24h,
				SUM(CASE WHEN bucket_start >= NOW() - INTERVAL '7 days' THEN request_count ELSE 0 END) AS requests_7d,
				SUM(CASE WHEN bucket_start >= NOW() - INTERVAL '24 hours' THEN failure_count ELSE 0 END) AS failures_24h,
				SUM(CASE WHEN bucket_start >= NOW() - INTERVAL '24 hours' THEN token_count ELSE 0 END) AS tokens_24h,
				SUM(CASE WHEN bucket_start >= NOW() - INTERVAL '7 days' THEN token_count ELSE 0 END) AS tokens_7d
			FROM %s.usage_rollup_1h
			WHERE workspace_id = $1 AND provider = $2
			GROUP BY credential_key
		)
		SELECT
			c.id, c.runtime_id, c.credential_name, c.selection_key, c.provider, c.label,
			COALESCE(pa.account_key, ''), COALESCE(pa.account_email, ''), c.status, c.status_message,
			c.disabled, c.unavailable, c.runtime_only, c.last_refresh_at, activity.last_seen_at, c.updated_at,
			COALESCE(r.requests_24h, 0), COALESCE(r.requests_7d, 0),
			COALESCE(r.failures_24h, 0), COALESCE(r.tokens_24h, 0), COALESCE(r.tokens_7d, 0),
			qs.health_percent, qs.conservative_risk_days, qs.avg_daily_quota_burn_percent, qs.mode, qs.quota_exceeded
		FROM %s.credentials c
		LEFT JOIN %s.provider_accounts pa ON pa.id = c.provider_account_id
		LEFT JOIN rollup r ON r.credential_key = c.id::text
		LEFT JOIN %s.credential_quota_snapshots qs ON qs.credential_id = c.id
		LEFT JOIN %s.credential_activity_snapshots activity ON activity.credential_id = c.id
		WHERE c.workspace_id = $1 AND c.provider = $2 AND c.deleted_at IS NULL
		  AND ($3 = '%%' OR LOWER(c.credential_name) LIKE $3 OR LOWER(c.label) LIKE $3)
		ORDER BY c.updated_at DESC, c.credential_name ASC
		OFFSET $4 LIMIT $5
		`, s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName()),
		s.workspaceID, provider, search, int64((page-1)*pageSize), int64(pageSize))
	if err != nil {
		return CredentialListResult{}, err
	}
	defer rows.Close()

	items := make([]ProviderCredentialRow, 0, pageSize)
	for rows.Next() {
		var (
			row           ProviderCredentialRow
			lastRefreshAt *time.Time
			lastSeenAt    *time.Time
			health        *float64
			risk          *float64
			burnValue     *float64
			failures24h   int64
			mode          string
			quotaExceeded bool
		)
		if err = rows.Scan(
			&row.ID, &row.SourceAuthID, &row.FileName, &row.SelectionKey, &row.Provider, &row.Label,
			&row.AccountKey, &row.AccountEmail, &row.Status, &row.StatusMessage,
			&row.Disabled, &row.Unavailable, &row.RuntimeOnly, &lastRefreshAt, &lastSeenAt, &row.UpdatedAt,
			&row.Requests24h, &row.Requests7d, &failures24h, &row.TotalTokens24h, &row.TotalTokens7d,
			&health, &risk, &burnValue, &mode, &quotaExceeded,
		); err != nil {
			return CredentialListResult{}, err
		}
		row.LastRefreshAt = lastRefreshAt
		row.LastSeenAt = lastSeenAt
		row.Failures24h = failures24h
		row.HealthPercent = health
		row.ConservativeRiskDays = risk
		row.AvgDailyQuotaBurnPercent = burnValue
		row.SnapshotMode = mode
		row.QuotaExceeded = quotaExceeded
		if row.Requests24h > 0 {
			row.FailureRate24h = float64(failures24h) / float64(row.Requests24h) * 100
		}
		items = append(items, row)
	}
	if err = rows.Err(); err != nil {
		return CredentialListResult{}, err
	}
	return CredentialListResult{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *store) listCredentials(ctx context.Context, query CredentialListQuery) (CredentialListResult, error) {
	page := query.Page
	if page <= 0 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	search := "%" + strings.ToLower(strings.TrimSpace(query.Search)) + "%"
	provider := normalizeProvider(query.Provider)
	status := strings.TrimSpace(strings.ToLower(query.Status))
	sortBy := strings.TrimSpace(strings.ToLower(query.SortBy))
	activityRange := strings.TrimSpace(strings.ToLower(query.ActivityRange))

	baseWhere := `
		c.workspace_id = $1
		AND c.deleted_at IS NULL
		AND ($2 = '%%' OR LOWER(c.credential_name) LIKE $2 OR LOWER(c.label) LIKE $2 OR LOWER(c.selection_key) LIKE $2 OR LOWER(COALESCE(pa.account_key, '')) LIKE $2 OR LOWER(COALESCE(pa.account_email, '')) LIKE $2)
	`
	switch status {
	case "disabled":
		baseWhere += " AND c.disabled = TRUE"
	case "unavailable":
		baseWhere += " AND c.unavailable = TRUE"
	case "quota-limited":
		baseWhere += " AND COALESCE(qs.quota_exceeded, FALSE) = TRUE"
	case "warning":
		baseWhere += " AND c.status_message <> ''"
	case "healthy":
		baseWhere += " AND c.disabled = FALSE AND c.unavailable = FALSE AND COALESCE(qs.quota_exceeded, FALSE) = FALSE AND c.status_message = ''"
	}
	switch activityRange {
	case "24h":
		baseWhere += " AND COALESCE(activity.requests_24h, 0) > 0"
	case "7d":
		baseWhere += " AND COALESCE(activity.requests_7d, 0) > 0"
	}
	filteredWhere := baseWhere + "\n\t\tAND ($3 = '' OR c.provider = $3)"

	orderBy := "c.updated_at DESC, c.credential_name ASC"
	switch sortBy {
	case "name":
		orderBy = "c.credential_name ASC"
	case "active-desc":
		orderBy = "activity.last_seen_at DESC NULLS LAST, c.credential_name ASC"
	case "success-desc":
		orderBy = "(COALESCE(activity.requests_24h, 0) - COALESCE(activity.failures_24h, 0)) DESC, c.credential_name ASC"
	case "failure-desc":
		orderBy = "COALESCE(activity.failures_24h, 0) DESC, c.credential_name ASC"
	}

	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %s.credentials c
		LEFT JOIN %s.credential_quota_snapshots qs ON qs.credential_id = c.id
		LEFT JOIN %s.credential_activity_snapshots activity ON activity.credential_id = c.id
		LEFT JOIN %s.provider_accounts pa ON pa.id = c.provider_account_id
		WHERE %s
	`, s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName(), filteredWhere)
	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, s.workspaceID, search, nullableProviderFilter(provider)).Scan(&total); err != nil {
		return CredentialListResult{}, err
	}
	facets, err := s.loadCredentialProviderFacets(ctx, baseWhere, search)
	if err != nil {
		return CredentialListResult{}, err
	}

	rowsQuery := fmt.Sprintf(`
		SELECT
			c.id, c.runtime_id, c.credential_name, c.selection_key, c.provider, c.label,
			COALESCE(pa.account_key, ''), COALESCE(pa.account_email, ''), c.status, c.status_message,
			c.disabled, c.unavailable, c.runtime_only, c.last_refresh_at, activity.last_seen_at, c.updated_at,
			COALESCE(activity.requests_24h, 0), COALESCE(activity.requests_7d, 0), COALESCE(activity.failures_24h, 0),
			COALESCE(activity.total_tokens_24h, 0), COALESCE(activity.total_tokens_7d, 0),
			qs.health_percent, qs.conservative_risk_days, qs.avg_daily_quota_burn_percent, COALESCE(qs.mode, 'usage-only'), COALESCE(qs.quota_exceeded, FALSE)
		FROM %s.credentials c
		LEFT JOIN %s.provider_accounts pa ON pa.id = c.provider_account_id
		LEFT JOIN %s.credential_quota_snapshots qs ON qs.credential_id = c.id
		LEFT JOIN %s.credential_activity_snapshots activity ON activity.credential_id = c.id
		WHERE %s
		ORDER BY %s
		OFFSET $4 LIMIT $5
	`, s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName(), filteredWhere, orderBy)
	rows, err := s.pool.Query(ctx, rowsQuery, s.workspaceID, search, nullableProviderFilter(provider), int64((page-1)*pageSize), int64(pageSize))
	if err != nil {
		return CredentialListResult{}, err
	}
	defer rows.Close()

	items := make([]ProviderCredentialRow, 0, pageSize)
	for rows.Next() {
		row, errRow := scanProviderCredentialRow(rows)
		if errRow != nil {
			return CredentialListResult{}, errRow
		}
		items = append(items, row)
	}
	if err = rows.Err(); err != nil {
		return CredentialListResult{}, err
	}
	return CredentialListResult{
		Items:          items,
		Page:           page,
		PageSize:       pageSize,
		Total:          total,
		ProviderFacets: facets,
	}, nil
}

func (s *store) loadCredentialProviderFacets(ctx context.Context, where string, search string) ([]ProviderFacet, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT c.provider, COUNT(*)
		FROM %s.credentials c
		LEFT JOIN %s.provider_accounts pa ON pa.id = c.provider_account_id
		LEFT JOIN %s.credential_quota_snapshots qs ON qs.credential_id = c.id
		LEFT JOIN %s.credential_activity_snapshots activity ON activity.credential_id = c.id
		WHERE %s
		GROUP BY c.provider
		ORDER BY COUNT(*) DESC, c.provider ASC
	`, s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName(), where), s.workspaceID, search)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	facets := make([]ProviderFacet, 0, 8)
	for rows.Next() {
		var facet ProviderFacet
		if err = rows.Scan(&facet.Provider, &facet.Count); err != nil {
			return nil, err
		}
		facets = append(facets, facet)
	}
	return facets, rows.Err()
}

func scanProviderCredentialRow(rows pgx.Rows) (ProviderCredentialRow, error) {
	var (
		row           ProviderCredentialRow
		lastRefreshAt *time.Time
		lastSeenAt    *time.Time
		health        *float64
		risk          *float64
		burnValue     *float64
		mode          string
		quotaExceeded bool
	)
	if err := rows.Scan(
		&row.ID, &row.SourceAuthID, &row.FileName, &row.SelectionKey, &row.Provider, &row.Label,
		&row.AccountKey, &row.AccountEmail, &row.Status, &row.StatusMessage,
		&row.Disabled, &row.Unavailable, &row.RuntimeOnly, &lastRefreshAt, &lastSeenAt, &row.UpdatedAt,
		&row.Requests24h, &row.Requests7d, &row.Failures24h, &row.TotalTokens24h, &row.TotalTokens7d,
		&health, &risk, &burnValue, &mode, &quotaExceeded,
	); err != nil {
		return ProviderCredentialRow{}, err
	}
	row.LastRefreshAt = lastRefreshAt
	row.LastSeenAt = lastSeenAt
	row.HealthPercent = health
	row.ConservativeRiskDays = risk
	row.AvgDailyQuotaBurnPercent = burnValue
	row.SnapshotMode = mode
	row.QuotaExceeded = quotaExceeded
	if row.Requests24h > 0 {
		row.FailureRate24h = float64(row.Failures24h) / float64(row.Requests24h) * 100
	}
	return row, nil
}

func (s *store) loadCredentialCounts(ctx context.Context, provider string) (int64, int64, int64, int64, int64, error) {
	var total, disabled, unavailable, loaded, failedQuota int64
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE disabled) AS disabled,
			COUNT(*) FILTER (WHERE unavailable) AS unavailable,
			COUNT(qs.credential_id) AS loaded,
			COUNT(*) FILTER (WHERE qs.mode = 'quota' AND qs.health_percent IS NULL) AS failed_quota
		FROM %s.credentials c
		LEFT JOIN %s.credential_quota_snapshots qs ON qs.credential_id = c.id
		WHERE c.workspace_id = $1 AND c.provider = $2 AND c.deleted_at IS NULL
	`, s.schemaName(), s.schemaName()), s.workspaceID, provider).Scan(&total, &disabled, &unavailable, &loaded, &failedQuota)
	return total, disabled, unavailable, loaded, failedQuota, err
}

func (s *store) loadWindowStats(ctx context.Context, provider string, total int64) ([]WindowStat, float64, error) {
	type windowDef struct {
		id       string
		label    string
		duration time.Duration
		table    string
	}
	windows := []windowDef{
		{id: "5h", label: "5h", duration: 5 * time.Hour, table: "usage_rollup_5m"},
		{id: "24h", label: "24h", duration: 24 * time.Hour, table: "usage_rollup_1h"},
		{id: "3d", label: "3d", duration: 72 * time.Hour, table: "usage_rollup_1h"},
		{id: "7d", label: "7d", duration: 168 * time.Hour, table: "usage_rollup_1h"},
	}
	stats := make([]WindowStat, 0, len(windows))
	var activePoolPercent7d float64
	for _, window := range windows {
		var requestCount, tokenCount, failureCount, activeCredentialCount int64
		if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
			SELECT
				COALESCE(SUM(request_count), 0),
				COALESCE(SUM(token_count), 0),
				COALESCE(SUM(failure_count), 0),
				COUNT(DISTINCT NULLIF(credential_key, ''))
			FROM %s.%s
			WHERE workspace_id = $1 AND provider = $2 AND bucket_start >= $3
		`, s.schemaName(), quoteIdentifier(window.table)), s.workspaceID, provider, time.Now().UTC().Add(-window.duration)).Scan(&requestCount, &tokenCount, &failureCount, &activeCredentialCount); err != nil {
			return nil, 0, err
		}
		failureRate := 0.0
		if requestCount > 0 {
			failureRate = float64(failureCount) / float64(requestCount) * 100
		}
		days := window.duration.Hours() / 24
		if days <= 0 {
			days = 1
		}
		activePoolPercent := 0.0
		if total > 0 {
			activePoolPercent = float64(activeCredentialCount) / float64(total) * 100
		}
		stat := WindowStat{
			ID:                    window.id,
			Label:                 window.label,
			RequestCount:          requestCount,
			TokenCount:            tokenCount,
			FailureCount:          failureCount,
			FailureRate:           failureRate,
			ActiveCredentialCount: activeCredentialCount,
			ActivePoolPercent:     activePoolPercent,
			AvgDailyRequests:      float64(requestCount) / days,
			AvgDailyTokens:        float64(tokenCount) / days,
		}
		if window.id == "7d" {
			activePoolPercent7d = activePoolPercent
		}
		stats = append(stats, stat)
	}
	return stats, activePoolPercent7d, nil
}

func (s *store) loadHistogramDatasets(ctx context.Context, provider string) ([]HistogramDataset, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		WITH base AS (
			SELECT
				q.dimension_id,
				q.dimension_label,
				GREATEST(0, LEAST(9, 9 - FLOOR(COALESCE(q.remaining_ratio, 0) * 10)::int)) AS bucket_index,
				COALESCE(q.remaining_ratio, 0) * 100 AS remaining_percent
			FROM %s.quota_dimensions_latest q
			JOIN %s.credentials c ON c.id = q.credential_id
			WHERE c.workspace_id = $1 AND c.provider = $2 AND c.deleted_at IS NULL
		),
		dataset_avg AS (
			SELECT
				dimension_id,
				AVG(remaining_percent) AS dataset_avg
			FROM base
			GROUP BY dimension_id
		)
		SELECT
			b.dimension_id,
			b.dimension_label,
			b.bucket_index,
			COUNT(*) AS count,
			da.dataset_avg
		FROM base b
		JOIN dataset_avg da ON da.dimension_id = b.dimension_id
		GROUP BY b.dimension_id, b.dimension_label, b.bucket_index, da.dataset_avg
		ORDER BY b.dimension_id, b.bucket_index
	`, s.schemaName(), s.schemaName()), s.workspaceID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexByDataset := make(map[string]int)
	datasets := make([]HistogramDataset, 0)
	for rows.Next() {
		var (
			id          string
			label       string
			bucketIndex int
			count       int64
			datasetAvg  *float64
		)
		if err = rows.Scan(&id, &label, &bucketIndex, &count, &datasetAvg); err != nil {
			return nil, err
		}
		pos, ok := indexByDataset[id]
		if !ok {
			pos = len(datasets)
			indexByDataset[id] = pos
			datasets = append(datasets, HistogramDataset{
				ID:     id,
				Label:  label,
				Color:  overviewColors[pos%len(overviewColors)],
				Counts: make([]int64, 10),
			})
		}
		datasets[pos].Counts[bucketIndex] = count
		if datasetAvg != nil {
			avg := *datasetAvg
			datasets[pos].AverageRemaining = &avg
		}
	}
	return datasets, rows.Err()
}

func (s *store) listHistogramBucketItems(ctx context.Context, provider string, datasetID string, bucketIndex int, page int, pageSize int) (HistogramBucketItemsResponse, error) {
	provider = normalizeProvider(provider)
	if provider == "" {
		return HistogramBucketItemsResponse{}, fmt.Errorf("platform: provider is required")
	}
	datasetID = strings.TrimSpace(datasetID)
	if datasetID == "" {
		return HistogramBucketItemsResponse{}, fmt.Errorf("platform: dataset_id is required")
	}
	if bucketIndex < 0 || bucketIndex > 9 {
		return HistogramBucketItemsResponse{}, fmt.Errorf("platform: bucket_index out of range")
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 200
	}

	var total int64
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %s.quota_dimensions_latest q
		JOIN %s.credentials c ON c.id = q.credential_id
		WHERE c.workspace_id = $1 AND c.provider = $2 AND c.deleted_at IS NULL
		  AND q.dimension_id = $3
		  AND GREATEST(0, LEAST(9, 9 - FLOOR(COALESCE(q.remaining_ratio, 0) * 10)::int)) = $4
	`, s.schemaName(), s.schemaName()), s.workspaceID, provider, datasetID, bucketIndex).Scan(&total); err != nil {
		return HistogramBucketItemsResponse{}, err
	}

	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			c.id,
			c.credential_name,
			COALESCE(q.remaining_ratio, 0) * 100 AS remaining_percent,
			q.reset_at,
			c.disabled,
			c.unavailable,
			COALESCE(qs.quota_exceeded, FALSE) AS quota_exceeded
		FROM %s.quota_dimensions_latest q
		JOIN %s.credentials c ON c.id = q.credential_id
		LEFT JOIN %s.credential_quota_snapshots qs ON qs.credential_id = c.id
		WHERE c.workspace_id = $1 AND c.provider = $2 AND c.deleted_at IS NULL
		  AND q.dimension_id = $3
		  AND GREATEST(0, LEAST(9, 9 - FLOOR(COALESCE(q.remaining_ratio, 0) * 10)::int)) = $4
		ORDER BY COALESCE(q.remaining_ratio, 0) DESC, c.credential_name ASC
		OFFSET $5 LIMIT $6
	`, s.schemaName(), s.schemaName(), s.schemaName()), s.workspaceID, provider, datasetID, bucketIndex, int64((page-1)*pageSize), int64(pageSize))
	if err != nil {
		return HistogramBucketItemsResponse{}, err
	}
	defer rows.Close()

	items := make([]HistogramBucketItemRow, 0)
	for rows.Next() {
		var (
			credentialID   string
			credentialName string
			remaining      float64
			resetAt        *time.Time
			disabled       bool
			unavailable    bool
			quotaExceeded  bool
		)
		if err := rows.Scan(&credentialID, &credentialName, &remaining, &resetAt, &disabled, &unavailable, &quotaExceeded); err != nil {
			return HistogramBucketItemsResponse{}, err
		}
		items = append(items, HistogramBucketItemRow{
			CredentialID:     credentialID,
			CredentialName:   credentialName,
			RemainingPercent: remaining,
			ResetAt:          resetAt,
			Disabled:         disabled,
			Unavailable:      unavailable,
			QuotaExceeded:    quotaExceeded,
		})
	}
	if err := rows.Err(); err != nil {
		return HistogramBucketItemsResponse{}, err
	}
	return HistogramBucketItemsResponse{
		Provider:    provider,
		DatasetID:   datasetID,
		BucketIndex: bucketIndex,
		Total:       total,
		Page:        page,
		PageSize:    pageSize,
		Items:       items,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func (s *store) getCredentialDetail(ctx context.Context, credentialID string) (CredentialDetail, error) {
	credentialID = strings.TrimSpace(credentialID)
	var (
		row           ProviderCredentialRow
		secretVersion int
		metadataRaw   []byte
		lastRefreshAt *time.Time
		lastSeenAt    *time.Time
		health        *float64
		risk          *float64
		burnValue     *float64
		mode          string
		quotaExceeded bool
	)
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT
			c.id, c.runtime_id, c.credential_name, c.selection_key, c.provider, c.label,
			COALESCE(pa.account_key, ''), COALESCE(pa.account_email, ''), c.status, c.status_message,
			c.disabled, c.unavailable, c.runtime_only, c.last_refresh_at, activity.last_seen_at, c.updated_at,
			COALESCE(activity.requests_24h, 0), COALESCE(activity.requests_7d, 0), COALESCE(activity.failures_24h, 0),
			COALESCE(activity.total_tokens_24h, 0), COALESCE(activity.total_tokens_7d, 0),
			qs.health_percent, qs.conservative_risk_days, qs.avg_daily_quota_burn_percent, COALESCE(qs.mode, 'usage-only'), COALESCE(qs.quota_exceeded, FALSE),
			COALESCE((SELECT MAX(version) FROM %s.credential_secret_versions sv WHERE sv.credential_id = c.id), 0),
			c.metadata
		FROM %s.credentials c
		LEFT JOIN %s.provider_accounts pa ON pa.id = c.provider_account_id
		LEFT JOIN %s.credential_quota_snapshots qs ON qs.credential_id = c.id
		LEFT JOIN %s.credential_activity_snapshots activity ON activity.credential_id = c.id
		WHERE c.workspace_id = $1 AND c.id = $2 AND c.deleted_at IS NULL
	`, s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName(), s.schemaName()), s.workspaceID, credentialID).Scan(
		&row.ID, &row.SourceAuthID, &row.FileName, &row.SelectionKey, &row.Provider, &row.Label,
		&row.AccountKey, &row.AccountEmail, &row.Status, &row.StatusMessage,
		&row.Disabled, &row.Unavailable, &row.RuntimeOnly, &lastRefreshAt, &lastSeenAt, &row.UpdatedAt,
		&row.Requests24h, &row.Requests7d, &row.Failures24h, &row.TotalTokens24h, &row.TotalTokens7d,
		&health, &risk, &burnValue, &mode, &quotaExceeded, &secretVersion, &metadataRaw,
	)
	if err != nil {
		return CredentialDetail{}, err
	}
	row.LastRefreshAt = lastRefreshAt
	row.LastSeenAt = lastSeenAt
	row.HealthPercent = health
	row.ConservativeRiskDays = risk
	row.AvgDailyQuotaBurnPercent = burnValue
	row.SnapshotMode = mode
	row.QuotaExceeded = quotaExceeded
	if row.Requests24h > 0 {
		row.FailureRate24h = float64(row.Failures24h) / float64(row.Requests24h) * 100
	}
	metadata := map[string]any{}
	if len(metadataRaw) > 0 {
		_ = json.Unmarshal(metadataRaw, &metadata)
	}
	dimensions, err := s.listQuotaDimensions(ctx, credentialID)
	if err != nil {
		return CredentialDetail{}, err
	}
	return CredentialDetail{
		ProviderCredentialRow: row,
		SecretVersion:         secretVersion,
		Metadata:              metadata,
		Dimensions:            dimensions,
	}, nil
}

func (s *store) listQuotaDimensions(ctx context.Context, credentialID string) ([]QuotaDimension, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT dimension_id, dimension_label, remaining_ratio, reset_at, window_seconds, state, observed_at
		FROM %s.quota_dimensions_latest
		WHERE credential_id = $1
		ORDER BY dimension_id ASC
	`, s.schemaName()), credentialID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]QuotaDimension, 0, 8)
	for rows.Next() {
		var (
			item          QuotaDimension
			windowSeconds *int
		)
		if err = rows.Scan(&item.ID, &item.Label, &item.RemainingRatio, &item.ResetAt, &windowSeconds, &item.State, &item.ObservedAt); err != nil {
			return nil, err
		}
		item.WindowSeconds = windowSeconds
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *store) downloadCredentialContent(ctx context.Context, credentialID string) (string, string, []byte, error) {
	var (
		fileName     string
		sourceAuthID string
		secret       SealedSecret
	)
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT
			c.credential_name,
			c.runtime_id,
			sv.content_sha256,
			sv.ciphertext,
			sv.cipher_nonce,
			sv.encrypted_dek,
			sv.dek_nonce,
			sv.algorithm,
			sv.key_version
		FROM %s.credentials c
		JOIN %s.credential_secret_versions sv ON sv.id = c.active_secret_version_id
		WHERE c.workspace_id = $1 AND c.id = $2 AND c.deleted_at IS NULL
	`, s.schemaName(), s.schemaName()), s.workspaceID, credentialID).Scan(
		&fileName,
		&sourceAuthID,
		&secret.ContentSHA256,
		&secret.Ciphertext,
		&secret.CipherNonce,
		&secret.EncryptedDEK,
		&secret.DEKNonce,
		&secret.Algorithm,
		&secret.KeyVersion,
	)
	if err != nil {
		return "", "", nil, err
	}
	payload, err := OpenSecret(s.masterKey, secret)
	if err != nil {
		return "", "", nil, err
	}
	return fileName, sourceAuthID, payload, nil
}

func (s *store) getQuotaRefreshPolicies(ctx context.Context) (QuotaRefreshPoliciesResponse, error) {
	const settingName = "quota_refresh_policies"
	var (
		raw       []byte
		updatedAt time.Time
	)
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT value, updated_at
		FROM %s.settings
		WHERE workspace_id = $1 AND name = $2
	`, s.schemaName()), s.workspaceID, settingName).Scan(&raw, &updatedAt)
	if err != nil && err != pgx.ErrNoRows {
		return QuotaRefreshPoliciesResponse{}, err
	}
	if err == nil {
		policies := defaultQuotaRefreshPolicies()
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &policies)
		}
		return QuotaRefreshPoliciesResponse{
			Policies:  normalizeQuotaRefreshPolicies(policies),
			UpdatedAt: updatedAt.UTC(),
			Source:    "database",
		}, nil
	}
	return QuotaRefreshPoliciesResponse{
		Policies:  defaultQuotaRefreshPolicies(),
		UpdatedAt: time.Now().UTC(),
		Source:    "default",
	}, nil
}

func (s *store) setQuotaRefreshPolicies(ctx context.Context, policies []QuotaRefreshPolicy) error {
	policies = normalizeQuotaRefreshPolicies(policies)
	payload, err := json.Marshal(policies)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.settings (id, tenant_id, workspace_id, name, value)
		VALUES ($1, $2, $3, 'quota_refresh_policies', $4::jsonb)
		ON CONFLICT (workspace_id, name)
		DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, s.schemaName()), uuid.NewString(), s.tenantID, s.workspaceID, string(payload))
	return err
}

func defaultQuotaRefreshPolicies() []QuotaRefreshPolicy {
	return []QuotaRefreshPolicy{
		{Provider: "codex", Enabled: true, IntervalSeconds: 900, TimeoutSeconds: 20, MaxParallelism: 4, StaleAfterSeconds: 1800, FailureBackoffSeconds: 300},
		{Provider: "claude", Enabled: true, IntervalSeconds: 900, TimeoutSeconds: 20, MaxParallelism: 4, StaleAfterSeconds: 1800, FailureBackoffSeconds: 300},
		{Provider: "gemini-cli", Enabled: true, IntervalSeconds: 1800, TimeoutSeconds: 25, MaxParallelism: 3, StaleAfterSeconds: 3600, FailureBackoffSeconds: 600},
		{Provider: "antigravity", Enabled: true, IntervalSeconds: 1800, TimeoutSeconds: 25, MaxParallelism: 3, StaleAfterSeconds: 3600, FailureBackoffSeconds: 600},
		{Provider: "kimi", Enabled: true, IntervalSeconds: 1800, TimeoutSeconds: 25, MaxParallelism: 3, StaleAfterSeconds: 3600, FailureBackoffSeconds: 600},
	}
}

func normalizeQuotaRefreshPolicies(input []QuotaRefreshPolicy) []QuotaRefreshPolicy {
	if len(input) == 0 {
		return defaultQuotaRefreshPolicies()
	}
	seen := map[string]struct{}{}
	result := make([]QuotaRefreshPolicy, 0, len(input))
	for _, item := range input {
		item.Provider = normalizeProvider(item.Provider)
		if item.Provider == "" {
			continue
		}
		if _, ok := seen[item.Provider]; ok {
			continue
		}
		seen[item.Provider] = struct{}{}
		if item.IntervalSeconds <= 0 {
			item.IntervalSeconds = 900
		}
		if item.TimeoutSeconds <= 0 {
			item.TimeoutSeconds = 20
		}
		if item.MaxParallelism <= 0 {
			item.MaxParallelism = 1
		}
		if item.StaleAfterSeconds <= 0 {
			item.StaleAfterSeconds = item.IntervalSeconds * 2
		}
		if item.FailureBackoffSeconds < 0 {
			item.FailureBackoffSeconds = 0
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return defaultQuotaRefreshPolicies()
	}
	return result
}

func nullableProviderFilter(provider string) string {
	return strings.TrimSpace(normalizeProvider(provider))
}

func (s *store) loadQuotaSummary(ctx context.Context, provider string) (*float64, *float64, *float64, *float64, *float64, error) {
	var conservativeHealth, averageHealth, conservativeRisk, averageRisk, burn *float64
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT
			MIN(qs.health_percent),
			AVG(qs.health_percent),
			MIN(qs.conservative_risk_days),
			AVG(qs.conservative_risk_days),
			AVG(qs.avg_daily_quota_burn_percent)
		FROM %s.credential_quota_snapshots qs
		JOIN %s.credentials c ON c.id = qs.credential_id
		WHERE c.workspace_id = $1 AND c.provider = $2 AND c.deleted_at IS NULL
		  AND qs.mode = 'quota'
	`, s.schemaName(), s.schemaName()), s.workspaceID, provider).Scan(&conservativeHealth, &averageHealth, &conservativeRisk, &averageRisk, &burn)
	return conservativeHealth, averageHealth, conservativeRisk, averageRisk, burn, err
}

func authAccountInfo(auth *coreauth.Auth) (string, string) {
	if auth == nil {
		return "unknown", ""
	}
	if accountType, account := auth.AccountInfo(); strings.TrimSpace(account) != "" {
		return normalizeAccountKey(accountType, account), strings.TrimSpace(account)
	}
	email := strings.TrimSpace(authEmail(auth))
	if email != "" {
		return normalizeAccountKey("email", email), email
	}
	if auth.ID != "" {
		return normalizeAccountKey("auth", auth.ID), ""
	}
	return "unknown", ""
}

func normalizeAccountKey(prefix, value string) string {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	value = strings.TrimSpace(strings.ToLower(value))
	if prefix == "" {
		prefix = "default"
	}
	if value == "" {
		value = "unknown"
	}
	return prefix + ":" + value
}

func normalizeProvider(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}

func normalizeProviderOrUnknown(provider string) string {
	trimmed := normalizeProvider(provider)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func normalizeRollupCredentialKey(credentialID string) string {
	return strings.TrimSpace(credentialID)
}

func authFileName(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if strings.TrimSpace(auth.FileName) != "" {
		return strings.TrimSpace(auth.FileName)
	}
	return strings.TrimSpace(auth.ID)
}

func isRuntimeOnlyAuth(auth *coreauth.Auth) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(auth.Attributes["runtime_only"]), "true")
}

func authEmail(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Metadata != nil {
		if value, ok := auth.Metadata["email"].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	if auth.Attributes != nil {
		if value := strings.TrimSpace(auth.Attributes["email"]); value != "" {
			return value
		}
		if value := strings.TrimSpace(auth.Attributes["account_email"]); value != "" {
			return value
		}
	}
	return ""
}

func loadAuthRawContent(auth *coreauth.Auth) ([]byte, error) {
	if auth == nil {
		return nil, fmt.Errorf("platform: auth is nil")
	}
	if auth.Attributes != nil {
		if rawPath := strings.TrimSpace(auth.Attributes["path"]); rawPath != "" {
			if data, err := os.ReadFile(rawPath); err == nil && len(data) > 0 {
				return data, nil
			}
		}
	}
	payload := make(map[string]any, len(auth.Metadata)+6)
	for key, value := range auth.Metadata {
		payload[key] = value
	}
	if strings.TrimSpace(auth.Provider) != "" {
		payload["type"] = auth.Provider
	}
	if strings.TrimSpace(auth.Label) != "" {
		payload["label"] = auth.Label
	}
	if strings.TrimSpace(auth.Prefix) != "" {
		payload["prefix"] = auth.Prefix
	}
	if strings.TrimSpace(auth.ProxyURL) != "" {
		payload["proxy_url"] = auth.ProxyURL
	}
	if auth.Disabled {
		payload["disabled"] = true
	} else if _, ok := payload["disabled"]; !ok {
		payload["disabled"] = false
	}
	return json.Marshal(payload)
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func bucketTime(ts time.Time, step time.Duration) time.Time {
	ts = ts.UTC()
	if step <= 0 {
		return ts
	}
	return ts.Truncate(step)
}

func buildSnapshotFromUsageDetails(snapshot map[string]map[string][]UsageEvent, event UsageEvent) map[string]map[string][]UsageEvent {
	if snapshot == nil {
		snapshot = make(map[string]map[string][]UsageEvent)
	}
	byModel := snapshot[event.Provider]
	if byModel == nil {
		byModel = make(map[string][]UsageEvent)
		snapshot[event.Provider] = byModel
	}
	byModel[event.Model] = append(byModel[event.Model], event)
	return snapshot
}
