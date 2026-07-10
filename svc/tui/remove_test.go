package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bizshuk/skills/svc/agent"
)

// sampleRemoveItems: two skills and one subagent, distributed across two
// agents. Mirrors what DiscoverInstalled returns on a machine with a few
// installs.
func sampleRemoveItems() []agent.InstalledItem {
	return []agent.InstalledItem{
		{
			Name: "writer",
			Kind: agent.InstalledSkill,
			Locations: []agent.InstalledLocation{
				{Agent: "claude-code", Scope: agent.ScopeProject, Path: "/p/.claude/skills/writer"},
			},
		},
		{
			Name: "helper",
			Kind: agent.InstalledSkill,
			Locations: []agent.InstalledLocation{
				{Agent: "antigravity", Scope: agent.ScopeProject, Path: "/p/.agents/skills/helper"},
				{Agent: "antigravity-cli", Scope: agent.ScopeProject, Path: "/p/.agents/skills/helper"},
			},
		},
		{
			Name: "reviewer",
			Kind: agent.InstalledSubagent,
			Locations: []agent.InstalledLocation{
				{Agent: "claude-code", Scope: agent.ScopeProject, Path: "/p/.claude/agents/reviewer.md"},
			},
		},
	}
}

func mustRemoveModel(t *testing.T, mi tea.Model) removeModel {
	t.Helper()
	m, ok := mi.(removeModel)
	require.True(t, ok, "Update must return a removeModel, got %T", mi)
	return m
}

// TestNewRemoveModel_NoneCheckedByDefault mirrors add's contract: every
// item starts unchecked; the user opts in with space.
func TestNewRemoveModel_NoneCheckedByDefault(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	sel := m.Selection()
	assert.Empty(t, sel.Items, "no items start checked")
}

// TestRemoveModel_SpaceTogglesRow: pressing Space on the cursor's row
// CHECKS it; pressing again UNCHECKS. The toggle is idempotent.
func TestRemoveModel_SpaceTogglesRow(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mustRemoveModel(t, updated)

	sel := m.Selection()
	require.Len(t, sel.Items, 1)
	assert.Equal(t, "writer", sel.Items[0].Name)

	// Toggle again — should clear.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mustRemoveModel(t, updated)
	assert.Empty(t, m.Selection().Items)
}

// TestRemoveModel_DownMovesCursorAndTogglesLaterRow: cursor moves to a
// later row, Space on that row toggles THAT row (not the first one).
// Guards against a bug where Down doesn't actually advance the cursor.
func TestRemoveModel_DownMovesCursorAndTogglesLaterRow(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mustRemoveModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mustRemoveModel(t, updated)

	sel := m.Selection()
	require.Len(t, sel.Items, 1)
	assert.Equal(t, "helper", sel.Items[0].Name, "after Down+Space, the SECOND item should be checked")
}

// TestRemoveModel_MultiSelect: user can check multiple items before Enter.
func TestRemoveModel_MultiSelect(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	for i := 0; i < 3; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
		m = mustRemoveModel(t, updated)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = mustRemoveModel(t, updated)
	}
	sel := m.Selection()
	require.Len(t, sel.Items, 3)
	got := []string{sel.Items[0].Name, sel.Items[1].Name, sel.Items[2].Name}
	assert.Equal(t, []string{"writer", "helper", "reviewer"}, got)
}

// TestRemoveModel_EnterCommits: pressing Enter sets done=true so RunRemove
// returns the current selection (not the initial empty one).
func TestRemoveModel_EnterCommits(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mustRemoveModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustRemoveModel(t, updated)
	assert.True(t, m.done)
	sel := m.Selection()
	require.Len(t, sel.Items, 1)
	assert.Equal(t, "writer", sel.Items[0].Name)
}

// TestRemoveModel_EscCancelsOnEmptySearch: with no search query, Esc
// sets cancel=true and RunRemove returns an empty selection.
func TestRemoveModel_EscCancelsOnEmptySearch(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mustRemoveModel(t, updated)
	assert.True(t, m.cancel)
	assert.Empty(t, m.Selection().Items)
}

