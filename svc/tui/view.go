// view.go holds every rendering concern of the skill picker: the lipgloss
// styles, the glyph vocabulary, and the three phase views (tree, agents,
// level). It reads Model state and returns strings — no state transitions
// happen here; those live in tui.go.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/skills/svc/plugin"
)

// Style constants — lipgloss renders ANSI color codes; raw output stays legible
// in terminals without ANSI support.
var (
	pluginHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	nestedHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("Green")).Bold(true)
	fetchErrStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("Red"))
	skillNameStyle    = lipgloss.NewStyle()
	skillDescStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	checkedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("Green"))
	subagentNameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("171"))
)

// unableFetchMarker is the literal text we look for in tests; keep it
// spelled exactly like this so the tests don't get fragile.
const unableFetchMarker = "unable to fetch"

// checkbox glyphs for header aggregates and individual skills.
const (
	glyphUnchecked     = "○"
	glyphChecked       = "●"
	glyphIndeterminate = "▣"
	glyphSAUnchecked   = "◇" // ◇ diamond, hollow — unchecked subagent
	glyphSAChecked     = "◆" // ◆ diamond, filled — checked subagent
)

// countSummary totals the plugins, skills and subagents in the
// catalog, splitting out how many of them came from remote sources.
// The numbers feed the header line View renders.
func (m Model) countSummary() (totalPlugins, remotePlugins, totalSkills, remoteSkills, totalSubagents int) {
	var walk func(c *plugin.Category)
	walk = func(c *plugin.Category) {
		if c == nil {
			return
		}
		totalPlugins++
		if c.OwnerRepo != "" {
			remotePlugins++
		}
		totalSkills += len(c.Skills)
		if c.OwnerRepo != "" {
			remoteSkills += len(c.Skills)
		}
		totalSubagents += len(c.Subagents)
		for _, ch := range c.Children {
			walk(ch)
		}
	}
	for _, r := range m.cat.Roots {
		walk(r)
	}
	return
}

// View renders the tree. The header order matches the spec:
//
//	Select skills to install
//	Search: <input>
//	↑↓ move, space select, enter confirm
//
// Each header is `<indent>> <box> <pluginName>` (or `> …` if cursor).
// Each skill row is `<indent>> <box> <name> — <description>`, with the
// em-dash and description omitted entirely when there is no description.
// Categories that failed to fetch are suffixed with `  [unable to fetch]`
// plus the underlying error when it carries info beyond the marker.

// View renders the tree. The header order matches the spec:
//
//	Select skills to install
//	Search: <input>
//	↑↓ move, space select, enter confirm
//	(blank)
//	<body rows, clipped to viewportHeight, plus "↓ N more" if off-screen>
func (m Model) View() string {
	switch m.phase {
	case phaseAgents:
		return m.viewAgents()
	case phaseLevel:
		return m.viewLevel()
	}

	var b strings.Builder
	b.WriteString("Select skills to install\n")
	tp, rp, ts, rs, tsa := m.countSummary()
	b.WriteString(fmt.Sprintf("Plugins: %d (%d remote), Skills: %d (%d remote), Subagents: %d\n", tp, rp, ts, rs, tsa))
	b.WriteString("Search: ")
	b.WriteString(m.search.View())
	b.WriteString("\n")
	b.WriteString("↑↓ move, space select, enter next\n")

	if len(m.rows) == 0 {
		// Treat "no matches" and "empty catalog" both as a one-line hint,
		// so callers don't have to special-case zero rows.
		b.WriteString("\n")
		b.WriteString("(no matching skills)\n")
		return b.String()
	}

	b.WriteString("\n") // spacer between key hints and the tree

	h := m.viewportHeight
	if h <= 0 {
		h = defaultViewportHeight
	}
	start := m.offset
	end := start + h
	if end > len(m.rows) {
		end = len(m.rows)
	}

	for i := start; i < end; i++ {
		r := m.rows[i]
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		// Indent: two spaces per depth level for both headers and skills.
		indent := strings.Repeat("  ", r.depth)

		if r.isHeader {
			box := glyphUnchecked
			all, partial := m.headerCheckState(r.node)
			switch {
			case partial:
				box = glyphIndeterminate
			case all:
				box = glyphChecked
			}
			text := r.node.PluginName
			if r.node.OwnerRepo != "" {
				text += "  " + r.node.OwnerRepo
			}
			if !r.node.FetchOK {
				text += "  [" + unableFetchMarker + "]"
				if r.node.FetchErr != "" && r.node.FetchErr != unableFetchMarker {
					text += " (" + r.node.FetchErr + ")"
				}
			}
			b.WriteString(fmt.Sprintf("%s%s%s %s\n", indent, cursor, box, pluginHeaderStyle.Render(text)))
			continue
		}

		// Skill row rendering.
		if r.skill != nil {
			box := glyphUnchecked
			if m.checked[r.skill.Path] {
				box = checkedStyle.Render(glyphChecked)
			}
			var desc string
			isLong := len([]rune(r.skill.Description)) > 60
			unfolded := isLong && m.skillUnfolded[r.skill.Path]

			if r.skill.Description != "" {
				if isLong {
					if !unfolded {
						desc = " — " + truncateRune(r.skill.Description, 60)
					}
				} else {
					desc = " — " + r.skill.Description
				}
			}

			b.WriteString(fmt.Sprintf("%s%s%s %s%s\n", indent, cursor, box, r.skill.Name, desc))

			if unfolded {
				wrappedLines := wrapText(r.skill.Description, 80)
				descIndent := indent + "      "
				for _, line := range wrappedLines {
					b.WriteString(fmt.Sprintf("%s%s\n", descIndent, skillDescStyle.Render(line)))
				}
			}
			continue
		}

		// Subagent row rendering: diamond icon (◇/◆), purple name.
		if r.subagent != nil {
			box := glyphSAUnchecked
			if m.checkedSubagent[r.subagent.Path] {
				box = checkedStyle.Render(glyphSAChecked)
			}
			var desc string
			if r.subagent.Description != "" {
				desc = " — " + r.subagent.Description
			}
			b.WriteString(fmt.Sprintf("%s%s%s %s%s\n",
				indent, cursor, box, subagentNameStyle.Render(r.subagent.Name), desc))
			continue
		}
	}

	remaining := len(m.rows) - end
	if remaining > 0 {
		b.WriteString(fmt.Sprintf("↓ %d more\n", remaining))
	}
	return b.String()
}

