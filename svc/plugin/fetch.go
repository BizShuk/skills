// fetch.go materializes a ParsedSource into a local directory on disk.
// The Fetcher interface is the single entry point: hand it a parsed source
// and it returns a path the rest of the pipeline (Walk, agent install) can
// read skills and manifest files from.
//
// Local sources are returned in place; GitHub sources are downloaded as a
// gzipped tarball from codeload.github.com and extracted into a fresh
// tempdir. The leading "<repo>-<ref>/" component of every archive entry is
// stripped so the returned directory IS the repo root, not a nested
// "<repo>-<ref>" wrapper.
package plugin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxAttempts is the total number of times we try a GitHub download before
// giving up. Per the project-wide CLAUDE.md convention ("max retry times is
// 5"), 5 covers the typical transient network blip without thrashing on a
// truly broken endpoint.
const maxAttempts = 5

// Fetcher turns a parsed source into a local directory path. Implementations
// are expected to be safe for concurrent use by multiple goroutines.
type Fetcher interface {
	Materialize(ctx context.Context, s ParsedSource) (string, error)
}

// New returns a production Fetcher backed by net/http. Tests in other
// packages (e.g. discover) can substitute their own Fetcher stub to avoid
// hitting the network; this package deliberately keeps the interface small
// and does not plumb a Client dependency.
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

