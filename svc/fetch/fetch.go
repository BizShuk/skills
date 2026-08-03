// fetch.go is the materialization half of the package: it turns a
// ParsedSource into a local directory the rest of the pipeline (Scan, Walk,
// agent install) can read skills and manifest files from.
//
// Dispatch is by target type — local paths are returned in place, GitHub and
// GitLab repos are downloaded as archives, any other git URL is cloned, and
// a plain https URL is fetched as a document. Every non-local strategy
// materializes into a fresh tempdir whose root IS the repo root, so callers
// never have to strip a "<repo>-<ref>" wrapper.
package fetch

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Fetcher turns a parsed source into a local directory path. Implementations
// are expected to be safe for concurrent use by multiple goroutines.
type Fetcher interface {
	Materialize(ctx context.Context, s ParsedSource) (string, error)
}

// New returns a production Fetcher backed by net/http (plus the `git` binary
// for generic git URLs). Tests in other packages can substitute their own
// Fetcher stub to avoid hitting the network; this package deliberately keeps
// the interface small and does not plumb a Client dependency.
func New() Fetcher {
	return &httpFetcher{
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// httpFetcher is the default Fetcher implementation. It uses a single
// http.Client (thread-safe) shared across all Materialize calls.
type httpFetcher struct {
	client *http.Client
}

// Materialize dispatches on the source type, then narrows the result to
// s.Subpath when the target named one. Unknown types are rejected with an
// error rather than silently succeeding, since the rest of the pipeline
// assumes a non-empty directory.
func (f *httpFetcher) Materialize(ctx context.Context, s ParsedSource) (string, error) {
	dir, err := f.materialize(ctx, s)
	if err != nil {
		return "", err
	}
	return applySubpath(dir, s.Subpath)
}

func (f *httpFetcher) materialize(ctx context.Context, s ParsedSource) (string, error) {
	switch s.Type {
	case Local:
		return materializeLocal(s)
	case GitHub:
		return f.materializeGitHub(ctx, s)
	case GitLab:
		return f.materializeGitLab(ctx, s)
	case Git:
		return materializeGit(ctx, s)
	case WellKnown:
		return f.materializeURL(ctx, s)
	default:
		return "", fmt.Errorf("fetch: unsupported source type for %q", s.URL)
	}
}

// materializeLocal returns s.LocalPath as-is, but only if it points to a
// real directory. A missing path produces the exact error message
// "local path not found: <path>" so callers (and tests) can match on it.
func materializeLocal(s ParsedSource) (string, error) {
	if s.LocalPath == "" {
		return "", fmt.Errorf("local path not found: <empty>")
	}
	info, err := os.Stat(s.LocalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("local path not found: %s", s.LocalPath)
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("local path not found: %s", s.LocalPath)
	}
	return s.LocalPath, nil
}

// applySubpath narrows a materialized directory to the repo-internal path
// the target named (e.g. "owner/repo/skills/foo" or a /tree/main/skills/foo
// URL). An empty subpath is a no-op. The joined path must stay inside dir
// and must exist as a directory — a subpath that escapes or is missing is an
// error rather than a silent fall back to the repo root, so the user is not
// handed the whole repo when they asked for one skill.
func applySubpath(dir, subpath string) (string, error) {
	if subpath == "" {
		return dir, nil
	}
	candidate := filepath.Join(dir, filepath.FromSlash(subpath))
	rel, err := filepath.Rel(dir, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("fetch: subpath %q escapes the fetched source", subpath)
	}
	info, err := os.Stat(candidate)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("fetch: subpath %q not found in the fetched source", subpath)
	}
	return candidate, nil
}
