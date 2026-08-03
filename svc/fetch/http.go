// http.go materializes a plain https target — a URL that names a document
// rather than a repository. Two shapes are supported, chosen by the URL's
// last path segment:
//
//	*.tar.gz / *.tgz  → extracted as an archive, same as a repo tarball
//	*.md              → written as <tmp>/skills/<name>/SKILL.md, i.e. a
//	                    one-skill repo in the conventional layout
//
// Anything else is an error: guessing at an arbitrary HTML page would hand
// the scanner a directory it cannot make sense of.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (f *httpFetcher) materializeURL(ctx context.Context, s ParsedSource) (string, error) {
	u, err := url.Parse(s.URL)
	if err != nil {
		return "", fmt.Errorf("fetch: invalid url %q: %w", s.URL, err)
	}
	last := path.Base(u.Path)

	switch {
	case strings.HasSuffix(last, ".tar.gz"), strings.HasSuffix(last, ".tgz"):
		return f.fetchArchive(ctx, s.URL, s.URL)
	case strings.HasSuffix(last, ".md"):
		return f.fetchSkillDoc(ctx, s.URL, skillDirNameFromPath(u.Path))
	}
	return "", fmt.Errorf(
		"fetch: unsupported url target %q: expected a .tar.gz archive or a .md skill document", s.URL)
}

// fetchSkillDoc downloads a single markdown document and lays it out in the
// conventional layout the scanner already knows — skills/<name>/SKILL.md —
// so a one-document target needs no special case downstream.
func (f *httpFetcher) fetchSkillDoc(ctx context.Context, rawURL, name string) (string, error) {
	resp, err := f.get(ctx, rawURL)
	if err != nil {
		return "", fmt.Errorf("unable to fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("unable to fetch %s: %w", rawURL, err)
	}

	tmpDir, err := os.MkdirTemp("", "skills-fetch-*")
	if err != nil {
		return "", err
	}
	skillDir := filepath.Join(tmpDir, "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", err
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), body, 0o644); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", err
	}
	return tmpDir, nil
}

// skillDirNameFromPath derives the skill directory name from a URL path.
// ".../web-design/SKILL.md" names the skill "web-design" (the filename
// carries no information), while ".../web-design.md" names it "web-design".
// A path that yields nothing usable falls back to "skill".
func skillDirNameFromPath(urlPath string) string {
	segments := strings.Split(strings.Trim(urlPath, "/"), "/")
	last := segments[len(segments)-1]
	if strings.EqualFold(last, "SKILL.md") && len(segments) > 1 {
		return segments[len(segments)-2]
	}
	name := strings.TrimSuffix(last, ".md")
	if name == "" {
		return "skill"
	}
	return name
}
