// archive.go holds the download-and-extract machinery shared by every
// archive-based target (GitHub codeload, GitLab archive, plain .tar.gz URL).
// The retry loop and the transient/permanent classification come from
// utils.Retry / utils.Retryable; what stays here is the tar.gz extractor
// that strips the archive's leading directory.
package fetch

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	gohttp "github.com/bizshuk/gosdk/http"
)

// fetchArchive downloads and extracts archiveURL, retrying transient
// failures under the shared utils retry policy. Optional header values are
// sent on every attempt (e.g. Authorization / PRIVATE-TOKEN for private
// repos). The final error (if any) is wrapped with an "unable to fetch
// <label>" prefix so callers surface a stable message.
func (f *httpFetcher) fetchArchive(ctx context.Context, archiveURL, label string, header http.Header) (string, error) {
	// Light exponential backoff (200ms, 400ms, 800ms, 1.6s) so we don't
	// hammer a struggling endpoint; utils.Retry stops early on permanent
	// errors (4xx) because downloadAndExtract leaves those untagged.
	dir, err := gohttp.Retry(ctx, gohttp.DefaultRetryPolicy(), func(ctx context.Context) (string, error) {
		return f.downloadAndExtract(ctx, archiveURL, header)
	})
	if err == nil {
		return dir, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	return "", fmt.Errorf("unable to fetch %s: %w", label, err)
}

// downloadAndExtract fetches the tarball once, classifies the result, and
// returns the extracted tempdir on success. The caller decides whether to
// retry based on the utils.Retryable tag.
func (f *httpFetcher) downloadAndExtract(ctx context.Context, archiveURL string, header http.Header) (string, error) {
	resp, err := f.get(ctx, archiveURL, header)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	tmpDir, err := os.MkdirTemp("", "skills-fetch-*")
	if err != nil {
		return "", gohttp.Retryable(err)
	}

	if err := extractTarGZ(resp.Body, tmpDir); err != nil {
		// Best-effort cleanup; ignore the error since we're already
		// returning one. The OS will eventually sweep the tempdir.
		_ = os.RemoveAll(tmpDir)
		return "", err
	}
	return tmpDir, nil
}

// get performs a single GET and returns the response only for a 2xx status.
// Retryable statuses (429 and 5xx, per gohttp.IsRetryableStatus) are tagged
// transient; every other 4xx is permanent. The body of a non-2xx response is
// drained and closed so the connection can be reused.
//
// 429 used to fall through to the permanent branch here while svc/rule and
// svc/token both retried it — sharing one classifier removes that split, at
// the cost of archive downloads now backing off on a rate limit instead of
// failing outright. That is what GitHub codeload actually wants.
func (f *httpFetcher) get(ctx context.Context, rawURL string, header http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, gohttp.Retryable(err)
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	// GitHub's API rejects requests without a User-Agent; set a stable one
	// when the caller did not supply their own.
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "skills-cli")
	}
	resp, err := f.client.Do(req)
	if err != nil {
		// Network-level failure (DNS, dial, TLS, timeout) — always transient.
		return nil, gohttp.Retryable(err)
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return resp, nil
	case gohttp.IsRetryableStatus(resp.StatusCode):
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return nil, gohttp.Retryable(fmt.Errorf("http %d from %s", resp.StatusCode, rawURL))
	default:
		// Remaining 4xx and anything else: permanent. The archive isn't
		// going to magically appear on retry.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("http %d from %s", resp.StatusCode, rawURL)
	}
}

// extractTarGZ streams a gzipped tar archive from r into dest. The leading
// "<repo>-<ref>/" component of every entry name is stripped, and any entry
// whose path tries to escape dest (after stripping) is rejected. Symlinks
// and other special file types are skipped — we only materialize regular
// files and directories.
func extractTarGZ(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return gohttp.Retryable(err)
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
		return gohttp.Retryable(err)
	}

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return gohttp.Retryable(err)
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
				return gohttp.Retryable(err)
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
				return gohttp.Retryable(err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(cleaned), 0o755); err != nil {
				return gohttp.Retryable(err)
			}
			if err := writeRegularFile(tr, cleaned, hdr.FileInfo().Mode()); err != nil {
				return gohttp.Retryable(err)
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
