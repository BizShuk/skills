package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/skills/svc/agent"
)

// agentSelectionModel is the focused one-phase picker used by
// `skills install`. It shares the add flow's agentRow rendering contract
// without carrying skill or install-level state.
type agentSelectionModel struct {
	rows   []agentRow
	cursor int
	offset int

	done   bool
	cancel bool
}

func newAgentSelectionModel(agents []agent.Agent) agentSelectionModel {
	return agentSelectionModel{rows: makeRuleAgents(agents)}
}

func makeRuleAgents(agents []agent.Agent) []agentRow {
	type groupKey struct {
		path     string
		detected bool
	}

	groups := make(map[groupKey][]agent.Agent)
	order := make([]groupKey, 0, len(agents))
	for _, configured := range agents {
		key := groupKey{
			path:     configured.GlobalRulePath,
			detected: isRuleAgentDetected(configured),
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], configured)
	}

	rows := make([]agentRow, 0, len(order))
	for _, key := range order {
		rows = append(rows, agentRow{
			agents:   groups[key],
			detected: key.detected,
			checked:  key.detected,
		})
	}
	return rows
}

func isRuleAgentDetected(configured agent.Agent) bool {
	if configured.DetectDir == "" {
		return false
	}
	info, err := os.Stat(configured.DetectDir)
	return err == nil && info.IsDir()
}

// Init satisfies tea.Model.
func (m agentSelectionModel) Init() tea.Cmd { return nil }

// Update handles the add-style agent navigation and selection keys.
func (m agentSelectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
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
	case tea.KeyDown:
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			m.ensureCursorVisible()
		}
	case tea.KeySpace:
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			m.rows[m.cursor].checked = !m.rows[m.cursor].checked
		}
	}
	return m, nil
}

func (m *agentSelectionModel) ensureCursorVisible() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+defaultViewportHeight {
		m.offset = m.cursor - defaultViewportHeight + 1
	}
	maxOffset := len(m.rows) - defaultViewportHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
}

// View renders the global-rule agent picker.
func (m agentSelectionModel) View() string {
	var b strings.Builder
	b.WriteString("Select agents to install the global rule into\n")
	b.WriteString("↑↓ move, space select, enter confirm, esc cancel\n\n")

	if len(m.rows) == 0 {
		b.WriteString("(no agents available)\n")
		return b.String()
	}

	start := m.offset
	end := min(start+defaultViewportHeight, len(m.rows))
	for i := start; i < end; i++ {
		row := m.rows[i]
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		box := glyphUnchecked
		if row.checked {
			box = checkedStyle.Render(glyphChecked)
		}
		label := row.displayName()
		if row.detected {
			label += "  (detected)"
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, box, pluginHeaderStyle.Render(label)))
	}

	if remaining := len(m.rows) - end; remaining > 0 {
		b.WriteString(fmt.Sprintf("↓ %d more\n", remaining))
	}
	return b.String()
}

// Selection returns every agent type contained in checked rows.
func (m agentSelectionModel) Selection() []agent.AgentType {
	if m.cancel {
		return nil
	}

	var selected []agent.AgentType
	for _, row := range m.rows {
		if !row.checked {
			continue
		}
		for _, configured := range row.agents {
			selected = append(selected, configured.Type)
		}
	}
	return selected
}

// RunAgentSelection opens the interactive global-rule agent picker.
func RunAgentSelection(agents []agent.Agent) ([]agent.AgentType, error) {
	if len(agents) == 0 {
		return nil, nil
	}

	program := tea.NewProgram(newAgentSelectionModel(agents))
	final, err := program.Run()
	if err != nil {
		return nil, err
	}
	model, ok := final.(agentSelectionModel)
	if !ok {
		return nil, fmt.Errorf("tui: unexpected final model type %T", final)
	}
	return model.Selection(), nil
}
