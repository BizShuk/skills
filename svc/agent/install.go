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

	seenSkills := make(map[string]bool)
	seenAgents := make(map[string]bool)

	for _, t := range sel.AgentTypes {
		a, ok := byType[t]
		if !ok {
			continue
		}

		// Skills: copy to SkillsDir, skipping duplicate resolved destinations.
		skillsRoot := a.ProjectSkillsDir
		if sel.Global {
			skillsRoot = a.UserSkillsDir
		}
		if root, ok := resolveUniqueRoot(skillsRoot, sel.Global, cwd, seenSkills); ok {
			for _, src := range sel.SkillPaths {
				name := filepath.Base(src)
				dst := filepath.Join(root, name)
				if err := utils.CopyTree(src, dst); err != nil {
					return fmt.Errorf("install: copy skill %s -> %s: %w", src, dst, err)
				}
			}
		}

		// Subagents: copy .md files to AgentsDir, skipping duplicate resolved destinations.
		agentsRoot := a.ProjectAgentsDir
		if sel.Global {
			agentsRoot = a.UserAgentsDir
		}
		if root, ok := resolveUniqueRoot(agentsRoot, sel.Global, cwd, seenAgents); ok {
			for _, src := range sel.SubagentPaths {
				dst := filepath.Join(root, filepath.Base(src))
				if err := utils.CopyTree(src, dst); err != nil {
					return fmt.Errorf("install: copy subagent %s -> %s: %w", src, dst, err)
				}
			}
		}
	}
	return nil
}

// resolveUniqueRoot anchors relative project paths under cwd, resolves symlinks
// via canonicalRoot, and returns false if that canonical destination was already
// processed in seen.
func resolveUniqueRoot(root string, global bool, cwd string, seen map[string]bool) (string, bool) {
	if root == "" {
		return "", false
	}
	if !global && !filepath.IsAbs(root) {
		root = filepath.Join(cwd, root)
	}
	canon := canonicalRoot(root)
	if seen[canon] {
		return "", false
	}
	seen[canon] = true
	return root, true
}

// canonicalRoot resolves symlinks on dir (or its deepest existing ancestor when
// dir has not been created yet) so shared paths and symlink aliases collapse to
// the same deduplication key.
func canonicalRoot(dir string) string {
	clean := filepath.Clean(dir)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved
	}
	var tail []string
	cur := clean
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return clean
		}
		tail = append(tail, filepath.Base(cur))
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			parts := make([]string, 0, len(tail)+1)
			parts = append(parts, resolved)
			for i := len(tail) - 1; i >= 0; i-- {
				parts = append(parts, tail[i])
			}
			return filepath.Join(parts...)
		}
		cur = parent
	}
}
