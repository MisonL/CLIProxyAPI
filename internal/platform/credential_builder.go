package platform

import (
	"fmt"
	"path"
	"strings"
	"time"

	appconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func BuildCredentialRecord(cfg *appconfig.Config, credentialName, runtimeID string, payload []byte, existing *coreauth.Auth) (*coreauth.Auth, error) {
	credentialName, err := normalizeCredentialReference(credentialName)
	if err != nil {
		return nil, fmt.Errorf("invalid credential_name: %w", err)
	}
	runtimeID, err = normalizeCredentialReference(runtimeID)
	if err != nil {
		return nil, fmt.Errorf("invalid runtime_id: %w", err)
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty credential payload")
	}

	credentials := synthesizer.SynthesizeCredentialFile(&synthesizer.SynthesisContext{
		Config:         cfg,
		CredentialsDir: "",
		Now:            time.Now().UTC(),
	}, credentialName, payload)
	if len(credentials) == 0 {
		return nil, fmt.Errorf("invalid credential file")
	}

	record := credentials[0]
	for _, auth := range credentials {
		if auth == nil {
			continue
		}
		if auth.Attributes == nil || !strings.EqualFold(strings.TrimSpace(auth.Attributes["runtime_only"]), "true") {
			record = auth
			break
		}
	}
	if record == nil || record.Metadata == nil {
		return nil, fmt.Errorf("invalid credential file")
	}

	record = record.Clone()
	record.ID = runtimeID
	record.FileName = credentialName
	record.UpdatedAt = time.Now().UTC()
	if record.Attributes == nil {
		record.Attributes = make(map[string]string)
	}
	delete(record.Attributes, "path")
	delete(record.Attributes, "source")
	if existing != nil {
		record.CreatedAt = existing.CreatedAt
		if record.LastRefreshedAt.IsZero() {
			record.LastRefreshedAt = existing.LastRefreshedAt
		}
		record.NextRefreshAfter = existing.NextRefreshAfter
		record.NextRetryAfter = existing.NextRetryAfter
		record.Runtime = existing.Runtime
		record.Quota = existing.Quota
		record.LastError = existing.LastError
		if len(record.ModelStates) == 0 {
			record.ModelStates = existing.ModelStates
		}
	} else if record.CreatedAt.IsZero() {
		record.CreatedAt = record.UpdatedAt
	}
	return record, nil
}

func normalizeCredentialReference(value string) (string, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, `\\`, "/"))
	trimmed = strings.TrimLeft(trimmed, "/")
	if trimmed == "" {
		return "", fmt.Errorf("value is empty")
	}
	clean := path.Clean(trimmed)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("value is empty")
	}
	if strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("path escapes base directory")
	}
	return clean, nil
}
