package migrate

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type CredentialStore interface {
	LookupCredentialIDByRuntimeID(ctx context.Context, runtimeID string) (string, bool, error)
	Save(ctx context.Context, auth *coreauth.Auth) (string, error)
}

type CredentialRecordBuilder func(credentialName, runtimeID string, payload []byte) (*coreauth.Auth, error)

type CredentialImportOptions struct {
	Dir         string
	DryRun      bool
	BuildRecord CredentialRecordBuilder
	Out         io.Writer
	ErrOut      io.Writer
}

type CredentialImportSummary struct {
	Scanned  int
	Imported int
	Updated  int
	Skipped  int
	Failed   int
}

func ImportCredentialsFromDir(ctx context.Context, store CredentialStore, opts CredentialImportOptions) (CredentialImportSummary, error) {
	var summary CredentialImportSummary
	if store == nil {
		return summary, fmt.Errorf("credential store is required")
	}
	if opts.BuildRecord == nil {
		return summary, fmt.Errorf("credential record builder is required")
	}
	root, err := filepath.Abs(strings.TrimSpace(opts.Dir))
	if err != nil {
		return summary, fmt.Errorf("resolve import dir: %w", err)
	}
	if root == "" {
		return summary, fmt.Errorf("import dir is required")
	}
	var failures []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			summary.Failed++
			failures = append(failures, fmt.Sprintf("%s: %v", path, walkErr))
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			summary.Skipped++
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		summary.Scanned++
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			summary.Failed++
			failures = append(failures, fmt.Sprintf("%s: %v", path, relErr))
			return nil
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "." || strings.HasPrefix(rel, "../") {
			summary.Failed++
			failures = append(failures, fmt.Sprintf("%s: invalid relative path %s", path, rel))
			return nil
		}
		payload, readErr := osReadFile(path)
		if readErr != nil {
			summary.Failed++
			failures = append(failures, fmt.Sprintf("%s: %v", rel, readErr))
			return nil
		}
		_, exists, lookupErr := store.LookupCredentialIDByRuntimeID(ctx, rel)
		if lookupErr != nil {
			summary.Failed++
			failures = append(failures, fmt.Sprintf("%s: lookup failed: %v", rel, lookupErr))
			return nil
		}
		record, buildErr := opts.BuildRecord(rel, rel, payload)
		if buildErr != nil {
			summary.Failed++
			failures = append(failures, fmt.Sprintf("%s: %v", rel, buildErr))
			return nil
		}
		if opts.DryRun {
			if exists {
				summary.Updated++
			} else {
				summary.Imported++
			}
			writeLine(opts.Out, "%s [dry-run %s]", rel, importAction(exists))
			return nil
		}
		ref, saveErr := store.Save(ctx, record)
		if saveErr != nil {
			summary.Failed++
			failures = append(failures, fmt.Sprintf("%s: save failed: %v", rel, saveErr))
			return nil
		}
		if exists {
			summary.Updated++
		} else {
			summary.Imported++
		}
		if strings.TrimSpace(ref) != "" {
			writeLine(opts.Out, "%s [%s] -> %s", rel, importAction(exists), ref)
		} else {
			writeLine(opts.Out, "%s [%s]", rel, importAction(exists))
		}
		return nil
	})
	if err != nil {
		return summary, err
	}
	for _, failure := range failures {
		writeLine(opts.ErrOut, "ERROR %s", failure)
	}
	writeLine(opts.Out, "summary scanned=%d imported=%d updated=%d skipped=%d failed=%d", summary.Scanned, summary.Imported, summary.Updated, summary.Skipped, summary.Failed)
	if summary.Failed > 0 {
		return summary, fmt.Errorf("credential import finished with %d failures", summary.Failed)
	}
	return summary, nil
}

func importAction(exists bool) string {
	if exists {
		return "updated"
	}
	return "imported"
}

func writeLine(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}

var osReadFile = func(path string) ([]byte, error) {
	return io.ReadAll(mustOpen(path))
}

func mustOpen(path string) io.Reader {
	f, err := openFile(path)
	if err != nil {
		return errorReader{err: err}
	}
	return f
}

var openFile = func(path string) (io.ReadCloser, error) {
	return fsysOpen(path)
}

var fsysOpen = func(path string) (io.ReadCloser, error) {
	return osOpen(path)
}

var osOpen = func(path string) (io.ReadCloser, error) {
	return osOpenImpl(path)
}

var osOpenImpl = func(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

type errorReader struct{ err error }

func (r errorReader) Read(_ []byte) (int, error) { return 0, r.err }
