// Package install is the leaf utility that turns a TUI selection into
// filesystem state. It owns the translation from agent.Provider definitions
// (sourced from the embedded svc/agent config files) to the install.Agent
// rows actually consumed by Apply, plus the detection of which agents the
// current user has set up, and the copy routine that writes the chosen
// skill directories into either project- or user-level locations.
//
// This package is a leaf: it must not import any other svc/* package EXCEPT
// svc/agent, which provides the source of truth for the supported agent
// list. Keeping install free of discover/source/manifest keeps the install
// step testable in isolation and reusable by future skills subcommands.
package install

import (
	"os"

	"github.com/bizshuk/skills/svc/agent"
)

// AgentType is the canonical name of one supported agent (e.g. "claude-code").
// It mirrors agent.Type but is kept as a separate alias so install does not
// leak the agent package's enum into every consumer's API.
type AgentType = agent.Type

// Agent is one row in the install-location table. ProjectSkillsDir /
// ProjectAgentsDir are paths relative to the current working directory when
// the Selection is in project mode; UserSkillsDir / UserAgentsDir are
// absolute paths anchored under $HOME. DetectDir is the absolute path whose
// presence on disk is the signal that the agent is installed.
//
// All `~/` paths are already expanded at construction time (when Agents()
// is called), so Apply and Detect never see a tilde.
type Agent struct {
	Type             AgentType
	ProjectSkillsDir string // relative to cwd when not absolute
	UserSkillsDir    string // absolute
	ProjectAgentsDir string // relative to cwd when not absolute
	UserAgentsDir    string // absolute
	DetectDir        string // if this dir exists on disk, the agent is "installed"
}

// Agents returns the canonical install-location table, translated from the
// embedded agent.Provider table. The translation runs every call so callers
// never see stale paths, and the returned slice is a fresh copy to prevent
// accidental mutation of the shared table.
func Agents() []Agent {
	src := agent.LoadAll()
	out := make([]Agent, 0, len(src))
	for _, p := range src {
		out = append(out, Agent{
			Type:             p.Type,
			ProjectSkillsDir: p.ProjectSkillsDir,
			UserSkillsDir:    agent.ExpandHome(p.UserSkillsDir),
			ProjectAgentsDir: p.ProjectAgentsDir,
			UserAgentsDir:    agent.ExpandHome(p.UserAgentsDir),
			DetectDir:        agent.ExpandHome(p.DetectDir),
		})
	}
	return out
}

// Detect returns the subset of Agents() whose DetectDir currently exists on
// disk. This is how `skills add` decides which agents to pre-tick in the TUI.
func Detect() []Agent {
	var found []Agent
	for _, a := range Agents() {
		if a.DetectDir == "" {
			continue
		}
		info, err := os.Stat(a.DetectDir)
		if err != nil || !info.IsDir() {
			continue
		}
		found = append(found, a)
	}
	return found
}