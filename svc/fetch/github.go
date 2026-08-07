// github.go materializes a GitHub target. Public repos are fetched as a
// single HTTP tarball (codeload). Private repos need credentials: when
// GITHUB_API_TOKEN is set the GitHub API tarball endpoint is used;
// otherwise (or when the archive request still fails) the code falls back
// to `git clone` so local SSH keys and credential helpers work.
package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Overridable in tests so the git-clone fallback can be exercised offline.
var (
	githubAPIBase      = "https://api.github.com"
	githubCodeloadBase = "https://codeload.github.com"
	gitMaterialize     = materializeGitWithRemoteFallback
)

// materializeGitHub tries an archive download first, then falls back to git.
func (f *httpFetcher) materializeGitHub(ctx context.Context, s ParsedSource) (string, error) {
	owner, repo, err := parseGitHubOwnerRepo(s.URL)
	if err != nil {
		return "", err
	}

	ref := s.Ref
	if ref == "" {
		// GitHub codeload / API accept the symbolic ref "HEAD" to mean the
		// repository's default branch. The resulting archive uses the
		// resolved branch name in its top-level directory (e.g. "main"),
		// which extractTarGZ strips dynamically.
		ref = "HEAD"
	}

	archiveURL, header := githubArchive(owner, repo, ref)
	label := owner + "/" + repo
	dir, err := f.fetchArchive(ctx, archiveURL, label, header)
	if err == nil {
		return dir, nil
	}
	// Private repos return 404 on unauthenticated codeload. Fall back to
	// git so local credentials (SSH, gh auth, credential helper) apply.
	gitDir, gitErr := gitMaterialize(ctx, s)
	if gitErr == nil {
		return gitDir, nil
	}
	return "", fmt.Errorf("%w; git fallback failed: %v", err, gitErr)
}

// githubArchive picks the archive URL and auth headers.
// With GITHUB_API_TOKEN → api.github.com (works for private repos).
// Without → codeload.github.com (public only, no auth header).
func githubArchive(owner, repo, ref string) (string, http.Header) {
	refPath := url.PathEscape(ref)
	if token := githubToken(); token != "" {
		u := fmt.Sprintf("%s/repos/%s/%s/tarball/%s", githubAPIBase, owner, repo, refPath)
		h := make(http.Header)
		h.Set("Authorization", "Bearer "+token)
		h.Set("Accept", "application/vnd.github+json")
		return u, h
	}
	return fmt.Sprintf("%s/%s/%s/tar.gz/%s", githubCodeloadBase, owner, repo, refPath), nil
}

// githubToken reads GITHUB_API_TOKEN.
func githubToken() string {
	return strings.TrimSpace(os.Getenv("GITHUB_API_TOKEN"))
}

// parseGitHubOwnerRepo extracts "owner/repo" from a GitHub URL of the form
// produced by Parse (e.g. "https://github.com/owner/repo.git" or
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
