package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	appconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/platform"
)

type importSummary = platform.CredentialImportResult

var importCredentialsDir = platform.ImportCredentialsDir

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError(stderr)
	}
	switch strings.TrimSpace(args[0]) {
	case "credentials":
		return runCredentialsImportCommand(args[1:], stdout, stderr)
	default:
		return usageError(stderr)
	}
}

func usageError(w io.Writer) error {
	fmt.Fprintln(w, "usage: CLIProxyAPI-migrate credentials --dir <path> --database-url <dsn> --database-schema <schema> --master-key <key> --tenant-slug <slug> --tenant-name <name> --workspace-slug <slug> --workspace-name <name> [--dry-run]")
	return errors.New("invalid migrate command")
}

func runCredentialsImportCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("credentials", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		dir            string
		databaseURL    string
		databaseSchema string
		masterKey      string
		tenantSlug     string
		tenantName     string
		workspaceSlug  string
		workspaceName  string
		dryRun         bool
	)
	fs.StringVar(&dir, "dir", "", "directory containing credential JSON files")
	fs.StringVar(&databaseURL, "database-url", "", "PostgreSQL DSN")
	fs.StringVar(&databaseSchema, "database-schema", "controlplane", "database schema")
	fs.StringVar(&masterKey, "master-key", "", "platform master key")
	fs.StringVar(&tenantSlug, "tenant-slug", "default", "tenant slug")
	fs.StringVar(&tenantName, "tenant-name", "", "tenant name")
	fs.StringVar(&workspaceSlug, "workspace-slug", "default", "workspace slug")
	fs.StringVar(&workspaceName, "workspace-name", "", "workspace name")
	fs.BoolVar(&dryRun, "dry-run", false, "validate without writing to database")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(databaseURL) == "" || strings.TrimSpace(masterKey) == "" {
		return usageError(stderr)
	}
	cfg := platform.Config{
		Enabled:        true,
		DatabaseURL:    strings.TrimSpace(databaseURL),
		DatabaseSchema: strings.TrimSpace(databaseSchema),
		MasterKey:      strings.TrimSpace(masterKey),
		TenantSlug:     strings.TrimSpace(tenantSlug),
		TenantName:     defaultName(strings.TrimSpace(tenantName), strings.TrimSpace(tenantSlug)),
		WorkspaceSlug:  strings.TrimSpace(workspaceSlug),
		WorkspaceName:  defaultName(strings.TrimSpace(workspaceName), strings.TrimSpace(workspaceSlug)),
	}
	result, err := importCredentialsDir(context.Background(), cfg, platform.CredentialImportOptions{
		Directory: strings.TrimSpace(dir),
		DryRun:    dryRun,
		Config:    &appconfig.Config{},
	})
	if encodeErr := writeJSON(stdout, result); encodeErr != nil && err == nil {
		err = encodeErr
	}
	if result.Failed > 0 && err == nil {
		err = fmt.Errorf("credential import completed with %d failures", result.Failed)
	}
	return err
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func defaultName(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
