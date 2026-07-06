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
