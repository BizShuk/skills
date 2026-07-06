package model

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
