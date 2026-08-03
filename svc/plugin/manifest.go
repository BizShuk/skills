// manifest.go owns manifest interpretation: reading marketplace.json /
// plugin.json / skill.json under a plugin base and turning them into
// model.Parsed. Entry classification lives in skillentry.go; on-disk
// skill and subagent scanning lives in scan.go.
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
