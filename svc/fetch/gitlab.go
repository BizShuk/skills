// gitlab.go materializes a GitLab target through the project archive
// endpoint, the GitLab counterpart of codeload. Subgroups are supported
// because the whole project path (group/subgroup/repo) goes into the URL
// verbatim.
package fetch

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// materializeGitLab downloads https://gitlab.com/<path>/-/archive/<ref>/<name>-<ref>.tar.gz
// and extracts it. Like the GitHub path, an empty Ref means the project's
// default branch, which GitLab resolves from the symbolic "HEAD".
func (f *httpFetcher) materializeGitLab(ctx context.Context, s ParsedSource) (string, error) {
	projectPath, err := parseGitLabProjectPath(s.URL)
	if err != nil {
		return "", err
	}

	ref := s.Ref
	if ref == "" {
		ref = "HEAD"
	}

	name := projectPath
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}

	archiveURL := fmt.Sprintf(
		"https://gitlab.com/%s/-/archive/%s/%s-%s.tar.gz",
		projectPath, url.PathEscape(ref), name, url.PathEscape(ref),
	)
	return f.fetchArchive(ctx, archiveURL, projectPath)
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