// viewAgents renders the agent-selection phase.
func (m Model) viewAgents() string {
	var b strings.Builder
	b.WriteString("Select agents to install into\n")
	b.WriteString("↑↓ move, space select, enter next, esc back\n\n")

	if len(m.agents) == 0 {
		b.WriteString("(no agents available)\n")
		return b.String()
	}

	h := m.viewportHeight
	if h <= 0 {
		h = defaultViewportHeight
	}
	start := m.agentOffset
	end := start + h
	if end > len(m.agents) {
		end = len(m.agents)
	}

	for i := start; i < end; i++ {
		a := m.agents[i]
		cursor := "  "
		if i == m.agentCursor {
			cursor = "> "
		}
		box := glyphUnchecked
		if a.checked {
			box = checkedStyle.Render(glyphChecked)
		}
		text := a.displayName()
		if a.detected {
			text += "  (detected)"
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, box, pluginHeaderStyle.Render(text)))
	}

	remaining := len(m.agents) - end
	if remaining > 0 {
		b.WriteString(fmt.Sprintf("↓ %d more\n", remaining))
	}
	return b.String()
}

// levelOptions is the fixed two-row list rendered by viewLevel: index 0 is
// Project (cwd-relative install dirs), index 1 is Global (user-level, under
// $HOME). The order matters — it's what levelCursor indexes into and what
// the "m.global = m.levelCursor == 1" assignment in Update assumes.
var levelOptions = [2]string{
	"Project — install into ./.claude/skills etc., relative to the current directory",
	"Global — install into ~/.claude/skills etc., available in every project",
}

// viewLevel renders the install-level phase: a two-row radio choice between
// Project and Global. The checked glyph marks the currently selected level
// (m.global); the "> " cursor marks the row Up/Down last highlighted, which
// only becomes the selection once Space commits it.
func (m Model) viewLevel() string {
	var b strings.Builder
	b.WriteString("Install at Project or Global level?\n")
	b.WriteString("↑↓ move, space select, enter confirm, esc back\n\n")

	for i, label := range levelOptions {
		cursor := "  "
		if i == m.levelCursor {
			cursor = "> "
		}
		box := glyphUnchecked
		isGlobalRow := i == 1
		if m.global == isGlobalRow {
			box = checkedStyle.Render(glyphChecked)
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, box, label))
	}
	return b.String()
}

func truncateRune(line string, maxChars int) string {
	if n := len([]rune(line)); n > maxChars {
		runes := []rune(line)[:maxChars]
		return strings.TrimRight(string(runes), " ") + "..."
	}
	return line
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	var currentLine string
	for _, word := range words {
		if currentLine == "" {
			currentLine = word
		} else if len(currentLine)+1+len(word) <= width {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}
	return lines
}
