package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bizshuk/skills/utils"
)

// Selection is the input to Apply: which skill directories and subagent
// .md files the user wants installed, into which agents' install locations,
// and whether those locations are the project-relative ones or the
// user-level ones.
//
// SkillPaths are absolute paths to skill directories, each containing a
// SKILL.md file (validated upstream by discover). SubagentPaths are
// absolute paths to .md files under agents/ directories. Cwd is used to
// anchor relative Project*Dir values; when empty, Apply calls os.Getwd()
// so callers don't have to plumb the cwd themselves.
type Selection struct {
	SkillPaths    []string
	SubagentPaths []string
	AgentTypes    []AgentType
	Global        bool
	Cwd           string
}

// Apply copies each SkillPath into the destination skills root of each
// Agent, and each SubagentPath into the destination agents root of each
// Agent. In global mode the destination is the User*Dir (absolute); in
// project mode it is the Project*Dir joined with Cwd (when relative).
//
// An agent with empty User*Dir in global mode, or empty Project*Dir in
// project mode, is skipped silently. Missing source paths or copy errors
// bubble up immediately so a partial failure is visible to the user.
func Apply(sel Selection) error {
	cwd := sel.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("install: resolve cwd: %w", err)
		}
	}

	// Build a lookup table of all known agents.
	agentTable := Agents()
	byType := make(map[AgentType]Agent, len(agentTable))
	for _, a := range agentTable {
		byType[a.Type] = a
	}

	for _, t := range sel.AgentTypes {
		a, ok := byType[t]
		if !ok {
			continue
		}

		// Skills: copy to SkillsDir.
		skillsRoot := a.ProjectSkillsDir
		if sel.Global {
			skillsRoot = a.UserSkillsDir
		}
		if skillsRoot != "" {
			if !sel.Global && !filepath.IsAbs(skillsRoot) {
				skillsRoot = filepath.Join(cwd, skillsRoot)
			}
			for _, src := range sel.SkillPaths {
				name := filepath.Base(src)
				dst := filepath.Join(skillsRoot, name)
				if err := utils.CopyTree(src, dst); err != nil {
					return fmt.Errorf("install: copy skill %s -> %s: %w", src, dst, err)
				}
			}
		}

		// Subagents: copy .md files to AgentsDir.
		agentsRoot := a.ProjectAgentsDir
		if sel.Global {
			agentsRoot = a.UserAgentsDir
		}
		if agentsRoot != "" {
			if !sel.Global && !filepath.IsAbs(agentsRoot) {
				agentsRoot = filepath.Join(cwd, agentsRoot)
			}
			for _, src := range sel.SubagentPaths {
				dst := filepath.Join(agentsRoot, filepath.Base(src))
				if err := utils.CopyTree(src, dst); err != nil {
					return fmt.Errorf("install: copy subagent %s -> %s: %w", src, dst, err)
				}
			}
		}
	}
	return nil
}

