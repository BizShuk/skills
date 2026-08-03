// github.go materializes a GitHub target by downloading the repository
// tarball from codeload.github.com. No git binary and no clone is involved,
// which keeps a shallow read of a public repo to a single HTTP request.
package fetch

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// materializeGitHub resolves the codeload URL for the parsed source, then
// downloads and extracts it.
func (f *httpFetcher) materializeGitHub(ctx context.Context, s ParsedSource) (string, error) {
	owner, repo, err := parseGitHubOwnerRepo(s.URL)
	if err != nil {
		return "", err
	}

	ref := s.Ref
	if ref == "" {
		// GitHub codeload accepts the symbolic ref "HEAD" to mean the
		// repository's default branch. The resulting archive uses the
		// resolved branch name in its top-level directory (e.g. "main"),
		// which extractTarGZ strips dynamically.
		ref = "HEAD"
	}

	archiveURL := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", owner, repo, url.PathEscape(ref))
	return f.fetchArchive(ctx, archiveURL, owner+"/"+repo)
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
