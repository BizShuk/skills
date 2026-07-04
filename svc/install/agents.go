// Package install is the leaf utility that turns a TUI selection into
// filesystem state. It owns the agent install-location table (six supported
// agents), the detection of which of those agents the current user has set
// up, and a copy routine that writes the chosen skill directories into
// either the project-relative location or the user-level directory.
//
// This package is a leaf: it must not import any other svc/* package so it
// stays usable independently by tests, by the future skills use/remove
// subcommands, and by other tooling.
package install

import (
	"os"
	"path/filepath"
)

// AgentType is the canonical name of one supported agent (e.g. "claude-code").
// It is the only field callers need in order to address an agent symbolically
// in CLI flags or persisted config.
type AgentType string

// Agent is one row in the install-location table. ProjectSkillsDir /
// ProjectAgentsDir are paths relative to the current working directory when
// the Selection is in project mode; UserSkillsDir / UserAgentsDir are
// absolute paths anchored under $HOME. DetectDir is the absolute path whose
// presence on disk is the signal that the agent is installed.
//
// All path fields are stored in the table exactly as written in the spec.
// Empty fields are tolerated; an agent with empty UserSkillsDir is skipped
// silently by Apply.
type Agent struct {
	Type             AgentType
	ProjectSkillsDir string // relative to cwd when not absolute
	UserSkillsDir    string // absolute
	ProjectAgentsDir string // relative to cwd when not absolute
	UserAgentsDir    string // absolute
	DetectDir        string // if this dir exists on disk, the agent is "installed"
}

// agentsTable is the canonical list of supported agents, sourced from the
// design spec §Install Locations. The agents/* columns are inferred values
// per spec ("已知需日後對 upstream 查證修正") and may be corrected later —
// callers should not pin those columns in tests.
var agentsTable = []Agent{
	{
		Type:             "claude-code",
		ProjectSkillsDir: ".claude/skills",
		UserSkillsDir:    "$HOME/.claude/skills",
		ProjectAgentsDir: ".claude/agents",
		UserAgentsDir:    "$HOME/.claude/agents",
		DetectDir:        "$HOME/.claude",
	},
	{
		Type:             "antigravity",
		ProjectSkillsDir: ".agents/skills",
		UserSkillsDir:    "$HOME/.gemini/antigravity/skills",
		ProjectAgentsDir: ".agents/agents",
		UserAgentsDir:    "$HOME/.gemini/antigravity/agents",
		DetectDir:        "$HOME/.gemini/antigravity",
	},
	{
		Type:             "antigravity-cli",
		ProjectSkillsDir: ".agents/skills",
		UserSkillsDir:    "$HOME/.gemini/antigravity-cli/skills",
		ProjectAgentsDir: ".agents/agents",
		UserAgentsDir:    "$HOME/.gemini/antigravity-cli/agents",
		DetectDir:        "$HOME/.gemini/antigravity-cli",
	},
	{
		Type:             "codex",
		ProjectSkillsDir: ".agents/skills",
		UserSkillsDir:    "$HOME/.codex/skills",
		ProjectAgentsDir: ".agents/agents",
		UserAgentsDir:    "$HOME/.codex/agents",
		DetectDir:        "$HOME/.codex",
	},
	{
		Type:             "opencode",
		ProjectSkillsDir: ".agents/skills",
		UserSkillsDir:    "$HOME/.config/opencode/skills",
		ProjectAgentsDir: ".agents/agents",
		UserAgentsDir:    "$HOME/.config/opencode/agents",
		DetectDir:        "$HOME/.config/opencode",
	},
	{
		Type:             "hermes-agent",
		ProjectSkillsDir: ".hermes/skills",
		UserSkillsDir:    "$HOME/.hermes/skills",
		ProjectAgentsDir: ".hermes/agents",
		UserAgentsDir:    "$HOME/.hermes/agents",
		DetectDir:        "$HOME/.hermes",
	},
}

// Agents returns a copy of the canonical install-location table. Returning a
// copy (rather than the underlying slice) lets callers mutate freely without
// poisoning the package-level table.
func Agents() []Agent {
	out := make([]Agent, len(agentsTable))
	copy(out, agentsTable)
	return out
}

// Detect returns the subset of Agents() whose DetectDir currently exists on
// disk. This is how `skills add` decides which agents to pre-tick in the TUI.
//
// We read $HOME via os.Getenv directly (not via go-homedir) so tests that
// override HOME with t.Setenv take effect — go-homedir caches its result
// at package init and would otherwise ignore the override.
func Detect() []Agent {
	home := os.Getenv("HOME")
	var found []Agent
	for _, a := range agentsTable {
		if a.DetectDir == "" {
			continue
		}
		expanded := expandHome(a.DetectDir, home)
		info, err := os.Stat(expanded)
		if err != nil || !info.IsDir() {
			continue
		}
		found = append(found, a)
	}
	return found
}

// expandHome replaces a leading "$HOME" (or "~") with the supplied home
// directory. Non-prefixed paths are returned unchanged so absolute paths
// passed in as-is still work. The home argument is what the caller wants
// $HOME to be — Detect() reads it via os.Getenv, but tests can pass any
// string for unit-level behavior.
func expandHome(p, home string) string {
	if p == "" {
		return p
	}
	switch {
	case len(p) >= 5 && p[:5] == "$HOME":
		return filepath.Join(home, p[5:])
	case len(p) >= 1 && p[:1] == "~":
		return filepath.Join(home, p[1:])
	}
	return p
}