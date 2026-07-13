package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/bizshuk/skills/model"
)

// Scan reads the .claude-plugin/marketplace.json and .claude-plugin/plugin.json
// under base and returns the local and remote plugins declared in them. Local
// plugins get their Skills scanned on disk; remote plugins are returned as
// metadata only — the caller is expected to fetch them.
//
// Path traversal: every computed path derived from manifest data must be
// contained within base. Any path that escapes base (e.g. via "../") is
// silently dropped so a bad manifest entry cannot reach past the base dir.
func Scan(base string) (model.Parsed, error) {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return model.Parsed{}, err
	}
	var out model.Parsed
	if err := scanMarketplace(absBase, &out); err != nil {
		return model.Parsed{}, err
	}
	if err := scanPluginAtBase(absBase, &out); err != nil {
		return model.Parsed{}, err
	}
	if err := scanSkillJsonAtBase(absBase, &out); err != nil {
		return model.Parsed{}, err
	}
	out.Locals = dedupeLocalsByBase(out.Locals)
	return out, nil
}

// scanSkillJsonAtBase reads `<base>/skill.json` (legacy/alternative plugin format)
// and appends it to out as a model.LocalPlugin whose Base is base itself.
func scanSkillJsonAtBase(base string, out *model.Parsed) error {
	data, err := os.ReadFile(filepath.Join(base, "skill.json"))
	if err != nil {
		return nil
	}
	var mf struct {
		Name           string   `json:"name"`
		Agents         []string `json:"agents"`
		TopLevelAgents bool     `json:"topLevelAgents"`
	}
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil
	}
	if mf.Name == "" {
		return nil
	}
	lp := model.LocalPlugin{Name: mf.Name, Base: base, TopLevelAgents: mf.TopLevelAgents, AgentPaths: mf.Agents}
	scanSkills(base, &lp, nil)
	out.Locals = append(out.Locals, lp)
	return nil
}

// dedupeLocalsByBase collapses LocalPlugins that resolve to the same base
// directory. A repo that ships BOTH a marketplace.json self-entry (source
// "./") and a plugin.json describes the very same root plugin twice; scanning
// both would otherwise surface it as two identical categories. Keeping the
// first occurrence (marketplace before plugin.json) yields one category.
// Distinct bases — real sub-plugins under different subdirs — are preserved in
// their original order.
func dedupeLocalsByBase(locals []model.LocalPlugin) []model.LocalPlugin {
	seen := make(map[string]bool, len(locals))
	out := make([]model.LocalPlugin, 0, len(locals))
	for _, lp := range locals {
		key := filepath.Clean(lp.Base)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, lp)
	}
	return out
}

// marketplacePlugin describes one entry under the marketplace's `plugins[]`.
type marketplacePlugin struct {
	Name   string          `json:"name"`
	Source json.RawMessage `json:"source"` // string | remote-object
	Skills          []string `json:"skills"`
	Agents          []string `json:"agents"`
	TopLevelAgents  bool     `json:"topLevelAgents"`
}

type marketplaceManifest struct {
	Metadata struct {
		PluginRoot string `json:"pluginRoot"`
	} `json:"metadata"`
	Plugins []marketplacePlugin `json:"plugins"`
}

type pluginManifest struct {
	Name   string   `json:"name"`
	Skills           []string `json:"skills"`
	Agents           []string `json:"agents"`
	TopLevelAgents   bool     `json:"topLevelAgents"`
}

// scanMarketplace reads `<base>/.claude-plugin/marketplace.json` (if present)
// and appends each plugin entry to out. Missing files and malformed JSON are
// silently ignored per the design spec.
func scanMarketplace(base string, out *model.Parsed) error {
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
		lp := model.LocalPlugin{Name: p.Name, Base: pluginBase, TopLevelAgents: p.TopLevelAgents, AgentPaths: p.Agents}
		scanSkills(base, &lp, p.Skills)
		out.Locals = append(out.Locals, lp)
	}
	return nil
}

// scanPluginAtBase reads `<base>/.claude-plugin/plugin.json` (single plugin)
// and appends it to out as a model.LocalPlugin whose Base is base itself.
func scanPluginAtBase(base string, out *model.Parsed) error {
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
	lp := model.LocalPlugin{Name: mf.Name, Base: base, TopLevelAgents: mf.TopLevelAgents, AgentPaths: mf.Agents}
	scanSkills(base, &lp, mf.Skills)
	out.Locals = append(out.Locals, lp)
	return nil
}

