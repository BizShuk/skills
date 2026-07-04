// Package tui renders a discovered discover.Catalog as an interactive
// checkbox tree and returns the user's selection. Rows for plugins whose
// remote fetch failed are listed as headers carrying an "unable to fetch"
// marker, and contribute no selectable rows of their own.
//
// The Model intentionally keeps no I/O and no bubbletea runtime state,
// which is what lets the package be unit-tested by feeding synthesized
// tea.KeyMsg values directly into Update without ever calling
// tea.NewProgram(...).Run(). Run is the only place that actually starts
// the bubbletea program; everything else is pure state transitions.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/skills/svc/discover"
	"github.com/bizshuk/skills/svc/install"
)

// row is one selectable skill line in the rendered tree. Category headers
// are NOT rows — they're context only and bypass the cursor entirely.
type row struct {
	category string
	skill    string
	path     string
	checked  bool
}

// Model is the bubbletea program state. Cursor, rows and the global flag
// are kept on the value receiver because bubbletea treats the model as
// immutable; mutations inside Update assign back to a local copy and
// return it.
type Model struct {
	cat    discover.Catalog
	rows   []row
	cursor int
	global bool
	done   bool
}

// NewModel materializes the model's rows from the given catalog. Every
// skill starts checked — the user can opt out with space — and failed
// plugins contribute zero rows (their presence is shown only via the
// "unable to fetch" header).
func NewModel(cat discover.Catalog) Model {
	m := Model{cat: cat}
	for _, c := range cat {
		for _, s := range c.Skills {
			m.rows = append(m.rows, row{
				category: c.PluginName,
				skill:    s.Name,
				path:     s.Path,
				checked:  true,
			})
		}
	}
	return m
}

// Init satisfies the bubbletea Model interface; we have no startup Cmd.
func (m Model) Init() tea.Cmd { return nil }

// Update dispatches key presses to cursor movement, toggle, and quit
// actions. Non-key messages are ignored. The returned Model carries the
// new cursor/checked state; the returned Cmd is nil except on enter
// (tea.Quit) and esc/ctrl-c (also tea.Quit, just without confirming).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case tea.KeySpace:
		if len(m.rows) > 0 {
			m.rows[m.cursor].checked = !m.rows[m.cursor].checked
		}
	case tea.KeyEnter:
		m.done = true
		return m, tea.Quit
	case tea.KeyCtrlC, tea.KeyEsc:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

// unableFetchMarker is the literal text we look for in tests; keep it
// spelled exactly like this so the tests don't get fragile.
const unableFetchMarker = "unable to fetch"

// View renders the tree. Each category header is `▸ <pluginName>`; if the
// category failed to fetch, the header is suffixed with `  [unable to
// fetch]` (plus the underlying FetchErr in parens, when it's a real
// error rather than the synthetic marker). Each skill row is
// `  > [x] <name>` when focused/checked, `  > [ ] <name>` when focused
// but unchecked, and `    [x] <name>` / `    [ ] <name>` when not
// focused.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString("Select skills to install (space toggle, enter confirm, esc cancel)\n\n")

	ri := 0
	for _, c := range m.cat {
		header := c.PluginName
		if !c.FetchOK {
			header += "  [" + unableFetchMarker + "]"
			// Surface the underlying error only when it carries info
			// beyond the synthetic marker — otherwise we'd just print
			// "unable to fetch (unable to fetch)".
			if c.FetchErr != "" && c.FetchErr != unableFetchMarker {
				header += " (" + c.FetchErr + ")"
			}
		}
		b.WriteString(fmt.Sprintf("▸ %s\n", header))

		for range c.Skills {
			r := m.rows[ri]
			cursor := "  "
			if ri == m.cursor {
				cursor = "> "
			}
			box := "[ ]"
			if r.checked {
				box = "[x]"
			}
			b.WriteString(fmt.Sprintf("  %s%s %s\n", cursor, box, r.skill))
			ri++
		}
	}
	return b.String()
}

// Selection returns the paths the user kept checked, paired with the
// current global flag. Agents is left to the caller; the TUI doesn't
// touch agent selection in this version.
func (m Model) Selection() install.Selection {
	paths := make([]string, 0, len(m.rows))
	for _, r := range m.rows {
		if r.checked {
			paths = append(paths, r.path)
		}
	}
	return install.Selection{SkillPaths: paths, Global: m.global}
}

// Run launches the bubbletea program on a fresh Model, blocks until quit,
// then casts the final model back to Model to extract the selection. The
// global flag is taken from the caller (cmd's --global) and re-applied
// to the returned Selection — this also covers the case where the user
// aborted, in which case the model carries the default false and we
// still honor the caller's flag.
func Run(cat discover.Catalog, global bool) (install.Selection, error) {
	m := NewModel(cat)
	m.global = global
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return install.Selection{}, err
	}
	fm, ok := final.(Model)
	if !ok {
		return install.Selection{}, fmt.Errorf("tui: unexpected final model type %T", final)
	}
	sel := fm.Selection()
	sel.Global = global
	return sel, nil
}
