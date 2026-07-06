package update

import (
	"context"
	"fmt"
	"os"

	"github.com/bizshuk/skills/svc/agent"
	"github.com/bizshuk/skills/svc/plugin"
	"github.com/bizshuk/skills/utils"
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
		if e.Scope == ScopeProject {
			if _, err := os.Stat(e.ProjectPath); os.IsNotExist(err) {
				fmt.Printf("[ERROR] project %s not found — entry removed (source: %s)\n", e.ProjectPath, e.Source)
				continue
			}
		}

		if err := updateEntry(e); err != nil {
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

// updateEntry re-discovers and re-installs one tracked entry.
func updateEntry(e Entry) error {
	ctx := context.Background()

	// Parse the original source.
	src, err := plugin.Parse(e.Source)
	if err != nil {
		return fmt.Errorf("parse source %q: %w", e.Source, err)
	}

	// Walk discovers the current skill/subagent tree from the source.
	cat, err := utils.Walk(ctx, plugin.New(), src, e.Depth)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}

	// Collect all skills and subagents (--yes semantics: install everything).
	allSkills := cat.AllSkills()
	allSubagents := cat.AllSubagents()

	var skillPaths []string
	for _, s := range allSkills {
		skillPaths = append(skillPaths, s.Path)
	}
	var subagentPaths []string
	for _, sa := range allSubagents {
		subagentPaths = append(subagentPaths, sa.Path)
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

	// Update recorded skills/subagents.
	e.Skills = make([]string, 0, len(allSkills))
	for _, s := range allSkills {
		e.Skills = append(e.Skills, s.Name)
	}
	e.Subagents = make([]string, 0, len(allSubagents))
	for _, sa := range allSubagents {
		e.Subagents = append(e.Subagents, sa.Name)
	}

	return nil
}
