// Package tui also renders the `skills remove` flow. The remove UI is a
// single-phase model: one flat list with one row per InstalledItem, search
// filter, multi-select via Space, and Enter to confirm. There is no agent
// or level phase — a row's checked state means "delete this skill from
// every agent it's currently installed in".
//
// Reuse: viewport math (defaultViewportHeight), the lipgloss style palette
// (pluginHeaderStyle, checkedStyle, subagentNameStyle), and the textinput
// search field mirror what tui.go does for `add`. We intentionally don't
// share the bubbletea Model type with tui.go — `add` is a tree with three
// phases, `remove` is a flat list with one phase — merging them would
// require either phantom phase values or a feature flag, both worse than
// a small dedicated model.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/bizshuk/skills/svc/agent"
)

// removeModel is the bubbletea model for the remove flow.
type removeModel struct {
	items   []agent.InstalledItem
	visible []int // indices into items after filter
	cursor  int
	offset  int

	search      textinput.Model
	searchQuery string

	checked map[string]bool // key = kind|name
	done    bool
	cancel  bool
}

// removeRowKey is the dedupe key for an InstalledItem across filter passes.
func removeRowKey(it agent.InstalledItem) string {
	return string(it.Kind) + "|" + it.Name
}

// NewRemoveModel builds the model from a discovered item list. Every item
// starts unchecked — the user opts in with space, just like the add flow.
func NewRemoveModel(items []agent.InstalledItem) removeModel {
	m := removeModel{
		items:   items,
		checked: make(map[string]bool),
		search:  textinput.New(),
	}
	m.search.Prompt = ""
	m.search.Placeholder = ""
	m.search.Focus()
	m.rebuildVisible()
	m.ensureCursorVisible()
	return m
}

// rebuildVisible refreshes m.visible against the current search query and
// resets the cursor if it falls off the end. Items match the query when
// their Name OR any of their agent names OR their kind substring contains
// the lower-cased query.
func (m *removeModel) rebuildVisible() {
	q := strings.ToLower(strings.TrimSpace(m.searchQuery))
	out := make([]int, 0, len(m.items))
	for i, it := range m.items {
		if q == "" {
			out = append(out, i)
			continue
		}
		if strings.Contains(strings.ToLower(it.Name), q) {
			out = append(out, i)
			continue
		}
		if strings.Contains(strings.ToLower(string(it.Kind)), q) {
			out = append(out, i)
			continue
		}
		for _, loc := range it.Locations {
			if strings.Contains(strings.ToLower(string(loc.Agent)), q) {
				out = append(out, i)
				break
			}
		}
	}
	m.visible = out
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// ensureCursorVisible advances m.offset so the cursor lands in the viewport.
func (m *removeModel) ensureCursorVisible() {
	h := defaultViewportHeight
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	maxOffset := len(m.visible) - h
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// Init satisfies the bubbletea Model interface.
func (m removeModel) Init() tea.Cmd { return nil }

// Update dispatches keys to search, cursor movement, toggle, commit, cancel.
func (m removeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case tea.KeyCtrlC:
		m.cancel = true
		return m, tea.Quit
	case tea.KeyEsc:
		if m.search.Value() != "" {
			m.search.SetValue("")
			m.searchQuery = ""
			m.rebuildVisible()
			m.cursor = 0
			m.offset = 0
			m.ensureCursorVisible()
			return m, nil
		}
		m.cancel = true
		return m, tea.Quit
	case tea.KeyEnter:
		m.done = true
		return m, tea.Quit
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			m.ensureCursorVisible()
		}
		return m, nil
	case tea.KeyDown:
		if m.cursor < len(m.visible)-1 {
			m.cursor++
			m.ensureCursorVisible()
		}
		return m, nil
	case tea.KeySpace:
		if len(m.visible) == 0 {
			return m, nil
		}
		it := m.items[m.visible[m.cursor]]
		k := removeRowKey(it)
		m.checked[k] = !m.checked[k]
		return m, nil
	}

	// Any other key: feed to the search input.
	prev := m.search.Value()
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	if m.search.Value() != prev {
		m.searchQuery = m.search.Value()
		m.rebuildVisible()
		m.cursor = 0
		m.offset = 0
		m.ensureCursorVisible()
	}
	return m, cmd
}