// classifyRemote maps the object form of `source` to a model.RemotePlugin.
// Returns ok=false for unrecognized shapes or missing owner/repo info, so
// the caller can drop the entry silently per the design spec.
func classifyRemote(name string, obj map[string]any) (model.RemotePlugin, bool) {
	srcType, _ := obj["source"].(string)
	ref, _ := obj["ref"].(string)
	_ = obj["sha"]
	switch srcType {
	case "github":
		repo, _ := obj["repo"].(string)
		ownerRepo := normalizeOwnerRepo(repo)
		if ownerRepo == "" {
			return model.RemotePlugin{}, false
		}
		return model.RemotePlugin{
			Name:      name,
			OwnerRepo: ownerRepo,
			URL:       "https://github.com/" + ownerRepo + ".git",
			Ref:       ref,
		}, true
	case "url":
		urlStr, _ := obj["url"].(string)
		ownerRepo := deriveOwnerRepoFromURL(urlStr)
		if ownerRepo == "" {
			return model.RemotePlugin{}, false
		}
		return model.RemotePlugin{Name: name, OwnerRepo: ownerRepo, URL: urlStr, Ref: ref}, true
	case "git-subdir":
		urlStr, _ := obj["url"].(string)
		subdir, _ := obj["path"].(string)
		ownerRepo := deriveOwnerRepoFromURL(urlStr)
		if ownerRepo == "" {
			return model.RemotePlugin{}, false
		}
		return model.RemotePlugin{
			Name:      name,
			OwnerRepo: ownerRepo,
			URL:       urlStr,
			Ref:       ref,
			Subdir:    subdir,
		}, true
	}
	return model.RemotePlugin{}, false
}

// deriveOwnerRepoFromURL pulls an "owner/repo" pair out of a git URL. It
// uses the same regexp set as source.go since this is now the same package.
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
func scanSkills(base string, lp *model.LocalPlugin, additive []string) {
	seen := map[string]bool{}

	add := func(skillDir string) {
		if seen[skillDir] {
			return
		}
		seen[skillDir] = true
		desc := readDescription(filepath.Join(skillDir, "SKILL.md"))
		lp.Skills = append(lp.Skills, model.Skill{
			Name:        filepath.Base(skillDir),
			Path:        skillDir,
			Description: desc,
		})
	}

	// Conventional: <lp.Base>/skills/<name>/SKILL.md, <lp.Base>/.claude/skills/<name>/SKILL.md, <lp.Base>/.agents/skills/<name>/SKILL.md
	conventionalDirs := []string{
		filepath.Join(lp.Base, "skills"),
		filepath.Join(lp.Base, ".claude", "skills"),
		filepath.Join(lp.Base, ".agents", "skills"),
	}
	for _, conv := range conventionalDirs {
		if entries, err := os.ReadDir(conv); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				skillDir := filepath.Join(conv, e.Name())
				skillFile := filepath.Join(skillDir, "SKILL.md")
				if _, err := os.Stat(skillFile); err != nil {
					continue
				}
				add(skillDir)
			}
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

	// Subagents: scan .md files under agents/ directories.
	scanSubagents(lp)
}

