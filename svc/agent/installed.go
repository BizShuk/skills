package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bizshuk/skills/model"
)

// Scope mirrors update.Scope exactly so the two can be converted at the
// command boundary without sharing an import. agent cannot import update
// (update already imports agent) — so this duplication is intentional.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// sectionOrder maps a Scope to its rendering priority in the remove TUI.
// Project is shown before global so the user's eye lands on the more
// context-specific installs first (cwd-relative); global is the
// always-on backup that's the same regardless of project.
var sectionOrder = map[Scope]int{
	ScopeProject: 0,
	ScopeGlobal:  1,
}

// InstalledKind discriminates a leaf in InstalledItem: a skill is a directory
// containing SKILL.md; a subagent is a flat .md file in the agent's agents dir.
type InstalledKind string

const (
	InstalledSkill    InstalledKind = "skill"
	InstalledSubagent InstalledKind = "subagent"
)

// InstalledLocation is one disk location where an InstalledItem lives.
// Multiple Locations on the same item mean the same skill is mirrored
// across several agents that share an install root (e.g. four agents
// all reading from .agents/skills). Each location belongs to exactly one
// Agent; the parent InstalledItem's Scope is the same for every location
// on it.
type InstalledLocation struct {
	Agent AgentType
	Path  string // absolute path on disk
}

// InstalledItem is one row in the remove TUI. Each row is fixed to a
// single scope (project OR global): a skill installed in both scopes
// shows as two rows, one per section, so the user can decide each
// independently. Locations lists every agent that has a copy at this
// scope; toggling the row removes all of them. Description is read from
// the skill's SKILL.md (or subagent .md frontmatter) at discovery time
// and rendered inline next to the name in the TUI.
type InstalledItem struct {
	Name        string             // "writer" (skill) or "code-reviewer" (subagent)
	Kind        InstalledKind
	Scope       Scope              // ScopeProject | ScopeGlobal
	Description string             // short summary for TUI rendering (may be empty)
	Locations   []InstalledLocation
}

// DiscoverInstalled enumerates every skill and subagent currently on disk
// across all known agents (both project-relative and user-relative dirs).
// Missing directories are not errors: agents whose install root does not
// exist simply contribute zero items, which is the normal state for a fresh
// machine or a project that has only ever used a subset of agents.
//
// Items are grouped by (Name, Kind, Scope) so the same skill in three agents
// at project scope shows up as one row with three Locations, and the same
// skill at both project + global scopes shows as TWO rows (one per
// section). The returned slice is sorted by (sectionOrder, Kind, Name) so
// the TUI can render the Project section before Global without an extra
// sort pass.
//
// cwd anchors the project-relative dirs; if empty, os.Getwd() is used.
func DiscoverInstalled() ([]InstalledItem, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("discover installed: resolve cwd: %w", err)
	}

	// Keyed by (kind|name|scope) so the same name at project and global
	// scopes becomes two distinct rows.
	type key struct {
		kind  InstalledKind
		name  string
		scope Scope
	}
	bucket := map[key]*InstalledItem{}

	add := func(kind InstalledKind, scope Scope, name, desc string, loc InstalledLocation) {
		k := key{kind, name, scope}
		it, ok := bucket[k]
		if !ok {
			it = &InstalledItem{Name: name, Kind: kind, Scope: scope, Description: desc}
			bucket[k] = it
		}
		it.Locations = append(it.Locations, loc)
	}

	for _, a := range Agents() {
		// Project-scope paths.
		if a.ProjectSkillsDir != "" {
			root := a.ProjectSkillsDir
			if !filepath.IsAbs(root) {
				root = filepath.Join(cwd, root)
			}
			if err := scanSkillsDir(root, func(name, abs string) {
				add(InstalledSkill, ScopeProject, name, model.ReadDescription(filepath.Join(abs, "SKILL.md")), InstalledLocation{
					Agent: a.Type, Path: abs,
				})
			}); err != nil {
				return nil, fmt.Errorf("discover installed: project skills for %s: %w", a.Type, err)
			}
		}
		if a.ProjectAgentsDir != "" {
			root := a.ProjectAgentsDir
			if !filepath.IsAbs(root) {
				root = filepath.Join(cwd, root)
			}
			if err := scanAgentsDir(root, func(name, abs string) {
				add(InstalledSubagent, ScopeProject, name, model.ReadDescription(abs), InstalledLocation{
					Agent: a.Type, Path: abs,
				})
			}); err != nil {
				return nil, fmt.Errorf("discover installed: project agents for %s: %w", a.Type, err)
			}
		}

		// Global-scope paths (already absolute per Agents()).
		if a.UserSkillsDir != "" {
			if err := scanSkillsDir(a.UserSkillsDir, func(name, abs string) {
				add(InstalledSkill, ScopeGlobal, name, model.ReadDescription(filepath.Join(abs, "SKILL.md")), InstalledLocation{
					Agent: a.Type, Path: abs,
				})
			}); err != nil {
				return nil, fmt.Errorf("discover installed: user skills for %s: %w", a.Type, err)
			}
		}
		if a.UserAgentsDir != "" {
			if err := scanAgentsDir(a.UserAgentsDir, func(name, abs string) {
				add(InstalledSubagent, ScopeGlobal, name, model.ReadDescription(abs), InstalledLocation{
					Agent: a.Type, Path: abs,
				})
			}); err != nil {
				return nil, fmt.Errorf("discover installed: user agents for %s: %w", a.Type, err)
			}
		}
	}

	out := make([]InstalledItem, 0, len(bucket))
	for _, it := range bucket {
		// Sort locations so the TUI renders the same string regardless of
		// the order in which Agents() happens to enumerate.
		sort.Slice(it.Locations, func(i, j int) bool {
			return it.Locations[i].Agent < it.Locations[j].Agent
		})
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool {
		if pi, pj := sectionOrder[out[i].Scope], sectionOrder[out[j].Scope]; pi != pj {
			return pi < pj
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// scanSkillsDir invokes add for every subdirectory of root that contains a
// SKILL.md file. A missing root is silent (returns nil); other read errors
// bubble up.
func scanSkillsDir(root string, add func(name, absPath string)) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
			continue
		}
		add(e.Name(), dir)
	}
	return nil
}

// scanAgentsDir invokes add for every .md file in root, treating the
// basename (minus .md extension) as the agent's name. README.md is
// skipped so a directory's own README never shows up as a subagent —
// the same convention used by svc/plugin/manifest.go's scanSubagents.
func scanAgentsDir(root string, add func(name, absPath string)) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if name == "README.md" {
			continue
		}
		add(strings.TrimSuffix(name, ".md"), filepath.Join(root, name))
	}
	return nil
}