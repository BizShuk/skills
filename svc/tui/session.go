package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/skills/model"
	"github.com/bizshuk/skills/svc/session"
)

const (
	sessionListFirstRow    = 4
	defaultSessionViewport = 20
)

type sessionPhase int

const (
	sessionListPhase sessionPhase = iota
	sessionLoadingPhase
	sessionDetailPhase
)

// SessionDetailLoader loads the full timeline for one selected session.
type SessionDetailLoader func(model.AgentSession) (model.AgentSessionDetail, error)

type sessionTab struct {
	agent string
	items []model.AgentSession
}

// SessionModel is the two-screen Bubble Tea model for agent sessions.
// The list is metadata-only until a session is opened; detail loading is
// performed by the injected SessionDetailLoader command.
type SessionModel struct {
	items []model.AgentSession
	tabs  []sessionTab

	loader      SessionDetailLoader
	activeAgent int

	cursor        int
	offset        int
	cursorByAgent map[string]int
	offsetByAgent map[string]int

	phase sessionPhase

	detail       model.AgentSessionDetail
	detailErr    error
	detailOffset int

	viewportHeight int
	width          int
	height         int
}

type detailLoadedMsg struct {
	detail model.AgentSessionDetail
	err    error
}

// NewSessionModel creates a session list model with the selected row at zero.
func NewSessionModel(items []model.AgentSession, loader SessionDetailLoader) SessionModel {
	tabs := buildSessionTabs(items)
	m := SessionModel{
		tabs:           tabs,
		loader:         loader,
		activeAgent:    -1,
		phase:          sessionListPhase,
		viewportHeight: defaultSessionViewport,
		cursorByAgent:  make(map[string]int, len(tabs)),
		offsetByAgent:  make(map[string]int, len(tabs)),
	}
	for _, tab := range tabs {
		m.cursorByAgent[tab.agent] = 0
		m.offsetByAgent[tab.agent] = 0
	}
	if len(tabs) > 0 {
		m.activeAgent = 0
		m.items = append([]model.AgentSession(nil), tabs[0].items...)
	}
	return m
}

func buildSessionTabs(items []model.AgentSession) []sessionTab {
	tabs := make([]sessionTab, 0)
	indexes := make(map[string]int)
	for _, item := range items {
		index, ok := indexes[item.Agent]
		if !ok {
			index = len(tabs)
			indexes[item.Agent] = index
			tabs = append(tabs, sessionTab{agent: item.Agent})
		}
		tabs[index].items = append(tabs[index].items, item)
	}
	return tabs
}

func (m SessionModel) agentNames() []string {
	names := make([]string, 0, len(m.tabs))
	for _, tab := range m.tabs {
		names = append(names, tab.agent)
	}
	return names
}

func (m SessionModel) activeAgentName() string {
	if m.activeAgent < 0 || m.activeAgent >= len(m.tabs) {
		return ""
	}
	return m.tabs[m.activeAgent].agent
}

func (m SessionModel) activeItems() []model.AgentSession {
	if m.activeAgent < 0 || m.activeAgent >= len(m.tabs) {
		return nil
	}
	return m.tabs[m.activeAgent].items
}

// Init satisfies tea.Model. Session detail loading starts only after a user
// opens a row, so the initial list needs no command.
func (m SessionModel) Init() tea.Cmd {
	return nil
}

// Update handles list navigation, lazy detail loading, mouse selection, and
// detail back-navigation/scrolling.
func (m SessionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch message := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		available := message.Height - sessionListFirstRow - 2
		if available < 1 {
			available = 1
		}
		m.viewportHeight = available
		m.ensureListCursorVisible()
		m.ensureDetailOffset()
		return m, nil
	case detailLoadedMsg:
		return m.applyDetail(message), nil
	case tea.KeyMsg:
		return m.updateKey(message)
	case tea.MouseMsg:
		return m.updateMouse(message)
	default:
		return m, nil
	}
}