// scanSubagents populates lp.Subagents from three sources, in order:
//  1. The conventional agents/ subdirs (lp.Base/agents/, lp.Base/.claude/agents/,
//     lp.Base/.agents/agents/) - always scanned.
//  2. Top-level .md files in lp.Base - only when lp.TopLevelAgents is true (set
//     via plugin.json's "topLevelAgents" field). This handles the "flat .md"
//     layout (e.g. awesome-claude-code-subagents where each category is a dir
//     of .md files) without auto-including unrelated top-level docs
//     (README, CHANGELOG, etc.).
//  3. Explicit AgentPaths from the manifest's "agents" array (e.g. plugin.json
//     "agents": ["./python-pro.md"]). Paths are relative to lp.Base.
//
// All sources are deduped by Name (basename minus .md) so the same subagent
// appearing in two sources shows up once in the TUI.
func scanSubagents(lp *model.LocalPlugin) {
	seenAgent := map[string]bool{}

	add := func(name, p string) {
		if name == "" || name == "README" {
			return
		}
		if seenAgent[name] {
			return
		}
		seenAgent[name] = true
		lp.Subagents = append(lp.Subagents, model.Subagent{
			Name:        name,
			Path:        p,
			Description: readDescription(p),
		})
	}

	// Source 1: conventional agents/ subdirs.
	agentDirs := []string{
		filepath.Join(lp.Base, "agents"),
		filepath.Join(lp.Base, ".claude", "agents"),
		filepath.Join(lp.Base, ".agents", "agents"),
	}
	for _, ad := range agentDirs {
		entries, err := os.ReadDir(ad)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if e.Name() == "README.md" {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".md")
			add(name, filepath.Join(ad, e.Name()))
		}
	}

	// Source 2: top-level .md files in lp.Base (opt-in).
	if lp.TopLevelAgents {
		if topEntries, terr := os.ReadDir(lp.Base); terr == nil {
			for _, te := range topEntries {
				if te.IsDir() {
					continue
				}
				if !strings.HasSuffix(te.Name(), ".md") {
					continue
				}
				if te.Name() == "README.md" {
					continue
				}
				name := strings.TrimSuffix(te.Name(), ".md")
				add(name, filepath.Join(lp.Base, te.Name()))
			}
		}
	}

	// Source 3: explicit AgentPaths from the manifest.
	for _, ap := range lp.AgentPaths {
		if ap == "" || !strings.HasSuffix(ap, ".md") {
			continue
		}
		var resolved string
		switch {
		case strings.HasPrefix(ap, "./"), strings.HasPrefix(ap, "../"):
			candidate := filepath.Join(lp.Base, ap)
			if !isContainedIn(candidate, lp.Base) {
				continue
			}
			resolved = candidate
		case filepath.IsAbs(ap):
			resolved = ap
		default:
			resolved = filepath.Join(lp.Base, ap)
		}
		if _, err := os.Stat(resolved); err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(resolved), ".md")
		add(name, resolved)
	}
}

// descMaxChars bounds the description preview the TUI renders per skill.
// Kept as a local re-export so existing references in scanSkills /
// scanSubagents compile unchanged; the real implementation lives in
// utils.ReadDescription so both install discovery and the remove
// discovery share one parsing path.
const descMaxChars = 60

// readDescription is a thin wrapper kept for the scanSkills /
// scanSubagents call sites below — the parser itself moved to
// model.ReadDescription so it can be shared with the remove flow.
func readDescription(path string) string { return model.ReadDescription(path) }


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

// hasAnyManifest reports whether ANY of the three manifest paths is reachable
// as a file under base. "Reachable" means os.Stat returned no error or any
// error other than os.IsNotExist (e.g. permission denied). A parse error on
// an existing-but-malformed file still counts as "exists" — we want the
// existing silent-ignore path to surface (or not) unchanged, and we do not
// want a transient permission glitch to flip a repo into fallback mode and
// emit a synthetic empty plugin.
func hasAnyManifest(base string) bool {
	paths := []string{
		filepath.Join(base, ".claude-plugin", "marketplace.json"),
		filepath.Join(base, ".claude-plugin", "plugin.json"),
		filepath.Join(base, "skill.json"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		} else if !os.IsNotExist(err) {
			return true
		}
	}
	return false
}

// hasAnyConventionalSkillsDir reports whether base contains at least one
// of the three conventional top-level skills directories. A file (not a
// directory) at any of the three paths does not count.
func hasAnyConventionalSkillsDir(base string) bool {
	paths := []string{
		filepath.Join(base, "skills"),
		filepath.Join(base, ".claude", "skills"),
		filepath.Join(base, ".agents", "skills"),
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// isInsideAgentDir reports whether base sits inside a conventional agents/
// directory. "Inside" means there is a path segment equal to "agents" that
// is NOT the first meaningful segment of base (a repo whose root folder is
// literally named "agents" is a valid fallback target — only nested
// agents/ subdirectories count), or a ".claude"/".agents" segment
// immediately followed by "agents". Path-segment match only — partial
// names (e.g. "agents-keeper") do not match.
func isInsideAgentDir(base string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(base)), "/")
	// Strip the leading empty segment from absolute paths so the index
	// math below lines up with "meaningful" path components.
	meaningful := parts
	if len(meaningful) > 0 && meaningful[0] == "" {
		meaningful = meaningful[1:]
	}
	for i, seg := range meaningful {
		if seg == "agents" && i > 0 {
			return true
		}
		if (seg == ".claude" || seg == ".agents") &&
			i+1 < len(meaningful) && meaningful[i+1] == "agents" {
			return true
		}
	}
	return false
}
