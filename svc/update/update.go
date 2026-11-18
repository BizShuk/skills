package update

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/bizshuk/skills/svc/agent"
	"github.com/bizshuk/skills/svc/discover"
	"github.com/bizshuk/skills/svc/fetch"
)

// Run loads the installs file and re-installs every tracked entry by
// re-discovering skills from the original source and applying them to the
// recorded agents. Project-level entries whose projectPath no longer exists
// on disk are removed from the file and reported as errors. Project-level
// skills and subagents the source no longer offers are deleted from the
// project. Each result is printed immediately so the user sees progress
// per-project.
func Run(args []string) error {
	f, err := Load()
	if err != nil {
		return fmt.Errorf("update: load installs: %w", err)
	}

	if len(f.Entries) == 0 {
		fmt.Println("no tracked installs — run \"skills add\" first")
		return nil
	}

	tracked := f.Entries // snapshot: names every entry tracked before this run
	var keep []Entry
	for i, e := range f.Entries {
		e := e
		if e.Scope == ScopeProject {
			if _, err := os.Stat(e.ProjectPath); os.IsNotExist(err) {
				fmt.Printf("[ERROR] project %s not found — entry removed (source: %s)\n", e.ProjectPath, e.Source)
				continue
			}
		}

		gone, err := updateEntry(&e)
		if err != nil {
			fmt.Printf("[ERROR] %s: %v\n", e.ProjectPath, err)
			keep = append(keep, e) // keep even on failure so user can retry
			continue
		}

		if e.Scope == ScopeProject {
			if err := pruneProject(e, gone, tracked, i); err != nil {
				fmt.Printf("[ERROR] %s: prune: %v\n", e.ProjectPath, err)
			}
			fmt.Printf("[OK] updated skills for %s\n", e.ProjectPath)
		} else {
			fmt.Printf("[OK] updated global skills from %s\n", e.Source)
		}
		keep = append(keep, e)
	}

	f.Entries = keep
	if err := Save(f); err != nil {
		return fmt.Errorf("update: save installs: %w", err)
	}
	return nil
}

// stale lists the tracked names an entry's source no longer offers.
type stale struct {
	Skills    []string
	Subagents []string
}

// updateEntry re-discovers and re-installs the skills and subagents one
// entry tracks. Items no longer offered by the source are dropped from the
// entry and returned; untracked items in the source are left alone, so
// `skills remove` stays effective across updates. When part of the source
// tree could not be fetched, nothing is dropped. e is updated in place.
func updateEntry(e *Entry) (stale, error) {
	ctx := context.Background()

	// Parse the original source.
	src, err := fetch.Parse(e.Source)
	if err != nil {
		return stale{}, fmt.Errorf("parse source %q: %w", e.Source, err)
	}

	// Walk discovers the current skill/subagent tree from the source.
	cat, err := discover.Walk(ctx, fetch.New(), src, e.Depth)
	if err != nil {
		return stale{}, fmt.Errorf("discover: %w", err)
	}

	var skillPaths, subagentPaths []string
	foundSkills, foundSubagents := map[string]bool{}, map[string]bool{}
	trackedSkills, trackedSubagents := toSet(e.Skills), toSet(e.Subagents)
	for _, s := range cat.AllSkills() {
		if name := agent.SkillNameFromPath(s.Path); trackedSkills[name] && !foundSkills[name] {
			skillPaths = append(skillPaths, s.Path)
			foundSkills[name] = true
		}
	}
	for _, sa := range cat.AllSubagents() {
		if name := agent.SubagentNameFromPath(sa.Path); trackedSubagents[name] && !foundSubagents[name] {
			subagentPaths = append(subagentPaths, sa.Path)
			foundSubagents[name] = true
		}
	}

	// Resolve agent types.
	agentTable := agent.Agents()
	byName := make(map[agent.AgentType]agent.Agent, len(agentTable))
	for _, a := range agentTable {
		byName[a.Type] = a
	}
	var agentTypes []agent.AgentType
	for _, name := range e.Agents {
		if _, ok := byName[agent.AgentType(name)]; ok {
			agentTypes = append(agentTypes, agent.AgentType(name))
		}
	}
	if len(agentTypes) == 0 {
		return stale{}, fmt.Errorf("no valid agent types in entry")
	}

	cwd := e.ProjectPath
	if e.Scope == ScopeGlobal {
		cwd = ""
	}

	if err := agent.Apply(agent.Selection{
		SkillPaths:    skillPaths,
		SubagentPaths: subagentPaths,
		AgentTypes:    agentTypes,
		Global:        e.Scope == ScopeGlobal,
		Cwd:           cwd,
	}); err != nil {
		return stale{}, fmt.Errorf("install: %w", err)
	}

	e.UpdatedAt = time.Now()
	if !cat.Complete() {
		// An unreachable plugin hides its skills; keep tracking them.
		return stale{}, nil
	}
	var gone stale
	e.Skills, gone.Skills = split(e.Skills, foundSkills)
	e.Subagents, gone.Subagents = split(e.Subagents, foundSubagents)
	return gone, nil
}

// pruneProject deletes the stale names of project entry e from its project
// dirs. A name another entry of the same project still tracks is kept,
// since agents can share a project dir (e.g. .agents/skills).
func pruneProject(e Entry, gone stale, tracked []Entry, self int) error {
	shared := map[string]bool{}
	for i, o := range tracked {
		if i == self || o.Scope != ScopeProject || o.ProjectPath != e.ProjectPath {
			continue
		}
		for _, n := range o.Skills {
			shared["skill:"+n] = true
		}
		for _, n := range o.Subagents {
			shared["subagent:"+n] = true
		}
	}
	var skills, subagents []string
	for _, n := range gone.Skills {
		if !shared["skill:"+n] {
			skills = append(skills, n)
		}
	}
	for _, n := range gone.Subagents {
		if !shared["subagent:"+n] {
			subagents = append(subagents, n)
		}
	}
	if len(skills) == 0 && len(subagents) == 0 {
		return nil
	}

	var types []agent.AgentType
	for _, n := range e.Agents {
		types = append(types, agent.AgentType(n))
	}
	removed, err := agent.Remove(agent.ProjectRemoveSelection(e.ProjectPath, types, skills, subagents))
	for _, n := range append(removed.ProjectSkills, removed.ProjectSubagents...) {
		fmt.Printf("[REMOVED] %s: %s (no longer offered by %s)\n", e.ProjectPath, n, e.Source)
	}
	return err
}

func toSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// split partitions names, in order, into those in found and those not.
func split(names []string, found map[string]bool) (kept, gone []string) {
	for _, n := range names {
		if found[n] {
			kept = append(kept, n)
		} else {
			gone = append(gone, n)
		}
	}
	return kept, gone
}
