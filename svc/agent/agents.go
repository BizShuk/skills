// Package agent provides the install-location table and copy logic for skills.
// Agents() returns the canonical table translated from the embedded Provider
// definitions; Detect() returns the subset whose DetectDir exists on disk.
package agent

import (
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
)

// AgentType is the canonical name of one supported agent (e.g. "claude-code").
// It mirrors the internal Type but is kept as a separate alias so consumers
// don't depend on the internal agent package enum.
type AgentType = Type

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
	DisplayName      string // human-readable name for TUI rendering
	ProjectSkillsDir  string // relative to cwd when not absolute
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
	homedir.DisableCache = true
	expand := func(path string) string {
		if expanded, err := homedir.Expand(path); err == nil {
			return expanded
		}
		return path
	}

	src := LoadAll()
	out := make([]Agent, 0, len(src))
	for _, p := range src {
		out = append(out, Agent{
			Type:              p.Type,
			DisplayName:       p.DisplayName,
			ProjectSkillsDir:  p.ProjectSkillsDir,
			UserSkillsDir:     expand(p.UserSkillsDir),
			ProjectAgentsDir:  p.ProjectAgentsDir,
			UserAgentsDir:     expand(p.UserAgentsDir),
			DetectDir:         expand(p.DetectDir),
		})
	}
	return out
}

// Detect returns the subset of Agents() whose DetectDir currently exists on
// disk. This is what the TUI uses to pre-check detected agents.
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

// SkillNameFromPath returns the basename of a skill path (the directory name).
func SkillNameFromPath(path string) string {
	return filepath.Base(path)
}

// SubagentNameFromPath returns the basename of a subagent path (the .md filename
// without its parent directory).
func SubagentNameFromPath(path string) string {
	return filepath.Base(path)
}