// Materialize dispatches on the source type. Unknown types are rejected
// with an error rather than silently succeeding, since the rest of the
// pipeline assumes a non-empty directory.
func (f *httpFetcher) Materialize(ctx context.Context, s ParsedSource) (string, error) {
	switch s.Type {
	case Local:
		return materializeLocal(s)
	case GitHub:
		return f.materializeGitHub(ctx, s)
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

// materializeGitHub resolves the codeload URL for the parsed source, then
// downloads and extracts the tarball with up to maxAttempts retries on
// transient errors. The final error (if any) is wrapped with the
// "unable to fetch <owner>/<repo>" prefix the design spec requires.
func (f *httpFetcher) materializeGitHub(ctx context.Context, s ParsedSource) (string, error) {
	owner, repo, err := parseGitHubOwnerRepo(s.URL)
	if err != nil {
		return "", err
	}
	ownerRepo := owner + "/" + repo

	ref := s.Ref
	if ref == "" {
		// GitHub codeload accepts the symbolic ref "HEAD" to mean the
		// repository's default branch. The resulting archive uses the
		// resolved branch name in its top-level directory (e.g. "main"),
		// which we strip dynamically in extractTarGZ.
		ref = "HEAD"
	}

	archiveURL := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", owner, repo, url.PathEscape(ref))

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		dir, err := f.downloadAndExtract(ctx, archiveURL)
		if err == nil {
			return dir, nil
		}
		lastErr = err
		if !isTransient(err) {
			// 4xx and other permanent errors: stop retrying immediately.
			break
		}
		if attempt < maxAttempts {
			// Light exponential backoff (200ms, 400ms, 800ms, 1.6s) so we
			// don't hammer a struggling endpoint. Capped to keep the user
			// experience snappy when the network is just flapping.
			delay := time.Duration(1<<uint(attempt-1)) * 200 * time.Millisecond
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return "", fmt.Errorf("unable to fetch %s: %w", ownerRepo, lastErr)
}

// parseGitHubOwnerRepo extracts "owner/repo" from a GitHub URL of the form
// produced by source.Parse (e.g. "https://github.com/owner/repo.git" or
// "https://github.com/owner/repo"). A bare "owner/repo" string is also
// accepted.
func parseGitHubOwnerRepo(rawURL string) (owner, repo string, err error) {
	if strings.Contains(rawURL, "://") {
		u, perr := url.Parse(rawURL)
		if perr != nil {
			return "", "", fmt.Errorf("invalid github url %q: %w", rawURL, perr)
		}
		if !strings.EqualFold(u.Host, "github.com") {
			return "", "", fmt.Errorf("not a github url: %q", rawURL)
		}
		parts := strings.SplitN(strings.Trim(u.Path, "/"), "/", 3)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid github url: %q", rawURL)
		}
		owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
		return owner, repo, nil
	}
	// Bare "owner/repo" shorthand.
	parts := strings.SplitN(rawURL, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid github shorthand: %q", rawURL)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

// downloadAndExtract fetches the tarball once, classifies the result, and
// returns the extracted tempdir on success. The caller decides whether to
// retry based on isTransient.
func (f *httpFetcher) downloadAndExtract(ctx context.Context, archiveURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return "", transient(err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		// Network-level failure (DNS, dial, TLS, timeout) — always transient.
		return "", transient(err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		// fall through to extract
	case resp.StatusCode >= 500:
		// Server-side failure: transient. Discard the body so the connection
		// can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", transient(fmt.Errorf("http %d from %s", resp.StatusCode, archiveURL))
	default:
		// 4xx and anything else: permanent. The archive isn't going to
		// magically appear on retry.
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("http %d from %s", resp.StatusCode, archiveURL)
	}

	tmpDir, err := os.MkdirTemp("", "skills-fetch-*")
	if err != nil {
		return "", transient(err)
	}

	if err := extractTarGZ(resp.Body, tmpDir); err != nil {
		// Best-effort cleanup; ignore the error since we're already
		// returning one. The OS will eventually sweep the tempdir.
		_ = os.RemoveAll(tmpDir)
		return "", err
	}
	return tmpDir, nil
}

// extractTarGZ streams a gzipped tar archive from r into dest. The leading
// "<repo>-<ref>/" component of every entry name is stripped, and any entry
// whose path tries to escape dest (after stripping) is rejected. Symlinks
// and other special file types are skipped — we only materialize regular
// files and directories.
func extractTarGZ(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return transient(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	// We learn the top-level prefix from the first entry rather than
	// reconstructing it as "<repo>-<ref>/", because GitHub's codeload
	// resolves "HEAD" to the default branch (e.g. "main") and uses the
	// resolved name in the archive's leading directory.
	var topPrefix string
	seenAny := false

	absDest, err := filepath.Abs(dest)
	if err != nil {
		return transient(err)
	}

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return transient(err)
		}
		seenAny = true

		// Normalize the entry name: tar entries from GitHub use forward
		// slashes regardless of OS.
		name := strings.TrimPrefix(hdr.Name, "./")
		if name == "" {
			continue
		}
		// Establish / refresh the top-level prefix from the first non-empty
		// entry. We re-derive it on every entry in case a tarball uses mixed
		// prefixes (it shouldn't, but it's cheap to be defensive).
		if i := strings.Index(name, "/"); i >= 0 {
			topPrefix = name[:i+1]
		} else {
			topPrefix = ""
		}
		rel := strings.TrimPrefix(name, topPrefix)

		// Path traversal guard: reject entries that try to escape dest.
		// We check three things — the relative path itself, the joined
		// target path, and a final containment assertion.
		if rel == "" {
			// Top-level directory entry — nothing to write.
			if err := os.MkdirAll(absDest, 0o755); err != nil {
				return transient(err)
			}
			continue
		}
		if containsParent(rel) || filepath.IsAbs(rel) {
			return fmt.Errorf("archive entry %q escapes destination", hdr.Name)
		}
		target := filepath.Join(absDest, rel)
		cleaned := filepath.Clean(target)
		check, err := filepath.Rel(absDest, cleaned)
		if err != nil || check == ".." || strings.HasPrefix(check, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive entry %q escapes destination", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleaned, 0o755); err != nil {
				return transient(err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(cleaned), 0o755); err != nil {
				return transient(err)
			}
			if err := writeRegularFile(tr, cleaned, hdr.FileInfo().Mode()); err != nil {
				return transient(err)
			}
		default:
			// Skip symlinks, devices, fifos, pax headers, etc. We only
			// need regular files and directories to discover skills.
			continue
		}
	}

	if !seenAny {
		return fmt.Errorf("archive is empty")
	}
	return nil
}

// writeRegularFile copies the body of a tar entry to disk, applying the
// mode from the header (masked to permission bits to avoid setuid binaries
// sneaking in through a malicious archive).
func writeRegularFile(src io.Reader, dst string, mode os.FileMode) error {
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm()&0o777)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, src); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// containsParent reports whether any path segment in p is "..".
func containsParent(p string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// transientError wraps an error that may succeed on retry. We use a typed
// wrapper (rather than a sentinel) so the classification survives wrapping
// by %w at the call site.
type transientError struct {
	err error
}

func (e *transientError) Error() string { return e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }

func transient(err error) error { return &transientError{err: err} }

// isTransient reports whether err (or any wrapped error) was tagged as
// retryable by the downloader.
func isTransient(err error) bool {
	var te *transientError
	return errors.As(err, &te)
}