func (m SessionModel) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}

	switch m.phase {
	case sessionListPhase:
		switch key.Type {
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
				m.ensureListCursorVisible()
			}
		case tea.KeyDown:
			if m.cursor < len(m.items)-1 {
				m.cursor++
				m.ensureListCursorVisible()
			}
		case tea.KeyRight, tea.KeyEnter:
			return m.openSelected()
		case tea.KeyEsc:
			return m, tea.Quit
		}
	case sessionLoadingPhase:
		if key.Type == tea.KeyEsc || key.Type == tea.KeyLeft {
			m.returnToList()
		}
	case sessionDetailPhase:
		switch key.Type {
		case tea.KeyEsc, tea.KeyLeft:
			m.returnToList()
		case tea.KeyUp:
			if m.detailOffset > 0 {
				m.detailOffset--
			}
		case tea.KeyDown:
			if m.detailOffset < m.maxDetailOffset() {
				m.detailOffset++
			}
		case tea.KeyPgUp:
			m.detailOffset -= m.detailViewportHeight()
			m.ensureDetailOffset()
		case tea.KeyPgDown:
			m.detailOffset += m.detailViewportHeight()
			m.ensureDetailOffset()
		}
	}
	return m, nil
}

func (m SessionModel) updateMouse(message tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.phase != sessionListPhase || message.Action != tea.MouseActionPress || message.Button != tea.MouseButtonLeft {
		return m, nil
	}

	row := m.offset + message.Y - sessionListFirstRow
	if row < m.offset || row >= len(m.items) || row >= m.offset+m.listViewportHeight() {
		return m, nil
	}
	m.cursor = row
	return m.openSelected()
}

func (m SessionModel) openSelected() (tea.Model, tea.Cmd) {
	if len(m.items) == 0 || m.cursor < 0 || m.cursor >= len(m.items) {
		return m, nil
	}

	item := m.items[m.cursor]
	loader := m.loader
	m.phase = sessionLoadingPhase
	m.detailErr = nil
	m.detailOffset = 0
	return m, func() tea.Msg {
		if loader == nil {
			return detailLoadedMsg{err: fmt.Errorf("session detail loader is not configured")}
		}
		detail, err := loader(item)
		return detailLoadedMsg{detail: detail, err: err}
	}
}

func (m SessionModel) applyDetail(message detailLoadedMsg) SessionModel {
	m.phase = sessionDetailPhase
	m.detail = message.detail
	if m.detail.Session.ID == "" && m.cursor >= 0 && m.cursor < len(m.items) {
		m.detail.Session = m.items[m.cursor]
	}
	m.detailErr = message.err
	m.detailOffset = 0
	m.ensureDetailOffset()
	return m
}

func (m *SessionModel) returnToList() {
	m.phase = sessionListPhase
	m.detail = model.AgentSessionDetail{}
	m.detailErr = nil
	m.detailOffset = 0
	m.ensureListCursorVisible()
}

func (m SessionModel) listViewportHeight() int {
	if m.viewportHeight > 0 {
		return m.viewportHeight
	}
	return defaultSessionViewport
}

func (m SessionModel) detailViewportHeight() int {
	height := m.listViewportHeight()
	if height < 1 {
		return 1
	}
	return height
}

func (m SessionModel) maxDetailOffset() int {
	max := len(m.detail.Events) - m.detailViewportHeight()
	if max < 0 {
		return 0
	}
	return max
}

