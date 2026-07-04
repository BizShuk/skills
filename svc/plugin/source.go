// source.go parses a user-supplied source string (owner/repo, git URL,
// local path, well-known URL) into a normalized ParsedSource.
package plugin

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// SourceType classifies a parsed source so downstream packages can pick
// the right materialization strategy (filesystem vs. git fetch vs. HTTP).
type SourceType int

const (
	Local SourceType = iota
	GitHub
	GitLab
	Git
	WellKnown
)

// ParsedSource is the normalized representation of a user-supplied source.
// All fields are independent and may be zero — call sites should branch on
// Type first, then consult the URL/Ref/Subpath/SkillFilter fields relevant
// to that type.
type ParsedSource struct {
	Type        SourceType
	URL         string // normalized git URL or absolute local path
	Ref         string // branch / tag / SHA; empty for Local and plain GitHub shorthand
	Subpath     string // path inside the repo (e.g. "skills/foo"); empty for whole-repo sources
	SkillFilter string // "owner/repo@skill" or "#ref@skill" skill-name filter; empty otherwise
	LocalPath   string // absolute path on disk; set when Type == Local, otherwise empty
}

var (
	reGitHubTreePath = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/tree/([^/]+)/(.+)`)
	reGitHubTree     = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/tree/([^/]+)$`)
	reGitHubRepo     = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`)
	reGitLabRepo     = regexp.MustCompile(`gitlab\.com/(.+?)(?:\.git)?/?$`)
	reAtSkill        = regexp.MustCompile(`^([^/]+)/([^/@]+)@(.+)$`)
	reShorthand      = regexp.MustCompile(`^([^/]+)/([^/]+)(?:/(.+?))?/?$`)
)

// sanitizeSubpath rejects any ".." segment that could escape the repo root.
func sanitizeSubpath(sub string) (string, error) {
	norm := strings.ReplaceAll(sub, `\`, "/")
	for _, seg := range strings.Split(norm, "/") {
		if seg == ".." {
			return "", fmt.Errorf("unsafe subpath %q: contains ..", sub)
		}
	}
	return sub, nil
}

func isLocalPath(in string) bool {
	return filepath.IsAbs(in) ||
		strings.HasPrefix(in, "./") ||
		strings.HasPrefix(in, "../") ||
		in == "." || in == ".."
}

// splitFragment extracts a trailing "#ref" or "#ref@skill" from git-like inputs.
func splitFragment(in string) (base, ref, skill string) {
	i := strings.Index(in, "#")
	if i < 0 {
		return in, "", ""
	}
	base = in[:i]
	frag := in[i+1:]
	if j := strings.Index(frag, "@"); j >= 0 {
		return base, frag[:j], frag[j+1:]
	}
	return base, frag, ""
}

// Parse turns a user-supplied source string into a normalized ParsedSource.
// It supports GitHub shorthand ("owner/repo" with optional subpath and
// "@skill" filter), GitHub full URLs (including /tree/branch/sub/path),
// GitHub "github:" prefix, GitLab URLs with subgroups, arbitrary git URLs,
// plain HTTPS "well-known" URLs, and local filesystem paths (relative or
// absolute). A "#ref" or "#ref@skill" fragment may be appended to git-like
// sources. Subpaths containing ".." are rejected.
func Parse(in string) (ParsedSource, error) {
	if isLocalPath(in) {
		abs, err := filepath.Abs(in)
		if err != nil {
			return ParsedSource{}, err
		}
		return ParsedSource{Type: Local, URL: abs, LocalPath: abs}, nil
	}

	base, fragRef, fragSkill := splitFragment(in)

	if strings.HasPrefix(base, "github:") {
		return Parse(reattach(strings.TrimPrefix(base, "github:"), fragRef, fragSkill))
	}
	if strings.HasPrefix(base, "gitlab:") {
		return Parse(reattach("https://gitlab.com/"+strings.TrimPrefix(base, "gitlab:"), fragRef, fragSkill))
	}

	if m := reGitHubTreePath.FindStringSubmatch(base); m != nil {
		sub, err := sanitizeSubpath(m[4])
		if err != nil {
			return ParsedSource{}, err
		}
		return ParsedSource{Type: GitHub, URL: ghURL(m[1], m[2]), Ref: m[3], Subpath: sub}, nil
	}
	if m := reGitHubTree.FindStringSubmatch(base); m != nil {
		return ParsedSource{Type: GitHub, URL: ghURL(m[1], m[2]), Ref: m[3]}, nil
	}
	if m := reGitHubRepo.FindStringSubmatch(base); m != nil {
		return ParsedSource{Type: GitHub, URL: ghURL(m[1], strings.TrimSuffix(m[2], ".git")), Ref: fragRef}, nil
	}
	if m := reGitLabRepo.FindStringSubmatch(base); m != nil && strings.Contains(m[1], "/") {
		return ParsedSource{Type: GitLab, URL: "https://gitlab.com/" + m[1] + ".git", Ref: fragRef}, nil
	}

	if !strings.Contains(base, ":") && !strings.HasPrefix(base, ".") && !strings.HasPrefix(base, "/") {
		if m := reAtSkill.FindStringSubmatch(base); m != nil {
			skill := m[3]
			if fragSkill != "" {
				skill = fragSkill
			}
			return ParsedSource{Type: GitHub, URL: ghURL(m[1], m[2]), Ref: fragRef, SkillFilter: skill}, nil
		}
		if m := reShorthand.FindStringSubmatch(base); m != nil {
			var sub string
			if m[3] != "" {
				s, err := sanitizeSubpath(m[3])
				if err != nil {
					return ParsedSource{}, err
				}
				sub = s
			}
			return ParsedSource{Type: GitHub, URL: ghURL(m[1], m[2]), Ref: fragRef, Subpath: sub, SkillFilter: fragSkill}, nil
		}
	}

	if isWellKnownURL(base) {
		return ParsedSource{Type: WellKnown, URL: base}, nil
	}

	return ParsedSource{Type: Git, URL: base, Ref: fragRef}, nil
}

func ghURL(owner, repo string) string {
	return "https://github.com/" + owner + "/" + strings.TrimSuffix(repo, ".git") + ".git"
}

func reattach(base, ref, skill string) string {
	if ref == "" {
		return base
	}
	if skill != "" {
		return base + "#" + ref + "@" + skill
	}
	return base + "#" + ref
}

func isWellKnownURL(in string) bool {
	if !strings.HasPrefix(in, "http://") && !strings.HasPrefix(in, "https://") {
		return false
	}
	u, err := url.Parse(in)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "github.com", "gitlab.com", "raw.githubusercontent.com":
		return false
	}
	return !strings.HasSuffix(in, ".git")
}
