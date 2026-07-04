package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Scan reads the .claude-plugin/marketplace.json and .claude-plugin/plugin.json
// under base and returns the local and remote plugins declared in them. Local
// plugins get their Skills scanned on disk; remote plugins are returned as
// metadata only — the caller is expected to fetch them.
//
// Path traversal: every computed path derived from manifest data must be
// contained within base. Any path that escapes base (e.g. via "../") is
// silently dropped so a bad manifest entry cannot reach past the base dir.
func Scan(base string) (Parsed, error) {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return Parsed{}, err
	}
	var out Parsed
	if err := scanMarketplace(absBase, &out); err != nil {
		return Parsed{}, err
	}
	if err := scanPluginAtBase(absBase, &out); err != nil {
		return Parsed{}, err
	}
	return out, nil
}

// marketplacePlugin describes one entry under the marketplace's `plugins[]`.
type marketplacePlugin struct {
	Name   string          `json:"name"`
	Source json.RawMessage `json:"source"` // string | remote-object
	Skills []string        `json:"skills"`
}

type marketplaceManifest struct {
	Metadata struct {
		PluginRoot string `json:"pluginRoot"`
	} `json:"metadata"`
	Plugins []marketplacePlugin `json:"plugins"`
}

type pluginManifest struct {
	Name   string   `json:"name"`
	Skills []string `json:"skills"`
}

// scanMarketplace reads `<base>/.claude-plugin/marketplace.json` (if present)
// and appends each plugin entry to out. Missing files and malformed JSON are
// silently ignored per the design spec.
func scanMarketplace(base string, out *Parsed) error {
	data, err := os.ReadFile(filepath.Join(base, ".claude-plugin", "marketplace.json"))
	if err != nil {
		return nil
	}
	var mf marketplaceManifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil
	}
	pluginRoot := mf.Metadata.PluginRoot
	if pluginRoot != "" && !strings.HasPrefix(pluginRoot, "./") {
		// PluginRoot must start with "./" to be honored. Anything else
		// (including "../" or "/abs/...") is treated as missing.
		return nil
	}
	for _, p := range mf.Plugins {
		if p.Name == "" {
			continue
		}
		var sourceStr string
		var sourceObj map[string]any
		var isObject bool
		if len(p.Source) > 0 {
			if err := json.Unmarshal(p.Source, &sourceObj); err == nil && sourceObj != nil {
				isObject = true
			} else {
				_ = json.Unmarshal(p.Source, &sourceStr)
			}
		}
		if isObject {
			if rp, ok := classifyRemote(p.Name, sourceObj); ok {
				out.Remotes = append(out.Remotes, rp)
			}
			continue
		}
		var pluginBase string
		if sourceStr == "" {
			// Fallback: use base+pluginRoot when source is absent.
			pluginBase = filepath.Join(base, pluginRoot)
		} else if strings.HasPrefix(sourceStr, "./") || strings.HasPrefix(sourceStr, "../") {
			pluginBase = filepath.Join(base, pluginRoot, sourceStr)
		} else {
			continue
		}
		if !isContainedIn(pluginBase, base) {
			continue
		}
		lp := LocalPlugin{Name: p.Name, Base: pluginBase}
		scanSkills(base, &lp, p.Skills)
		out.Locals = append(out.Locals, lp)
	}
	return nil
}

// scanPluginAtBase reads `<base>/.claude-plugin/plugin.json` (single plugin)
// and appends it to out as a LocalPlugin whose Base is base itself.
func scanPluginAtBase(base string, out *Parsed) error {
	data, err := os.ReadFile(filepath.Join(base, ".claude-plugin", "plugin.json"))
	if err != nil {
		return nil
	}
	var mf pluginManifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil
	}
	if mf.Name == "" {
		return nil
	}
	lp := LocalPlugin{Name: mf.Name, Base: base}
	scanSkills(base, &lp, mf.Skills)
	out.Locals = append(out.Locals, lp)
	return nil
}

// classifyRemote maps the object form of `source` to a RemotePlugin.
// Returns ok=false for unrecognized shapes or missing owner/repo info, so
// the caller can drop the entry silently per the design spec.
func classifyRemote(name string, obj map[string]any) (RemotePlugin, bool) {
	srcType, _ := obj["source"].(string)
	ref, _ := obj["ref"].(string)
	_ = obj["sha"]
	switch srcType {
	case "github":
		repo, _ := obj["repo"].(string)
		ownerRepo := normalizeOwnerRepo(repo)
		if ownerRepo == "" {
			return RemotePlugin{}, false
		}
		return RemotePlugin{
			Name:      name,
			OwnerRepo: ownerRepo,
			URL:       "https://github.com/" + ownerRepo + ".git",
			Ref:       ref,
		}, true
	case "url":
		urlStr, _ := obj["url"].(string)
		ownerRepo := deriveOwnerRepoFromURL(urlStr)
		if ownerRepo == "" {
			return RemotePlugin{}, false
		}
		return RemotePlugin{Name: name, OwnerRepo: ownerRepo, URL: urlStr, Ref: ref}, true
	case "git-subdir":
		urlStr, _ := obj["url"].(string)
		subdir, _ := obj["path"].(string)
		ownerRepo := deriveOwnerRepoFromURL(urlStr)
		if ownerRepo == "" {
			return RemotePlugin{}, false
		}
		return RemotePlugin{
			Name:      name,
			OwnerRepo: ownerRepo,
			URL:       urlStr,
			Ref:       ref,
			Subdir:    subdir,
		}, true
	}
	return RemotePlugin{}, false
}

