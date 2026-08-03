// update.go owns the state transitions of the skill picker: the bubbletea
// Init/Update pair plus the scroll bookkeeping the key handlers depend on.
// It mutates Model and never renders — rendering lives in view.go.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Init satisfies the bubbletea Model interface; we have no startup Cmd.
// The search field is already focused in NewModel so there is nothing
// for Init to kick off.
func (m Model) Init() tea.Cmd { return nil }

// Update dispatches key presses to cursor movement, toggle, fold/unfold,
// search filtering, and quit actions. Non-key messages are ignored. The
// returned Model carries the new cursor / checked / fold / search state;
// the returned Cmd is nil except on enter (tea.Quit) and ctrl-c (also
// tea.Quit) or esc-when-search-empty (also tea.Quit).
//
// Esc is overloaded: when the search field is non-empty it clears the
// search instead of quitting, so the user can iteratively narrow; with
// an empty field the second esc quits, matching the conventional
// pattern.
//
// In the agent phase, Up/Down navigate the agent list, Space toggles
// the agent under the cursor, and Enter confirms the selection.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.Type {
	case tea.KeyCtrlC:
		m.done = true
		return m, tea.Quit
	case tea.KeyEsc:
		switch m.phase {
		case phaseLevel:
			// Back to agent phase from level phase.
			m.phase = phaseAgents
			return m, nil
		case phaseAgents:
			// Back to skill phase from agent phase.
			m.phase = phaseSkills
			m.agentCursor = 0
			m.agentOffset = 0
			return m, nil
		}
		if m.search.Value() != "" {
			m.search.SetValue("")
			m.searchQuery = ""
			m.rebuildVisible()
			m.cursor = 0
			m.offset = 0
			m.ensureCursorVisible()
			return m, nil
		}
		m.done = true
		return m, tea.Quit
	case tea.KeyEnter:
		switch m.phase {
		case phaseSkills:
			// Advance to agent selection.
			m.phase = phaseAgents
			m.agentCursor = 0
			m.agentOffset = 0
			return m, nil
		case phaseAgents:
			// Advance to install-level selection. Pre-highlight whichever
			// row matches the current global flag (set by NewModel/Run
			// from the caller's --global default).
			m.phase = phaseLevel
			if m.global {
				m.levelCursor = 1
			} else {
				m.levelCursor = 0
			}
			return m, nil
		default: // phaseLevel
			m.done = true
			return m, tea.Quit
		}
	}

	// Level phase: Up/Down move the highlight between Project and Global;
	// Space commits the highlighted row as the chosen install level.
	if m.phase == phaseLevel {
		switch key.Type {
		case tea.KeyUp:
			if m.levelCursor > 0 {
				m.levelCursor--
			}
		case tea.KeyDown:
			if m.levelCursor < 1 {
				m.levelCursor++
			}
		case tea.KeySpace:
			m.global = m.levelCursor == 1
		}
		return m, nil
	}

	// Agent phase: Up/Down move the cursor, Space toggles the agent
	// under the cursor.
	if m.phase == phaseAgents {
		switch key.Type {
		case tea.KeyUp:
			if m.agentCursor > 0 {
				m.agentCursor--
				m.ensureAgentCursorVisible()
			}
		case tea.KeyDown:
			if m.agentCursor < len(m.agents)-1 {
				m.agentCursor++
				m.ensureAgentCursorVisible()
			}
		case tea.KeySpace:
			if m.agentCursor >= 0 && m.agentCursor < len(m.agents) {
				m.agents[m.agentCursor].checked = !m.agents[m.agentCursor].checked
			}
		}
		return m, nil
	}

	// Navigation keys are NOT fed to the search input (otherwise Up/Down
	// would move within the text rather than navigate the tree, and Space
	// would insert a space rather than toggle). Everything else
	// (printable runes, Backspace, Delete, Arrow keys for cursor within
	// the field, etc) goes to the input first.
	switch key.Type {
	case tea.KeyUp, tea.KeyDown, tea.KeySpace, tea.KeyLeft, tea.KeyRight:
		// fall through to navigation handler below
	default:
		prev := m.search.Value()
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		if m.search.Value() != prev {
			m.searchQuery = m.search.Value()
			m.rebuildVisible()
			m.cursor = 0
			m.offset = 0
			m.ensureCursorVisible()
			return m, cmd
		}
		return m, nil
	}

	if len(m.rows) == 0 {
		// Navigation key on an empty list: no-op (still allow Up/Down to
		// keep the cursor pinned at 0).
		switch key.Type {
		case tea.KeyUp, tea.KeyDown:
			// no-op
		}
		return m, nil
	}

	switch key.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			m.ensureCursorVisible()
		}
	case tea.KeyDown:
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			m.ensureCursorVisible()
		}
	case tea.KeySpace:
		r := m.rows[m.cursor]
		if r.isHeader {
			m.toggleSubtree(r.node)
		} else if r.skill != nil {
			m.checked[r.skill.Path] = !m.checked[r.skill.Path]
		} else if r.subagent != nil {
			m.checkedSubagent[r.subagent.Path] = !m.checkedSubagent[r.subagent.Path]
		}
	case tea.KeyRight:
		r := m.rows[m.cursor]
		if r.isHeader {
			// Toggle fold on any header that has any descendants (children, skills, or
			// subagents). The cascade helpers handle all three uniformly.
			if len(r.node.Children) > 0 || len(r.node.Skills) > 0 || len(r.node.Subagents) > 0 {
				if m.folded[r.node] {
					unfoldSubtree(r.node, &m.folded)
				} else {
					foldSubtree(r.node, &m.folded)
				}
				m.rebuildVisible()
				m.ensureCursorVisible()
			}
		} else if r.skill != nil {
			if len([]rune(r.skill.Description)) > 60 {
				m.skillUnfolded[r.skill.Path] = true
			} else {
				m.cursor = m.findParentHeader(m.cursor)
				m.ensureCursorVisible()
			}
		}
	case tea.KeyLeft:
		r := m.rows[m.cursor]
		if r.isHeader {
			if !m.folded[r.node] {
				foldSubtree(r.node, &m.folded)
				m.rebuildVisible()
				m.ensureCursorVisible()
			}
		} else if r.skill != nil {
			if m.skillUnfolded[r.skill.Path] {
				delete(m.skillUnfolded, r.skill.Path)
			} else {
				m.cursor = m.findParentHeader(m.cursor)
				m.ensureCursorVisible()
			}
		}
	}
	return m, nil
}

// ensureAgentCursorVisible keeps the agent cursor within the visible window.
func (m *Model) ensureAgentCursorVisible() {
	h := m.viewportHeight
	if h <= 0 {
		h = defaultViewportHeight
	}
	if m.agentCursor < m.agentOffset {
		m.agentOffset = m.agentCursor
	}
	if m.agentCursor >= m.agentOffset+h {
		m.agentOffset = m.agentCursor - h + 1
	}
	maxOffset := len(m.agents) - h
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.agentOffset > maxOffset {
		m.agentOffset = maxOffset
	}
	if m.agentOffset < 0 {
		m.agentOffset = 0
	}
}
