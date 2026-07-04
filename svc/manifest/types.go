// Package manifest scans .claude-plugin/marketplace.json and
// .claude-plugin/plugin.json to produce a Parsed view of the local plugins
// (with their Skills) and remote plugins (to be fetched by other layers).
package manifest

// Skill is a single skill directory within a local plugin. The directory
// must contain a SKILL.md file to be recognized.
type Skill struct {
	Name string // directory name (e.g. "web-design")
	Path string // absolute path to the skill directory
}

// LocalPlugin is a plugin whose skills live on disk under Base. Its Skills
// are the union of the conventional `<Base>/skills/<name>/SKILL.md` entries
// plus any additive entries declared in the manifest's `skills` array.
type LocalPlugin struct {
	Name   string   // grouping name taken from manifest
	Base   string   // absolute path of the plugin directory
	Skills []Skill  // union of conventional + additive skill dirs
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