// TestRemoveModel_EscClearsSearchFirst: with a non-empty search field,
// Esc clears the search instead of quitting, matching the add TUI's UX.
func TestRemoveModel_EscClearsSearchFirst(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	// Seed a search query by typing.
	for _, r := range "help" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mustRemoveModel(t, updated)
	}
	require.Equal(t, "help", m.search.Value())

	// First Esc: clears search, does NOT cancel.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mustRemoveModel(t, updated)
	assert.False(t, m.cancel, "first Esc should clear the search, not cancel")
	assert.Empty(t, m.search.Value())

	// Second Esc: cancels.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mustRemoveModel(t, updated)
	assert.True(t, m.cancel)
}

// TestRemoveModel_SearchFiltersRows: typing "help" leaves only the "helper"
// row visible. The cursor and toggle target follow the filtered list.
func TestRemoveModel_SearchFiltersRows(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	for _, r := range "help" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mustRemoveModel(t, updated)
	}
	require.Len(t, m.visible, 1)
	assert.Equal(t, "helper", m.items[m.visible[0]].Name)
}

// TestRemoveModel_ViewRendersAgents confirms the row layout carries the
// agent + scope summary so the user can see what each row will delete.
func TestRemoveModel_ViewRendersAgents(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	v := m.View()
	assert.Contains(t, v, "writer")
	assert.Contains(t, v, "helper")
	assert.Contains(t, v, "reviewer")
	assert.Contains(t, v, "claude-code", "agent name should appear in the location summary")
	assert.Contains(t, v, "antigravity")
	assert.Contains(t, v, "project")
}

// TestRemoveModel_ViewEmptyItems shows a friendly empty state when there
// is nothing installed (or nothing matches the search).
func TestRemoveModel_ViewEmptyItems(t *testing.T) {
	m := NewRemoveModel(nil)
	v := m.View()
	assert.Contains(t, v, "no matching installed items")

	// Same check after a search that excludes everything.
	m2 := NewRemoveModel(sampleRemoveItems())
	for _, r := range "zzznomatch" {
		updated, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m2 = mustRemoveModel(t, updated)
	}
	assert.Contains(t, m2.View(), "no matching installed items")
}

// TestRemoveModel_CursorClampsAfterFilter guards against an off-by-one
// when the filter shrinks the visible list past the cursor's old position.
func TestRemoveModel_CursorClampsAfterFilter(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	// Move cursor to last row.
	for i := 0; i < 3; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = mustRemoveModel(t, updated)
	}

	// Apply a filter that leaves only one row.
	for _, r := range "help" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mustRemoveModel(t, updated)
	}

	// Cursor should land on the new single row (index 0).
	require.Len(t, m.visible, 1)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mustRemoveModel(t, updated)
	require.Len(t, m.Selection().Items, 1)
	assert.Equal(t, "helper", m.Selection().Items[0].Name)
}

// TestRemoveModel_SearchIsCaseInsensitive protects against a regression
// where uppercase queries miss lower-case names.
func TestRemoveModel_SearchIsCaseInsensitive(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	for _, r := range strings.ToUpper("WRITER") {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mustRemoveModel(t, updated)
	}
	require.Len(t, m.visible, 1)
	assert.Equal(t, "writer", m.items[m.visible[0]].Name)
}

// TestRemoveModel_SearchByAgentName verifies that filtering on an agent
// type narrows down to items present in that agent.
func TestRemoveModel_SearchByAgentName(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	for _, r := range "antigravity" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mustRemoveModel(t, updated)
	}
	// helper is in antigravity; writer and reviewer are in claude-code only.
	require.Len(t, m.visible, 1)
	assert.Equal(t, "helper", m.items[m.visible[0]].Name)
}

// TestRunRemove_EmptyItemsReturnsEmpty verifies that an empty input skips
// the bubbletea program entirely (no point launching TUI for "nothing to
// remove").
func TestRunRemove_EmptyItemsReturnsEmpty(t *testing.T) {
	sel, err := RunRemove(nil)
	require.NoError(t, err)
	assert.Empty(t, sel.Items)

	sel, err = RunRemove([]agent.InstalledItem{})
	require.NoError(t, err)
	assert.Empty(t, sel.Items)
}