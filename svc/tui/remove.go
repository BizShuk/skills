// Package tui also renders the `skills remove` flow. The remove UI is a
// flat list split into two sections — Project first, then Global — so the
// user can tell at a glance what's installed at each scope. One row per
// (Name, Kind, Scope) triple: a skill installed in both project and
// global shows as two separate rows, one per section, with independent
// checkboxes.
//
// Reuse: viewport math (defaultViewportHeight), the lipgloss style palette
// (pluginHeaderStyle, checkedStyle, subagentNameStyle), and the textinput
// search field mirror what tui.go does for `add`. We intentionally don't
// share the bubbletea Model type with tui.go — `add` is a tree with three
// phases, `remove` is a sectioned list with one phase — merging them would
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

// removeRowKind discriminates a visible row: a section header vs. an item.
type removeRowKind int

const (
	rowSectionHeader removeRowKind = iota
	rowItem
)

// removeRow is one line in the rendered remove view. Section headers
// carry their scope so View() can render the right label; items carry
// a pointer into removeModel.items.
type removeRow struct {
	kind    removeRowKind
	item    *agent.InstalledItem // non-nil for rowItem
	section agent.Scope          // valid for both header and item rows
}

// removeSectionLayout describes the ordering and visibility of the two
// sections in the rendered list. "Active" sections contribute a header
// row + their filtered items; sections with zero items (or zero matches)
// collapse entirely.
var removeSectionLayout = []agent.Scope{agent.ScopeProject, agent.ScopeGlobal}

// removeModel is the bubbletea model for the remove flow.
type removeModel struct {
	items   []agent.InstalledItem
	visible []removeRow
	cursor  int
	offset  int

	search      textinput.Model
	searchQuery string

	checked map[string]bool // key = scope|kind|name
	done    bool
	cancel  bool
}

// removeRowKey is the dedupe key for an InstalledItem across filter passes.
func removeRowKey(it agent.InstalledItem) string {
	return string(it.Scope) + "|" + string(it.Kind) + "|" + it.Name
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
// the section layout. Items match the query when their Name OR any of
// their agent names OR their kind substring contains the lower-cased
// query. Sections with zero matching items are omitted entirely so the
// user doesn't see an empty "Global" header on a project-only install.
func (m *removeModel) rebuildVisible() {
	q := strings.ToLower(strings.TrimSpace(m.searchQuery))

	// Bucket items by section (preserving source order within each).
	bySection := make(map[agent.Scope][]agent.InstalledItem, len(removeSectionLayout))
	for _, it := range m.items {
		bySection[it.Scope] = append(bySection[it.Scope], it)
	}

	var out []removeRow
	for _, scope := range removeSectionLayout {
		items := bySection[scope]
		var matched []agent.InstalledItem
		for i := range items {
			if q == "" || itemMatchesQuery(&items[i], q) {
				matched = append(matched, items[i])
			}
		}
		if len(matched) == 0 {
			continue
		}
		out = append(out, removeRow{kind: rowSectionHeader, section: scope})
		for i := range matched {
			it := matched[i]
			out = append(out, removeRow{kind: rowItem, item: &it, section: scope})
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

// itemMatchesQuery is the per-item search predicate used by rebuildVisible.
func itemMatchesQuery(it *agent.InstalledItem, q string) bool {
	if strings.Contains(strings.ToLower(it.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(string(it.Kind)), q) {
		return true
	}
	for _, loc := range it.Locations {
		if strings.Contains(strings.ToLower(string(loc.Agent)), q) {
			return true
		}
	}
	return false
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
		r := m.visible[m.cursor]
		if r.kind != rowItem || r.item == nil {
			// Space on a section header is a no-op — toggling "all in
			// section" is a tempting feature but adds non-obvious
			// semantics (subagent + skill mix). Keep it simple.
			return m, nil
		}
		k := removeRowKey(*r.item)
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

// View renders the sectioned list. Layout:
//
//	Select installed skills/subagents to remove
//	Installed: N skills, M subagents
//	Search: <input>
//	↑↓ move, space select, enter confirm, esc cancel
//
//	▼ Project (cwd-relative)
//	  ○  helper  [skill] — claude-code
//	  ●  reviewer  [subagent] — claude-code
//
//	▼ Global (under $HOME)
//	  ○  helper  [skill] — claude-code
//
//	↓ N more (when off-screen)
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
		r := m.visible[i]

		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		if r.kind == rowSectionHeader {
			label := sectionLabel(r.section)
			b.WriteString(fmt.Sprintf("%s%s%s\n", cursor, "", pluginHeaderStyle.Render("▼ "+label)))
			continue
		}

		k := removeRowKey(*r.item)
		box := glyphUnchecked
		if m.checked[k] {
			box = checkedStyle.Render(glyphChecked)
		}

		var nameStyle lipgloss.Style
		switch r.item.Kind {
		case agent.InstalledSubagent:
			nameStyle = subagentNameStyle
		default:
			nameStyle = skillNameStyle
		}

		// Row format: cursor + box + name + dim description + (agents).
		// The kind tag and em-dash are dropped per UX simplification —
		// description gives the row context, agents in parens tell the
		// user exactly which installs the toggle will reach.
		var desc string
		if r.item.Description != "" {
			desc = "  " + skillDescStyle.Render(truncateRune(r.item.Description, 60))
		}

		agents := make([]string, 0, len(r.item.Locations))
		for _, loc := range r.item.Locations {
			agents = append(agents, string(loc.Agent))
		}
		b.WriteString(fmt.Sprintf("%s  %s %s%s  (%s)\n",
			cursor, box, nameStyle.Render(r.item.Name), desc, strings.Join(agents, ", ")))
	}

	remaining := len(m.visible) - end
	if remaining > 0 {
		b.WriteString(fmt.Sprintf("\n↓ %d more\n", remaining))
	}
	return b.String()
}

// sectionLabel maps a Scope to its header label. Kept here (rather than
// next to the agent.Scope definition) so the wording stays close to the
// place that renders it — and so an i18n pass later only touches this
// package.
func sectionLabel(s agent.Scope) string {
	switch s {
	case agent.ScopeProject:
		return "Project (cwd-relative)"
	case agent.ScopeGlobal:
		return "Global (under $HOME)"
	default:
		return string(s)
	}
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