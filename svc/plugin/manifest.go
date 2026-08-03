package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/bizshuk/skills/model"
	"github.com/bizshuk/skills/svc/fetch"
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

	// Fallback: when no manifest declared this dir as a plugin, but it
	// still looks like a skill repo (has a conventional skills/ dir) and
	// it isn't itself an agents/ subdirectory, treat base as a synthetic
	// root plugin. This keeps "drop a skill in skills/ and try it" working
	// without requiring a manifest up front.
	//
	// The narrower case first: base IS a single skill directory. That is
	// what a subpath target ("owner/repo/skills/foo") materializes to, and
	// what a .md URL is laid out as — neither has a manifest or a skills/
	// dir, so without this they would scan to nothing. A dir that also has
	// a conventional skills/ child is a repo, not a skill, and falls
	// through to the broader fallback below.
	if !hasAnyManifest(absBase) && hasSkillFile(absBase) && !hasAnyConventionalSkillsDir(absBase) {
		out.Locals = append(out.Locals, model.LocalPlugin{
			Name: baseName(absBase),
			Base: absBase,
			Skills: []model.Skill{{
				Name:        baseName(absBase),
				Path:        absBase,
				Description: readDescription(filepath.Join(absBase, "SKILL.md")),
			}},
		})
		return out, nil
	}

	if !hasAnyManifest(absBase) &&
		hasAnyConventionalSkillsDir(absBase) &&
		!isInsideAgentDir(absBase) {
		lp := model.LocalPlugin{Name: baseName(absBase), Base: absBase}
		scanSkills(absBase, &lp, nil)
		out.Locals = append(out.Locals, lp)
	}

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
	Name           string            `json:"name"`
	Source         json.RawMessage   `json:"source"` // string | remote-object
	Skills         []json.RawMessage `json:"skills"`
	Agents         []string          `json:"agents"`
	TopLevelAgents bool              `json:"topLevelAgents"`
}

type marketplaceManifest struct {
	Metadata struct {
		PluginRoot string `json:"pluginRoot"`
	} `json:"metadata"`
	Plugins []marketplacePlugin `json:"plugins"`
}

type pluginManifest struct {
	Name           string
	SkillPaths     []string
	RemoteSkills   []model.RemotePlugin
	Agents         []string
	TopLevelAgents bool
}

type rawPluginManifest struct {
	Name           string            `json:"name"`
	Skills         []json.RawMessage `json:"skills"`
	Agents         []string          `json:"agents"`
	TopLevelAgents bool              `json:"topLevelAgents"`
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
		skillPaths, remoteSkills := parseManifestSkillEntries(p.Skills, pluginBase)
		if mf, ok := readPluginManifest(pluginBase); ok {
			skillPaths = append(skillPaths, mf.SkillPaths...)
			remoteSkills = append(remoteSkills, mf.RemoteSkills...)
		}
		lp := model.LocalPlugin{
			Name:           p.Name,
			Base:           pluginBase,
			TopLevelAgents: p.TopLevelAgents,
			AgentPaths:     p.Agents,
			RemoteSkills:   remoteSkills,
		}
		scanSkills(base, &lp, skillPaths)
		out.Locals = append(out.Locals, lp)
	}
	return nil
}

// scanPluginAtBase reads `<base>/.claude-plugin/plugin.json` (single plugin)
// and appends it to out as a model.LocalPlugin whose Base is base itself.
func scanPluginAtBase(base string, out *model.Parsed) error {
	mf, ok := readPluginManifest(base)
	if !ok {
		return nil
	}
	lp := model.LocalPlugin{
		Name:           mf.Name,
		Base:           base,
		TopLevelAgents: mf.TopLevelAgents,
		AgentPaths:     mf.Agents,
		RemoteSkills:   mf.RemoteSkills,
	}
	scanSkills(base, &lp, mf.SkillPaths)
	out.Locals = append(out.Locals, lp)
	return nil
}

func readPluginManifest(base string) (pluginManifest, bool) {
	data, err := os.ReadFile(filepath.Join(base, ".claude-plugin", "plugin.json"))
	if err != nil {
		return pluginManifest{}, false
	}
	var raw rawPluginManifest
	if err := json.Unmarshal(data, &raw); err != nil || raw.Name == "" {
		return pluginManifest{}, false
	}
	skillPaths, remoteSkills := parseManifestSkillEntries(raw.Skills, base)
	return pluginManifest{
		Name:           raw.Name,
		SkillPaths:     skillPaths,
		RemoteSkills:   remoteSkills,
		Agents:         raw.Agents,
		TopLevelAgents: raw.TopLevelAgents,
	}, true
}

