package platform

import (
	"context"
	"fmt"
	appconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type CredentialImportOptions struct {
	Directory string
	DryRun    bool
	Config    *appconfig.Config
}

type CredentialImportFailure struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

type CredentialImportResult struct {
	Directory string                    `json:"directory"`
	DryRun    bool                      `json:"dry_run"`
	Scanned   int                       `json:"scanned"`
	Imported  int                       `json:"imported"`
	Updated   int                       `json:"updated"`
	Skipped   int                       `json:"skipped"`
	Failed    int                       `json:"failed"`
	Failures  []CredentialImportFailure `json:"failures,omitempty"`
}

func ImportCredentialsDir(ctx context.Context, cfg Config, options CredentialImportOptions) (CredentialImportResult, error) {
	result := CredentialImportResult{DryRun: options.DryRun}
	if err := validateImportConfig(cfg); err != nil {
		return result, err
	}
	root, err := filepath.Abs(strings.TrimSpace(options.Directory))
	if err != nil {
		return result, fmt.Errorf("platform: resolve import directory: %w", err)
	}
	result.Directory = root
	files, err := collectCredentialJSONFiles(root)
	if err != nil {
		return result, err
	}
	store, err := newStore(ctx, cfg)
	if err != nil {
		return result, err
	}
	defer store.close()

	existingAuths, err := store.listAuths(ctx)
	if err != nil {
		return result, err
	}
	existingByRuntimeID := make(map[string]*coreauth.Auth, len(existingAuths))
	for _, auth := range existingAuths {
		if auth == nil || strings.TrimSpace(auth.ID) == "" {
			continue
		}
		existingByRuntimeID[strings.TrimSpace(auth.ID)] = auth
	}

	for _, filePath := range files {
		result.Scanned++
		relPath, err := filepath.Rel(root, filePath)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, CredentialImportFailure{Path: filePath, Error: err.Error()})
			continue
		}
		credentialName, err := normalizeCredentialReference(relPath)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, CredentialImportFailure{Path: filePath, Error: err.Error()})
			continue
		}
		payload, err := os.ReadFile(filePath)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, CredentialImportFailure{Path: filePath, Error: err.Error()})
			continue
		}
		existing := existingByRuntimeID[credentialName]
		record, err := BuildCredentialRecord(options.Config, credentialName, credentialName, payload, existing)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, CredentialImportFailure{Path: credentialName, Error: err.Error()})
			continue
		}
		if options.DryRun {
			if existing != nil {
				result.Updated++
			} else {
				result.Imported++
			}
			continue
		}
		if _, err := store.upsertAuthWithID(ctx, record); err != nil {
			result.Failed++
			result.Failures = append(result.Failures, CredentialImportFailure{Path: credentialName, Error: err.Error()})
			continue
		}
		if existing != nil {
			result.Updated++
		} else {
			result.Imported++
		}
		existingByRuntimeID[credentialName] = record.Clone()
	}
	return result, nil
}

func validateImportConfig(cfg Config) error {
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return fmt.Errorf("platform: database URL is required")
	}
	if strings.TrimSpace(cfg.MasterKey) == "" {
		return fmt.Errorf("platform: master key is required")
	}
	if strings.TrimSpace(cfg.DatabaseSchema) == "" {
		return fmt.Errorf("platform: database schema is required")
	}
	if strings.TrimSpace(cfg.TenantSlug) == "" || strings.TrimSpace(cfg.WorkspaceSlug) == "" {
		return fmt.Errorf("platform: tenant/workspace slug is required")
	}
	return nil
}

func collectCredentialJSONFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("platform: stat import directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("platform: import path is not a directory")
	}
	files := make([]string, 0, 32)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("platform: walk import directory: %w", err)
	}
	sort.Strings(files)
	return files, nil
}