func (m *SessionModel) ensureListCursorVisible() {
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	h := m.listViewportHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	max := len(m.items) - h
	if max < 0 {
		max = 0
	}
	if m.offset > max {
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *SessionModel) ensureDetailOffset() {
	max := m.maxDetailOffset()
	if m.detailOffset < 0 {
		m.detailOffset = 0
	}
	if m.detailOffset > max {
		m.detailOffset = max
	}
}

// View renders the current session list, loading state, or normalized detail.
func (m SessionModel) View() string {
	switch m.phase {
	case sessionLoadingPhase:
		return m.viewLoading()
	case sessionDetailPhase:
		return m.viewDetail()
	default:
		return m.viewList()
	}
}

func (m SessionModel) viewList() string {
	var b strings.Builder
	b.WriteString("Session list\n")
	b.WriteString(fmt.Sprintf("Sessions: %d\n", len(m.items)))
	b.WriteString("↑↓ move, →/enter/click open, esc quit\n\n")

	if len(m.items) == 0 {
		b.WriteString("(no sessions)\n")
		return b.String()
	}

	start := m.offset
	end := start + m.listViewportHeight()
	if end > len(m.items) {
		end = len(m.items)
	}
	for index := start; index < end; index++ {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		b.WriteString(cursor)
		b.WriteString(formatSessionRow(m.items[index]))
		b.WriteByte('\n')
	}
	if end < len(m.items) {
		b.WriteString(fmt.Sprintf("↓ %d more\n", len(m.items)-end))
	}
	return b.String()
}

func (m SessionModel) viewLoading() string {
	item := m.selectedItem()
	return fmt.Sprintf("Session detail\nLoading %s/%s...\n\n←/esc back\n", item.Agent, item.ID)
}

func (m SessionModel) viewDetail() string {
	var b strings.Builder
	detail := m.detail
	b.WriteString("Session detail\n")
	b.WriteString(fmt.Sprintf("Agent: %s\n", displayOrDash(detail.Session.Agent)))
	b.WriteString(fmt.Sprintf("ID: %s\n", displayOrDash(detail.Session.ID)))
	b.WriteString(fmt.Sprintf("Title: %s\n", displayOrDash(detail.Title)))
	b.WriteString(fmt.Sprintf("CWD: %s\n", displayOrDash(detail.CWD)))
	b.WriteString(fmt.Sprintf("Source: %s\n", displayOrDash(detail.Session.Path)))
	b.WriteString("↑↓/pgup/pgdown scroll, ←/esc back\n\n")

	if m.detailErr != nil {
		b.WriteString("Error: ")
		b.WriteString(sanitizeLine(m.detailErr.Error()))
		b.WriteByte('\n')
		return b.String()
	}
	if len(detail.Events) == 0 {
		b.WriteString("(no displayable events)\n")
		return b.String()
	}

	start := m.detailOffset
	end := start + m.detailViewportHeight()
	if end > len(detail.Events) {
		end = len(detail.Events)
	}
	for _, event := range detail.Events[start:end] {
		b.WriteString(formatSessionEvent(event, m.width))
		b.WriteByte('\n')
	}
	if end < len(detail.Events) {
		b.WriteString(fmt.Sprintf("↓ %d more events\n", len(detail.Events)-end))
	}
	return b.String()
}

func (m SessionModel) selectedItem() model.AgentSession {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return m.items[m.cursor]
	}
	return model.AgentSession{}
}

func formatSessionRow(item model.AgentSession) string {
	lastActivity := "-"
	if !item.LastActivity.IsZero() {
		lastActivity = item.LastActivity.Local().Format("2006-01-02 15:04:05")
	}
	return truncateRune(fmt.Sprintf("%s  %s  %s", item.Agent, item.ID, lastActivity), 120)
}

func formatSessionEvent(event model.SessionEvent, width int) string {
	timestamp := "-"
	if !event.Timestamp.IsZero() {
		timestamp = event.Timestamp.Local().Format("15:04:05")
	}
	labelParts := make([]string, 0, 2)
	if event.Role != "" {
		labelParts = append(labelParts, event.Role)
	}
	if event.Kind != "" {
		labelParts = append(labelParts, event.Kind)
	}
	label := strings.Join(labelParts, "/")
	if label == "" {
		label = "event"
	}
	summary := event.Summary
	if summary == "" {
		summary = event.Raw
	}
	summary = sanitizeLine(summary)
	if summary == "" {
		summary = "-"
	}
	line := fmt.Sprintf("%s  %-18s %s", timestamp, label, summary)
	maxWidth := width
	if maxWidth <= 0 {
		maxWidth = 120
	}
	return truncateRune(line, maxWidth)
}

func displayOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return sanitizeLine(value)
}

func sanitizeLine(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\x1b", "")), " ")
}

// RunSession launches the interactive session browser with mouse support.
func RunSession(items []model.AgentSession) error {
	if len(items) == 0 {
		return nil
	}
	m := NewSessionModel(items, session.LoadDetail)
	p := tea.NewProgram(m, tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
