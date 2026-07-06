// Package model declares the manifest value types (Skill, LocalPlugin,
// RemotePlugin, Parsed) produced by Scan and consumed by Walk.
package model

// Skill is a single skill directory within a local plugin. The directory
// must contain a SKILL.md file to be recognized.
//
// Description is a short human-readable summary the TUI renders next to
// each skill row. It is the first non-empty non-heading line of SKILL.md,
// truncated to fit in a one-line preview; empty when the file is
// unreadable / has no body.
type Skill struct {
	Name        string // directory name (e.g. "web-design")
	Path        string // absolute path to the skill directory
	Description string // short summary for TUI rendering (may be empty)
}

// Subagent is a single subagent .md file under an agents/ directory within
// a local plugin. Unlike skills (which are directories containing SKILL.md),
// subagents are flat .md files whose filename (minus .md extension) is the
// subagent name.
//
// Description is extracted from the .md file using the same YAML-frontmatter
// + first-body-line logic as skills (readDescription).
type Subagent struct {
	Name        string // filename without .md extension (e.g. "code-reviewer")
	Path        string // absolute path to the .md file
	Description string // short summary for TUI rendering (may be empty)
}

// LocalPlugin is a plugin whose skills live on disk under Base. Its Skills
// are the union of the conventional `<Base>/skills/<name>/SKILL.md` entries
// plus any additive entries declared in the manifest's `skills` array.
// Subagents are .md files discovered under `<Base>/agents/`,
// `<Base>/.claude/agents/`, and `<Base>/.agents/agents/`.
type LocalPlugin struct {
	Name      string     // grouping name taken from manifest
	Base      string     // absolute path of the plugin directory
	Skills    []Skill    // union of conventional + additive skill dirs
	Subagents []Subagent // .md files under agents/ directories
	// TopLevelAgents, when true, also scans top-level .md files in Base
	// as subagents (the "flat .md" layout used by some marketplaces). Defaults
	// to false; opt in via plugin.json's "topLevelAgents" field.
	TopLevelAgents bool

	// AgentPaths is the explicit list of subagent .md files declared by the
	// manifest via the "agents" array (e.g. plugin.json's "agents": ["./foo.md"]
	// or marketplace.json's sub-plugin entry with the same field). Paths are
	// relative to Base at parse time. The scanner resolves these into
	// Subagent entries after dedup; if a path is relative, it is joined with
	// Base at resolution time. Set by scanSkills from mf.Agents / p.Agents.
	AgentPaths []string
}

// RemotePlugin is a plugin whose skills must be fetched from another repo.
// OwnerRepo + URL + Ref identify the source; Subdir narrows it inside the
// repo for git-subdir sources.
type RemotePlugin struct {
	Name      string // grouping name taken from manifest
	OwnerRepo string // normalized lowercase "owner/repo"
	URL       string // full git URL or marketplace entry url
	Ref       string // pinned branch or tag (may be empty)
	Subdir    string // repo-internal path (git-subdir sources only)
}

// Parsed is the combined output of Scan: every local plugin discovered on
// disk under base plus every remote plugin declared in either manifest.
type Parsed struct {
	Locals  []LocalPlugin
	Remotes []RemotePlugin
}