// parseManifestSkillEntries splits a manifest's `skills` array into
// on-disk paths (resolved relative to pluginBase) and remote plugins to be
// fetched. String entries are classified by resolveSkillEntry; object
// entries are always remote sources.
func parseManifestSkillEntries(entries []json.RawMessage, pluginBase string) ([]string, []model.RemotePlugin) {
	var paths []string
	var remotes []model.RemotePlugin
	for _, entry := range entries {
		var path string
		if err := json.Unmarshal(entry, &path); err == nil {
			path = strings.TrimSpace(path)
			if path != "" {
				if rp, ok := resolveSkillEntry(path, pluginBase); ok {
					remotes = append(remotes, rp)
				} else {
					paths = append(paths, path)
				}
			}
			continue
		}

		var obj map[string]any
		if err := json.Unmarshal(entry, &obj); err != nil || len(obj) == 0 {
			continue
		}
		name, _ := obj["name"].(string)
		name = strings.TrimSpace(name)

		sourceObj := obj
		if nested, ok := obj["source"].(map[string]any); ok {
			sourceObj = nested
		}
		if name == "" {
			name = inferRemoteSkillName(sourceObj)
		}
		if name == "" {
			continue
		}
		if rp, ok := classifyRemote(name, sourceObj); ok {
			remotes = append(remotes, rp)
		}
	}
	return paths, remotes
}

// resolveSkillEntry decides what a string entry in a manifest's `skills`
// array actually names, and returns a RemotePlugin only when the entry is a
// repo. ok=false means "treat it as a path" — the caller hands it to
// resolveManifestSkillDirs.
//
// The decision is made against the target itself, in this order:
//
//  1. Explicit filesystem markers ("./", "../", absolute) are always paths.
//  2. Explicit remote markers ("github:", a URL scheme, "git@") are always
//     repos.
//  3. What is left is the genuinely ambiguous case: "skills/foo" is valid
//     GitHub shorthand AND a valid relative path. It is resolved against
//     the disk — if it exists under pluginBase it is a path, otherwise it
//     is treated as owner/repo. A repo checked out locally therefore wins
//     over a same-named repo on GitHub, which is what a plugin author
//     shipping their own skills directory expects.
func resolveSkillEntry(raw, pluginBase string) (model.RemotePlugin, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || fetch.IsLocalPath(raw) {
		return model.RemotePlugin{}, false
	}

	source, ref, _ := strings.Cut(strings.TrimPrefix(raw, "github:"), "#")
	explicitRemote := strings.HasPrefix(raw, "github:") ||
		strings.Contains(raw, "://") ||
		strings.HasPrefix(raw, "git@")

	if !explicitRemote && existsUnder(pluginBase, raw) {
		return model.RemotePlugin{}, false
	}

	// A full URL keeps its own address; only its identity has to be derived.
	// GitHub and GitLab URLs yield a real owner/repo, anything else (Gitea,
	// self-hosted, ssh remotes) falls back to the trailing two path segments
	// so the walker still has a stable key for the entry.
	if strings.Contains(source, "://") || strings.HasPrefix(source, "git@") {
		ownerRepo := fetch.OwnerRepoFromURL(source)
		if ownerRepo == "" {
			ownerRepo = trailingOwnerRepo(source)
		}
		if ownerRepo == "" {
			return model.RemotePlugin{}, false
		}
		return model.RemotePlugin{
			Name:      lastPathSegment(ownerRepo),
			OwnerRepo: ownerRepo,
			URL:       source,
			Ref:       ref,
		}, true
	}

	ownerRepo := fetch.NormalizeOwnerRepo(source)
	parts := strings.Split(ownerRepo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return model.RemotePlugin{}, false
	}
	return model.RemotePlugin{
		Name:      parts[1],
		OwnerRepo: ownerRepo,
		URL:       fetch.RepoURL(parts[0], parts[1]),
		Ref:       ref,
	}, true
}

