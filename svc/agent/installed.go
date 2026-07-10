package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Scope mirrors update.Scope exactly so the two can be converted at the
// command boundary without sharing an import. agent cannot import update
// (update already imports agent) — so this duplication is intentional.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// InstalledKind discriminates a leaf in InstalledItem: a skill is a directory
// containing SKILL.md; a subagent is a flat .md file in the agent's agents dir.
type InstalledKind string

const (
	InstalledSkill    InstalledKind = "skill"
	InstalledSubagent InstalledKind = "subagent"
)

// InstalledLocation is one disk location where an (item, kind) pair lives.
// A single item may appear at several locations (different agents, both
// project + global scopes); they are aggregated onto one InstalledItem.
type InstalledLocation struct {
	Agent AgentType
	Scope Scope     // ScopeProject | ScopeGlobal
	Path  string    // absolute path on disk
}

// InstalledItem is one row in the remove TUI: the union of every location
// where (Name, Kind) is currently installed across the supported agents.
// Toggling the row removes ALL copies.
type InstalledItem struct {
	Name      string             // "writer" (skill) or "code-reviewer" (subagent)
	Kind      InstalledKind
	Locations []InstalledLocation
}

// DiscoverInstalled enumerates every skill and subagent currently on disk
// across all known agents (both project-relative and user-relative dirs).
// Missing directories are not errors: agents whose install root does not
// exist simply contribute zero items, which is the normal state for a fresh
// machine or a project that has only ever used a subset of agents.
//
// Items are grouped by (Name, Kind) so the same skill in three agents shows
// up as one row whose Locations list carries the agent-scope pairs. The
// returned slice is sorted by (Kind, Name) for stable TUI rendering.
//
// cwd anchors the project-relative dirs; if empty, os.Getwd() is used.
func DiscoverInstalled() ([]InstalledItem, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("discover installed: resolve cwd: %w", err)
	}

	// Keyed by (kind|name) so we can merge locations across agents/scopes.
	type key struct {
		kind InstalledKind
		name string
	}
	bucket := map[key]*InstalledItem{}

	add := func(kind InstalledKind, name string, loc InstalledLocation) {
		k := key{kind, name}
		it, ok := bucket[k]
		if !ok {
			it = &InstalledItem{Name: name, Kind: kind}
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
				add(InstalledSkill, name, InstalledLocation{
					Agent: a.Type, Scope: ScopeProject, Path: abs,
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
				add(InstalledSubagent, name, InstalledLocation{
					Agent: a.Type, Scope: ScopeProject, Path: abs,
				})
			}); err != nil {
				return nil, fmt.Errorf("discover installed: project agents for %s: %w", a.Type, err)
			}
		}

		// Global-scope paths (already absolute per Agents()).
		if a.UserSkillsDir != "" {
			if err := scanSkillsDir(a.UserSkillsDir, func(name, abs string) {
				add(InstalledSkill, name, InstalledLocation{
					Agent: a.Type, Scope: ScopeGlobal, Path: abs,
				})
			}); err != nil {
				return nil, fmt.Errorf("discover installed: user skills for %s: %w", a.Type, err)
			}
		}
		if a.UserAgentsDir != "" {
			if err := scanAgentsDir(a.UserAgentsDir, func(name, abs string) {
				add(InstalledSubagent, name, InstalledLocation{
					Agent: a.Type, Scope: ScopeGlobal, Path: abs,
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
			if it.Locations[i].Agent != it.Locations[j].Agent {
				return it.Locations[i].Agent < it.Locations[j].Agent
			}
			return it.Locations[i].Scope < it.Locations[j].Scope
		})
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool {
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