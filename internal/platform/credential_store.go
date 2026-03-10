package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

var _ coreauth.Store = (*Runtime)(nil)

type authMetadataEnvelope struct {
	Attributes map[string]string `json:"attributes"`
	Metadata   map[string]any    `json:"metadata"`
	ProxyURL   string            `json:"proxy_url"`
	Prefix     string            `json:"prefix"`
}

func (r *Runtime) AuthCount(ctx context.Context) (int, error) {
	if r == nil || r.store == nil {
		return 0, nil
	}
	return r.store.authCount(ctx)
}

func (r *Runtime) List(ctx context.Context) ([]*coreauth.Auth, error) {
	if r == nil || r.store == nil {
		return nil, nil
	}
	return r.store.listAuths(ctx)
}

func (r *Runtime) Save(ctx context.Context, auth *coreauth.Auth) (string, error) {
	if r == nil || r.store == nil || auth == nil {
		return "", nil
	}
	credentialID, err := r.store.upsertAuthWithID(ctx, auth)
	if err != nil {
		return "", err
	}
	r.invalidateProviderCache(ctx, auth.Provider)
	if err := r.publishCredentialChanged(ctx, auth.ID, normalizeProviderOrUnknown(auth.Provider)); err != nil {
		return "", err
	}
	if strings.TrimSpace(credentialID) != "" {
		return strings.TrimSpace(credentialID), nil
	}
	return strings.TrimSpace(auth.ID), nil
}

func (r *Runtime) Delete(ctx context.Context, id string) error {
	if r == nil || r.store == nil {
		return nil
	}
	if err := r.store.markAuthDeleted(ctx, id); err != nil {
		return err
	}
	r.invalidateAllOverviewCaches(ctx)
	return nil
}

func (s *store) authCount(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %s.credentials
		WHERE workspace_id = $1 AND deleted_at IS NULL
	`, s.schemaName()), s.workspaceID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *Runtime) LookupCredentialID(ctx context.Context, sourceAuthID string) (string, error) {
	if r == nil || r.store == nil {
		return "", fmt.Errorf("platform runtime unavailable")
	}
	return r.store.lookupCredentialIDBySourceAuthID(ctx, sourceAuthID)
}

func (s *store) lookupCredentialIDBySourceAuthID(ctx context.Context, sourceAuthID string) (string, error) {
	sourceAuthID = strings.TrimSpace(sourceAuthID)
	if sourceAuthID == "" {
		return "", fmt.Errorf("source auth id is required")
	}
	var credentialID string
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT id
		FROM %s.credentials
		WHERE workspace_id = $1 AND runtime_id = $2 AND deleted_at IS NULL
	`, s.schemaName()), s.workspaceID, sourceAuthID).Scan(&credentialID); err != nil {
		return "", err
	}
	return strings.TrimSpace(credentialID), nil
}

func (s *store) hasRuntimeID(ctx context.Context, runtimeID string) (bool, error) {
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" {
		return false, fmt.Errorf("runtime id is required")
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1
			FROM %s.credentials
			WHERE workspace_id = $1 AND runtime_id = $2 AND deleted_at IS NULL
		)
	`, s.schemaName()), s.workspaceID, runtimeID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *store) listAuths(ctx context.Context) ([]*coreauth.Auth, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT
			c.runtime_id,
			c.credential_name,
			c.selection_key,
			c.provider,
			c.label,
			c.status,
			c.status_message,
			c.disabled,
			c.unavailable,
			c.runtime_only,
			c.last_refresh_at,
			c.metadata,
			c.created_at,
			c.updated_at,
			COALESCE(qs.quota_exceeded, FALSE),
			cooldown.reset_at
		FROM %s.credentials c
		LEFT JOIN %s.credential_quota_snapshots qs ON qs.credential_id = c.id
		LEFT JOIN %s.quota_dimensions_latest cooldown
			ON cooldown.credential_id = c.id AND cooldown.dimension_id = 'cooldown'
		WHERE c.workspace_id = $1 AND c.deleted_at IS NULL
		ORDER BY c.credential_name, c.runtime_id
	`, s.schemaName(), s.schemaName(), s.schemaName()), s.workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	credentials := make([]*coreauth.Auth, 0, 32)
	for rows.Next() {
		var (
			sourceAuthID  string
			fileName      string
			authIndex     string
			provider      string
			label         string
			status        string
			statusMessage string
			disabled      bool
			unavailable   bool
			runtimeOnly   bool
			lastRefreshAt *time.Time
			metadataJSON  []byte
			createdAt     time.Time
			updatedAt     time.Time
			quotaExceeded bool
			nextRecoverAt *time.Time
		)
		if err = rows.Scan(
			&sourceAuthID,
			&fileName,
			&authIndex,
			&provider,
			&label,
			&status,
			&statusMessage,
			&disabled,
			&unavailable,
			&runtimeOnly,
			&lastRefreshAt,
			&metadataJSON,
			&createdAt,
			&updatedAt,
			&quotaExceeded,
			&nextRecoverAt,
		); err != nil {
			return nil, err
		}

		envelope := authMetadataEnvelope{}
		if len(metadataJSON) > 0 {
			if err = json.Unmarshal(metadataJSON, &envelope); err != nil {
				return nil, err
			}
		}
		if envelope.Attributes == nil {
			envelope.Attributes = make(map[string]string)
		}
		if runtimeOnly {
			envelope.Attributes["runtime_only"] = "true"
		}

		resolvedFileName := strings.TrimSpace(fileName)
		if runtimeOnly {
			resolvedFileName = strings.TrimSpace(sourceAuthID)
		}
		auth := &coreauth.Auth{
			ID:            strings.TrimSpace(sourceAuthID),
			FileName:      resolvedFileName,
			Provider:      strings.TrimSpace(provider),
			Label:         strings.TrimSpace(label),
			Prefix:        strings.TrimSpace(envelope.Prefix),
			Status:        coreauth.Status(strings.TrimSpace(status)),
			StatusMessage: strings.TrimSpace(statusMessage),
			Disabled:      disabled,
			Unavailable:   unavailable,
			ProxyURL:      strings.TrimSpace(envelope.ProxyURL),
			Attributes:    envelope.Attributes,
			Metadata:      envelope.Metadata,
			CreatedAt:     createdAt.UTC(),
			UpdatedAt:     updatedAt.UTC(),
		}
		if auth.Metadata == nil {
			auth.Metadata = make(map[string]any)
		}
		if lastRefreshAt != nil {
			auth.LastRefreshedAt = lastRefreshAt.UTC()
		}
		auth.Quota.Exceeded = quotaExceeded
		if nextRecoverAt != nil {
			auth.Quota.NextRecoverAt = nextRecoverAt.UTC()
		}
		if auth.EnsureSelectionKey() == "" && strings.TrimSpace(authIndex) != "" {
			auth.FileName = strings.TrimSpace(authIndex)
			auth.EnsureSelectionKey()
		}
		credentials = append(credentials, auth)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return credentials, nil
}
