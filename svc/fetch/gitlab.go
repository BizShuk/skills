// gitlab.go materializes a GitLab target through an archive download when
// possible, falling back to `git clone` so private projects work with local
// credentials. With GITLAB_TOKEN or PRIVATE_TOKEN the project archive API is
// used; without a token the public web archive URL is tried first.
package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Overridable in tests.
var gitlabBase = "https://gitlab.com"

// materializeGitLab tries an archive download first, then falls back to git.
func (f *httpFetcher) materializeGitLab(ctx context.Context, s ParsedSource) (string, error) {
	projectPath, err := parseGitLabProjectPath(s.URL)
	if err != nil {
		return "", err
	}

	ref := s.Ref
	if ref == "" {
		ref = "HEAD"
	}

	archiveURL, header := gitlabArchive(projectPath, ref)
	dir, err := f.fetchArchive(ctx, archiveURL, projectPath, header)
	if err == nil {
		return dir, nil
	}
	gitDir, gitErr := gitMaterialize(ctx, s)
	if gitErr == nil {
		return gitDir, nil
	}
	return "", fmt.Errorf("%w; git fallback failed: %v", err, gitErr)
}

// gitlabArchive builds the archive URL and optional PRIVATE-TOKEN header.
func gitlabArchive(projectPath, ref string) (string, http.Header) {
	if token := gitlabToken(); token != "" {
		// API form encodes the full project path (subgroups included).
		u := fmt.Sprintf(
			"%s/api/v4/projects/%s/repository/archive.tar.gz?sha=%s",
			gitlabBase, url.PathEscape(projectPath), url.QueryEscape(ref),
		)
		h := make(http.Header)
		h.Set("PRIVATE-TOKEN", token)
		return u, h
	}

	name := projectPath
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return fmt.Sprintf(
		"%s/%s/-/archive/%s/%s-%s.tar.gz",
		gitlabBase, projectPath, url.PathEscape(ref), name, url.PathEscape(ref),
	), nil
}

// gitlabToken reads GITLAB_TOKEN, then PRIVATE_TOKEN.
func gitlabToken() string {
	if t := strings.TrimSpace(os.Getenv("GITLAB_TOKEN")); t != "" {
		return t
	}
	return strings.TrimSpace(os.Getenv("PRIVATE_TOKEN"))
}

// parseGitLabProjectPath extracts the "group/subgroup/repo" path from a
// GitLab URL of the form produced by Parse.
func parseGitLabProjectPath(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid gitlab url %q: %w", rawURL, err)
	}
	if !strings.EqualFold(u.Host, "gitlab.com") {
		return "", fmt.Errorf("not a gitlab url: %q", rawURL)
	}
	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	if !strings.Contains(path, "/") {
		return "", fmt.Errorf("invalid gitlab url: %q", rawURL)
	}
	return path, nil
}
