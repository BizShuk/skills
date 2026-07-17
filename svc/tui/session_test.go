package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bizshuk/skills/model"
)

func sampleSessions() []model.AgentSession {
	return []model.AgentSession{
		{Agent: "claude-code", ID: "session-1", Path: "/tmp/session-1.jsonl"},
		{Agent: "codex", ID: "session-2", Path: "/tmp/session-2.jsonl"},
	}
}

func mustSessionModel(t *testing.T, value tea.Model) SessionModel {
	t.Helper()
	m, ok := value.(SessionModel)
	require.True(t, ok, "Update must return SessionModel, got %T", value)
	return m
}

func TestSessionModelStartsOnListAndRendersRows(t *testing.T) {
	m := NewSessionModel(sampleSessions(), nil)

	assert.Contains(t, m.View(), "Session list")
	assert.Contains(t, m.View(), "claude-code")
	assert.Contains(t, m.View(), "session-1")
	assert.Equal(t, 0, m.cursor)
}

func TestNewSessionModelGroupsSessionsByAgentInFirstSeenOrder(t *testing.T) {
	items := []model.AgentSession{
		{Agent: "codex", ID: "codex-1"},
		{Agent: "claude-code", ID: "claude-1"},
		{Agent: "codex", ID: "codex-2"},
	}

	m := NewSessionModel(items, nil)

	assert.Equal(t, []string{"codex", "claude-code"}, m.agentNames())
	assert.Equal(t, 0, m.activeAgent)
	assert.Equal(t, []string{"codex-1", "codex-2"}, sessionIDs(m.items))
	assert.Equal(t, map[string]int{"codex": 0, "claude-code": 0}, m.cursorByAgent)
	assert.Equal(t, map[string]int{"codex": 0, "claude-code": 0}, m.offsetByAgent)
}

func TestNewSessionModelWithNoSessionsHasNoAgentTabs(t *testing.T) {
	m := NewSessionModel(nil, nil)

	assert.Empty(t, m.agentNames())
	assert.Empty(t, m.items)
	assert.Equal(t, -1, m.activeAgent)
}

func sessionIDs(items []model.AgentSession) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestSessionModelRightArrowLoadsDetail(t *testing.T) {
	want := model.AgentSessionDetail{
		Session: sampleSessions()[0],
		Title:   "Inspect parser",
		Events: []model.SessionEvent{{
			Role:    "user",
			Kind:    "message",
			Summary: "hello",
		}},
	}
	loader := func(item model.AgentSession) (model.AgentSessionDetail, error) {
		assert.Equal(t, "session-1", item.ID)
		return want, nil
	}

	m := NewSessionModel(sampleSessions(), loader)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = mustSessionModel(t, updated)
	require.NotNil(t, cmd)
	assert.Contains(t, m.View(), "Loading")

	loaded, ok := cmd().(detailLoadedMsg)
	require.True(t, ok)
	updated, _ = m.Update(loaded)
	m = mustSessionModel(t, updated)
	assert.Contains(t, m.View(), "Inspect parser")
	assert.Contains(t, m.View(), "hello")
}

func TestSessionModelMouseClickLoadsClickedRow(t *testing.T) {
	var selected string
	loader := func(item model.AgentSession) (model.AgentSessionDetail, error) {
		selected = item.ID
		return model.AgentSessionDetail{Session: item}, nil
	}

	m := NewSessionModel(sampleSessions(), loader)
	updated, cmd := m.Update(tea.MouseMsg{
		X:      2,
		Y:      sessionListFirstRow + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	m = mustSessionModel(t, updated)
	require.NotNil(t, cmd)
	loaded, ok := cmd().(detailLoadedMsg)
	require.True(t, ok)
	assert.Equal(t, "session-2", selected)
	assert.Equal(t, "session-2", loaded.detail.Session.ID)
}

func TestSessionModelEscAndLeftReturnToList(t *testing.T) {
	detail := model.AgentSessionDetail{
		Session: sampleSessions()[0],
		Title:   "Inspect parser",
		Events:  []model.SessionEvent{{Summary: "hello"}},
	}
	loader := func(item model.AgentSession) (model.AgentSessionDetail, error) {
		return detail, nil
	}

	for _, key := range []tea.KeyType{tea.KeyEsc, tea.KeyLeft} {
		m := NewSessionModel(sampleSessions(), loader)
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = mustSessionModel(t, updated)
		updated, _ = m.Update(cmd().(detailLoadedMsg))
		m = mustSessionModel(t, updated)
		require.Equal(t, sessionDetailPhase, m.phase)

		updated, _ = m.Update(tea.KeyMsg{Type: key})
		m = mustSessionModel(t, updated)
		assert.Equal(t, sessionListPhase, m.phase)
		assert.Equal(t, 0, m.cursor)
		assert.Empty(t, m.detail.Events)
		assert.Empty(t, m.detailErr)
	}
}

func TestSessionModelDetailScrollStaysWithinBounds(t *testing.T) {
	events := make([]model.SessionEvent, 5)
	for i := range events {
		events[i] = model.SessionEvent{Role: "assistant", Kind: "message", Summary: "event"}
	}
	m := NewSessionModel(sampleSessions(), nil)
	m.phase = sessionDetailPhase
	m.detail = model.AgentSessionDetail{Session: sampleSessions()[0], Events: events}
	m.viewportHeight = 2

	for i := 0; i < len(events)+3; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = mustSessionModel(t, updated)
	}
	assert.Equal(t, len(events)-m.detailViewportHeight(), m.detailOffset)

	for i := 0; i < len(events)+3; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = mustSessionModel(t, updated)
	}
	assert.Equal(t, 0, m.detailOffset)
}

func TestRunSessionReturnsImmediatelyForEmptyItems(t *testing.T) {
	require.NoError(t, RunSession(nil))
}