// existsUnder reports whether rel names something that exists inside base.
// Entries that escape base do not count as existing, so a manifest cannot
// use "../../etc/passwd" to win the ambiguity check.
func existsUnder(base, rel string) bool {
	if base == "" {
		return false
	}
	candidate := filepath.Join(base, filepath.FromSlash(rel))
	if !isContainedIn(candidate, base) {
		return false
	}
	_, err := os.Stat(candidate)
	return err == nil
}

func inferRemoteSkillName(obj map[string]any) string {
	if repo, _ := obj["repo"].(string); repo != "" {
		return lastPathSegment(strings.TrimSuffix(repo, ".git"))
	}
	if urlStr, _ := obj["url"].(string); urlStr != "" {
		ownerRepo := fetch.OwnerRepoFromURL(urlStr)
		if ownerRepo != "" {
			return lastPathSegment(ownerRepo)
		}
		return lastPathSegment(strings.TrimSuffix(urlStr, ".git"))
	}
	return ""
}

// trailingOwnerRepo derives an "owner/repo"-shaped identity from a URL that
// neither GitHub nor GitLab claims (Gitea, self-hosted, ssh remotes), by
// taking its last two path segments. It exists only to give such entries a
// stable dedupe key; the fetch itself always uses the original URL.
func trailingOwnerRepo(rawURL string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(rawURL), ".git")
	if i := strings.Index(trimmed, "://"); i >= 0 {
		trimmed = trimmed[i+3:]
	}
	trimmed = strings.TrimPrefix(trimmed, "git@")
	trimmed = strings.Replace(trimmed, ":", "/", 1)
	segments := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(segments) < 2 {
		return ""
	}
	return strings.ToLower(segments[len(segments)-2] + "/" + segments[len(segments)-1])
}

func lastPathSegment(raw string) string {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" {
		return ""
	}
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		return raw[i+1:]
	}
	return raw
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
		ownerRepo := fetch.NormalizeOwnerRepo(repo)
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
		ownerRepo := fetch.OwnerRepoFromURL(urlStr)
		if ownerRepo == "" {
			return model.RemotePlugin{}, false
		}
		return model.RemotePlugin{Name: name, OwnerRepo: ownerRepo, URL: urlStr, Ref: ref}, true
	case "git-subdir":
		urlStr, _ := obj["url"].(string)
		subdir, _ := obj["path"].(string)
		ownerRepo := fetch.OwnerRepoFromURL(urlStr)
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

	// Additive paths may identify a SKILL.md file, a directory containing
	// SKILL.md directly, or a collection of direct <name>/SKILL.md children.
	for _, sp := range additive {
		for _, skillDir := range resolveManifestSkillDirs(base, lp.Base, sp) {
			add(skillDir)
		}
	}

	// Subagents: scan .md files under agents/ directories.
	scanSubagents(lp)
}

// resolveManifestSkillDirs turns one path entry from a manifest's `skills`
// array into the skill directories it names. The entry may be written with
// or without a "./" prefix, and may point at a SKILL.md file, a directory
// holding SKILL.md, or a collection directory whose children are skills.
//
// Absolute entries are honored only when they still land inside base;
// everything that escapes base — via "../" or an absolute path elsewhere on
// the machine — resolves to nothing, so a fetched manifest cannot read
// outside the tree it was fetched into.
func resolveManifestSkillDirs(base, pluginBase, manifestPath string) []string {
	manifestPath = strings.TrimSpace(manifestPath)
	if manifestPath == "" {
		return nil
	}
	candidate := manifestPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(pluginBase, filepath.FromSlash(manifestPath))
	}
	if !isContainedIn(candidate, base) {
		return nil
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		skillDir := filepath.Dir(candidate)
		if filepath.Base(candidate) == "SKILL.md" && hasSkillFile(skillDir) {
			return []string{skillDir}
		}
		return nil
	}
	if hasSkillFile(candidate) {
		return []string{candidate}
	}

	entries, err := os.ReadDir(candidate)
	if err != nil {
		return nil
	}
	var skillDirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(candidate, entry.Name())
		if hasSkillFile(skillDir) {
			skillDirs = append(skillDirs, skillDir)
		}
	}
	return skillDirs
}

// baseName is filepath.Base with a usable name for degenerate roots.
func baseName(dir string) string {
	name := filepath.Base(dir)
	if name == "." || name == string(filepath.Separator) {
		return "root"
	}
	return name
}

func hasSkillFile(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil && !info.IsDir()
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
