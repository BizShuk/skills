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
// on disk are removed from the file and reported as errors. Each result is
// printed immediately so the user sees progress per-project.
func Run(args []string) error {
	f, err := Load()
	if err != nil {
		return fmt.Errorf("update: load installs: %w", err)
	}

	if len(f.Entries) == 0 {
		fmt.Println("no tracked installs — run \"skills add\" first")
		return nil
	}

	var keep []Entry
	for _, e := range f.Entries {
		e := e
		if e.Scope == ScopeProject {
			if _, err := os.Stat(e.ProjectPath); os.IsNotExist(err) {
				fmt.Printf("[ERROR] project %s not found — entry removed (source: %s)\n", e.ProjectPath, e.Source)
				continue
			}
		}

		if err := updateEntry(&e); err != nil {
			fmt.Printf("[ERROR] %s: %v\n", e.ProjectPath, err)
			keep = append(keep, e) // keep even on failure so user can retry
			continue
		}

		if e.Scope == ScopeProject {
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

// updateEntry re-discovers and re-installs the skills and subagents one
// entry tracks. Items no longer offered by the source are dropped from the
// entry; untracked items in the source are left alone, so `skills remove`
// stays effective across updates. e is updated in place.
func updateEntry(e *Entry) error {
	ctx := context.Background()

	// Parse the original source.
	src, err := fetch.Parse(e.Source)
	if err != nil {
		return fmt.Errorf("parse source %q: %w", e.Source, err)
	}

	// Walk discovers the current skill/subagent tree from the source.
	cat, err := discover.Walk(ctx, fetch.New(), src, e.Depth)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}

	tracked := func(names []string) map[string]bool {
		m := make(map[string]bool, len(names))
		for _, n := range names {
			m[n] = true
		}
		return m
	}
	trackedSkills, trackedSubagents := tracked(e.Skills), tracked(e.Subagents)

	var skillPaths, skillNames []string
	for _, s := range cat.AllSkills() {
		if name := agent.SkillNameFromPath(s.Path); trackedSkills[name] {
			skillPaths = append(skillPaths, s.Path)
			skillNames = append(skillNames, name)
		}
	}
	var subagentPaths, subagentNames []string
	for _, sa := range cat.AllSubagents() {
		if name := agent.SubagentNameFromPath(sa.Path); trackedSubagents[name] {
			subagentPaths = append(subagentPaths, sa.Path)
			subagentNames = append(subagentNames, name)
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
		return fmt.Errorf("no valid agent types in entry")
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
		return fmt.Errorf("install: %w", err)
	}

	e.Skills = skillNames
	e.Subagents = subagentNames
	e.UpdatedAt = time.Now()
	return nil
}