var (
	reGitHubRepo = regexp.MustCompile(`github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`)
	reGitLabRepo = regexp.MustCompile(`gitlab\.com/(.+?)(?:\.git)?/?$`)
)

// deriveOwnerRepoFromURL pulls an "owner/repo" pair out of a git URL. It
// is a stripped-down subset of svc/source.Parse so this package can stay
// independent of source (per the design's layering note).
func deriveOwnerRepoFromURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	// Strip the scheme so the regex sees the host+path part.
	candidate := rawURL
	if i := strings.Index(candidate, "://"); i >= 0 {
		candidate = candidate[i+3:]
	} else if strings.HasPrefix(candidate, "git@") {
		candidate = strings.TrimPrefix(candidate, "git@")
		candidate = strings.Replace(candidate, ":", "/", 1)
	}
	if m := reGitHubRepo.FindStringSubmatch(candidate); m != nil {
		return strings.ToLower(m[1] + "/" + strings.TrimSuffix(m[2], ".git"))
	}
	if m := reGitLabRepo.FindStringSubmatch(candidate); m != nil {
		return strings.ToLower(strings.TrimSuffix(m[1], ".git"))
	}
	return ""
}

// normalizeOwnerRepo lower-cases and trims suffixes from a GitHub-shorthand
// "owner/repo" string.
func normalizeOwnerRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimPrefix(repo, "github:")
	repo = strings.TrimSuffix(repo, ".git")
	if !strings.Contains(repo, "/") {
		return ""
	}
	return strings.ToLower(repo)
}

// scanSkills fills lp.Skills with the union of conventional entries
// (lp.Base/skills/<name>/SKILL.md) and additive entries (declared in
// the manifest's `skills` array, each treated as a path to SKILL.md).
// All entry paths must be contained within base; out-of-bounds entries
// are silently dropped.
//
// Each skill's Description is populated by reading the first non-empty
// non-heading line of its SKILL.md (truncated to descMaxChars + "...").
// Files that fail to read or have no body leave Description empty — the
// TUI renders empty parens for those.
func scanSkills(base string, lp *LocalPlugin, additive []string) {
	seen := map[string]bool{}

	add := func(skillDir string) {
		if seen[skillDir] {
			return
		}
		seen[skillDir] = true
		desc := readDescription(filepath.Join(skillDir, "SKILL.md"))
		lp.Skills = append(lp.Skills, Skill{
			Name:        filepath.Base(skillDir),
			Path:        skillDir,
			Description: desc,
		})
	}

	// Conventional: <lp.Base>/skills/<name>/SKILL.md
	conventional := filepath.Join(lp.Base, "skills")
	if entries, err := os.ReadDir(conventional); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillDir := filepath.Join(conventional, e.Name())
			skillFile := filepath.Join(skillDir, "SKILL.md")
			if _, err := os.Stat(skillFile); err != nil {
				continue
			}
			add(skillDir)
		}
	}

	// Additive: each entry is a path to SKILL.md relative to lp.Base.
	// The skill dir is its parent directory.
	for _, sp := range additive {
		if !strings.HasPrefix(sp, "./") {
			continue
		}
		candidate := filepath.Join(lp.Base, sp)
		skillDir := filepath.Dir(candidate)
		if !isContainedIn(skillDir, base) {
			continue
		}
		skillFile := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}
		add(skillDir)
	}
}

// descMaxChars bounds the description preview the TUI renders per skill.
// If SKILL.md's first body line exceeds this, truncate it and append
// "..." so the user still sees a non-clipped summary.
const descMaxChars = 60

// readDescription returns the first non-empty, non-heading line of path
// (treated as a markdown file), trimmed and truncated to descMaxChars
// runes. Returns "" if the file is unreadable, empty, or all headings.
func readDescription(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Skip Markdown ATX headings (#, ##, …). Also skip setext-style
		// underline (=== / ---) which appears under the title; we
		// recognize it by virtue of the previous line already being
		// skipped, and we filter it here too as a safety net.
		if strings.HasPrefix(line, "#") {
			continue
		}
		if line == "---" || line == "===" {
			continue
		}
		// Truncate by rune count, not byte count, so non-ASCII (CJK)
		// descriptions don't clip mid-character.
		if n := len([]rune(line)); n > descMaxChars {
			// Keep whole runes up to descMaxChars, then add ellipsis.
			runes := []rune(line)[:descMaxChars]
			return strings.TrimRight(string(runes), " ") + "..."
		}
		return line
	}
	return ""
}

// isContainedIn reports whether target resolves to a path inside (or equal
// to) base. It cleans both sides and uses filepath.Rel — a target outside
// base yields a relative path starting with "..".
func isContainedIn(target, base string) bool {
	cleanTarget := filepath.Clean(target)
	cleanBase := filepath.Clean(base)
	if cleanTarget == cleanBase {
		return true
	}
	rel, err := filepath.Rel(cleanBase, cleanTarget)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}
