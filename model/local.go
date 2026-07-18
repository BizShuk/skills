package model

// LocalPlugin is a plugin whose skills live on disk under Base. Its Skills
// are the union of the conventional `<Base>/skills/<name>/SKILL.md` entries
// plus any additive entries declared in the manifest's `skills` array.
// Subagents are .md files discovered under `<Base>/agents/`,
// `<Base>/.claude/agents/`, and `<Base>/.agents/agents/`.
type LocalPlugin struct {
	Name         string         // grouping name taken from manifest
	Base         string         // absolute path of the plugin directory
	Skills       []Skill        // union of conventional + additive skill dirs
	Subagents    []Subagent     // .md files under agents/ directories
	RemoteSkills []RemotePlugin // remote skill entries declared by plugin.json skills objects
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