// View renders the remove list. Layout mirrors tui.go's "add" view closely:
// header line, totals, search field, key hints, body rows, "↓ N more" if
// off-screen.
func (m removeModel) View() string {
	var b strings.Builder
	b.WriteString("Select installed skills/subagents to remove\n")

	var nSkills, nSubagents int
	for _, it := range m.items {
		switch it.Kind {
		case agent.InstalledSkill:
			nSkills++
		case agent.InstalledSubagent:
			nSubagents++
		}
	}
	b.WriteString(fmt.Sprintf("Installed: %d skills, %d subagents\n", nSkills, nSubagents))
	b.WriteString("Search: ")
	b.WriteString(m.search.View())
	b.WriteString("\n")
	b.WriteString("↑↓ move, space select, enter confirm, esc cancel\n")

	if len(m.visible) == 0 {
		b.WriteString("\n(no matching installed items)\n")
		return b.String()
	}

	b.WriteString("\n")

	h := defaultViewportHeight
	start := m.offset
	end := start + h
	if end > len(m.visible) {
		end = len(m.visible)
	}

	for i := start; i < end; i++ {
		idx := m.visible[i]
		it := m.items[idx]

		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		k := removeRowKey(it)
		box := glyphUnchecked
		if m.checked[k] {
			box = checkedStyle.Render(glyphChecked)
		}

		kindTag := string(it.Kind)
		var nameStyle lipgloss.Style
		switch it.Kind {
		case agent.InstalledSubagent:
			nameStyle = subagentNameStyle
		default:
			nameStyle = skillNameStyle
		}

		// "<agents (scope), ...>" summary, deterministic order (Locations
		// are already sorted by DiscoverInstalled).
		parts := make([]string, 0, len(it.Locations))
		for _, loc := range it.Locations {
			parts = append(parts, fmt.Sprintf("%s (%s)", loc.Agent, loc.Scope))
		}
		locs := strings.Join(parts, ", ")

		b.WriteString(fmt.Sprintf("%s%s%s %s  [%s] — %s\n",
			cursor, "", box, nameStyle.Render(it.Name), kindTag, locs))
	}

	remaining := len(m.visible) - end
	if remaining > 0 {
		b.WriteString(fmt.Sprintf("↓ %d more\n", remaining))
	}
	return b.String()
}

// Selection returns the InstalledItems the user kept checked. The order
// matches the input order, not the visible order, so the caller's
// downstream Remove call processes items deterministically.
func (m removeModel) Selection() agent.RemoveSelection {
	out := agent.RemoveSelection{}
	for _, it := range m.items {
		if m.checked[removeRowKey(it)] {
			out.Items = append(out.Items, it)
		}
	}
	return out
}

// RunRemove launches the bubbletea program on a fresh removeModel, blocks
// until the user quits, and returns the picked RemoveSelection. A cancel
// (esc/ctrl-c) returns an empty selection with no error so the caller can
// decide whether to abort silently or surface a message.
func RunRemove(items []agent.InstalledItem) (agent.RemoveSelection, error) {
	if len(items) == 0 {
		return agent.RemoveSelection{}, nil
	}
	m := NewRemoveModel(items)
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return agent.RemoveSelection{}, err
	}
	fm, ok := final.(removeModel)
	if !ok {
		return agent.RemoveSelection{}, fmt.Errorf("tui: unexpected final model type %T", final)
	}
	if fm.cancel {
		return agent.RemoveSelection{}, nil
	}
	return fm.Selection(), nil
}