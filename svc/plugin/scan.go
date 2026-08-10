// scan.go walks a local plugin's directory tree to populate its Skills and
// Subagents — the conventional layouts plus whatever the manifest declared
// additively. Everything here is disk-facing; nothing parses a manifest.
package plugin

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bizshuk/skills/model"
	"github.com/bizshuk/skills/utils"
)

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

	// Conventional: <lp.Base>/skills/<name>/SKILL.md — the source repo's own
	// skills only. Agent install destinations (.claude/skills, .agents/skills)
	// are deliberately NOT scanned: those hold skills already installed into
	// that repo from elsewhere, and offering them again would re-publish a
	// copy under the wrong provenance.
	conv := filepath.Join(lp.Base, "skills")
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
//  1. The conventional agents/ dir (lp.Base/agents/) - always scanned. Agent
//     install destinations (.claude/agents, .agents/agents) are deliberately
//     NOT scanned, for the same reason as their skills counterparts in
//     scanSkills: they hold subagents already installed into that repo from
//     elsewhere.
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

	// Source 1: the repo's own conventional agents/ dir.
	ad := filepath.Join(lp.Base, "agents")
	if entries, err := os.ReadDir(ad); err == nil {
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
// scanSubagents call sites below — the parser itself lives in
// utils.ReadDescription so it can be shared with the remove flow.
func readDescription(path string) string { return utils.ReadDescription(path) }

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
