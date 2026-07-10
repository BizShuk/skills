package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bizshuk/skills/svc/agent"
)

// sampleRemoveItems: two project items and one global item, distributed
// across two agents. Mirrors what DiscoverInstalled returns on a machine
// with a few installs in each scope.
func sampleRemoveItems() []agent.InstalledItem {
	return []agent.InstalledItem{
		{
			Name:        "writer",
			Kind:        agent.InstalledSkill,
			Scope:       agent.ScopeProject,
			Description: "Writes things fluently.",
			Locations: []agent.InstalledLocation{
				{Agent: "claude-code", Path: "/p/.claude/skills/writer"},
			},
		},
		{
			Name:        "helper",
			Kind:        agent.InstalledSkill,
			Scope:       agent.ScopeProject,
			Description: "A friendly assistant.",
			Locations: []agent.InstalledLocation{
				{Agent: "antigravity", Path: "/p/.agents/skills/helper"},
				{Agent: "antigravity-cli", Path: "/p/.agents/skills/helper"},
			},
		},
		{
			Name:        "reviewer",
			Kind:        agent.InstalledSubagent,
			Scope:       agent.ScopeProject,
			Description: "Reviews PRs skeptically.",
			Locations: []agent.InstalledLocation{
				{Agent: "claude-code", Path: "/p/.claude/agents/reviewer.md"},
			},
		},
		{
			Name:        "global-helper",
			Kind:        agent.InstalledSkill,
			Scope:       agent.ScopeGlobal,
			Description: "Available everywhere.",
			Locations: []agent.InstalledLocation{
				{Agent: "claude-code", Path: "/home/.claude/skills/global-helper"},
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

// TestRemoveModel_RendersBothSections verifies that the visible rows
// include a header row for each section before the items in that section,
// in the project-first / global-second order the user asked for.
func TestRemoveModel_RendersBothSections(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())

	// visible[0] should be the Project header.
	require.Greater(t, len(m.visible), 0)
	assert.Equal(t, rowSectionHeader, m.visible[0].kind)
	assert.Equal(t, agent.ScopeProject, m.visible[0].section)

	// Last few rows should be in the Global section.
	last := m.visible[len(m.visible)-1]
	assert.Equal(t, agent.ScopeGlobal, last.section)

	// Find the Global header — must come AFTER all Project items.
	globalIdx := -1
	for i, r := range m.visible {
		if r.kind == rowSectionHeader && r.section == agent.ScopeGlobal {
			globalIdx = i
			break
		}
	}
	require.GreaterOrEqual(t, globalIdx, 0)

	// Every row before globalIdx must be Project scope.
	for i := 0; i < globalIdx; i++ {
		assert.Equal(t, agent.ScopeProject, m.visible[i].section,
			"row %d should be in Project section", i)
	}
}

// TestRemoveModel_SpaceTogglesRow: pressing Space on the cursor's row
// CHECKS it; pressing again UNCHECKS. The toggle is idempotent.
func TestRemoveModel_SpaceTogglesRow(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())

	// Skip the Project header (row 0) by pressing Down once so the
	// cursor lands on the first item row.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mustRemoveModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
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
// Guards against a bug where Down doesn't actually advance the cursor
// past section headers.
func TestRemoveModel_DownMovesCursorAndTogglesLaterRow(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())

	// Walk past the Project header + first item, land on the second
	// project item (helper).
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mustRemoveModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mustRemoveModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mustRemoveModel(t, updated)

	sel := m.Selection()
	require.Len(t, sel.Items, 1)
	assert.Equal(t, "helper", sel.Items[0].Name, "after Down+Down+Space, helper should be checked")
}

// TestRemoveModel_MultiSelect: user can check multiple items before Enter.
// The cursor starts on the Project header row, so the first Space toggles
// nothing (header is a no-op). We need to step past it before item toggles
// start counting.
func TestRemoveModel_MultiSelect(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	// Step past the Project header.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mustRemoveModel(t, updated)
	// Three rounds of (toggle, down) check writer, helper, reviewer.
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

// TestRemoveModel_SameNameInBothScopesIsTwoRows is the key sectioning
// guarantee: "shared" installed in both Project and Global appears as two
// rows with distinct keys. Toggling one does NOT toggle the other.
func TestRemoveModel_SameNameInBothScopesIsTwoRows(t *testing.T) {
	items := []agent.InstalledItem{
		{Name: "shared", Kind: agent.InstalledSkill, Scope: agent.ScopeProject,
			Locations: []agent.InstalledLocation{{Agent: "claude-code", Path: "/p/x"}}},
		{Name: "shared", Kind: agent.InstalledSkill, Scope: agent.ScopeGlobal,
			Locations: []agent.InstalledLocation{{Agent: "claude-code", Path: "/h/x"}}},
	}
	m := NewRemoveModel(items)
	require.GreaterOrEqual(t, len(m.visible), 4, "expect 2 headers + 2 items = 4")

	// Find the indices of the project item and global item.
	var projIdx, globIdx int = -1, -1
	for i, r := range m.visible {
		if r.kind != rowItem {
			continue
		}
		switch r.section {
		case agent.ScopeProject:
			projIdx = i
		case agent.ScopeGlobal:
			globIdx = i
		}
	}
	require.NotEqual(t, -1, projIdx)
	require.NotEqual(t, -1, globIdx)

	// Toggle only the project item.
	m.cursor = projIdx
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mustRemoveModel(t, updated)

	sel := m.Selection()
	require.Len(t, sel.Items, 1)
	assert.Equal(t, agent.ScopeProject, sel.Items[0].Scope, "only project row should be checked")

	// Now toggle the global item — both should be checked.
	m.cursor = globIdx
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mustRemoveModel(t, updated)

	sel = m.Selection()
	require.Len(t, sel.Items, 2)
	scopes := map[agent.Scope]bool{}
	for _, it := range sel.Items {
		scopes[it.Scope] = true
	}
	assert.True(t, scopes[agent.ScopeProject])
	assert.True(t, scopes[agent.ScopeGlobal])
}

// TestRemoveModel_SpaceOnHeaderIsNoop: pressing Space on a section header
// does not toggle anything (avoids the surprising "select all in section"
// semantics).
func TestRemoveModel_SpaceOnHeaderIsNoop(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	// Cursor starts on row 0 = Project header.
	require.Equal(t, rowSectionHeader, m.visible[0].kind)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mustRemoveModel(t, updated)
	assert.Empty(t, m.Selection().Items, "Space on section header must not toggle")
}

// TestRemoveModel_EnterCommits: pressing Enter sets done=true so RunRemove
// returns the current selection (not the initial empty one).
func TestRemoveModel_EnterCommits(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mustRemoveModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
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
	for _, r := range "help" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mustRemoveModel(t, updated)
	}
	require.Equal(t, "help", m.search.Value())

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mustRemoveModel(t, updated)
	assert.False(t, m.cancel, "first Esc should clear the search, not cancel")
	assert.Empty(t, m.search.Value())

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mustRemoveModel(t, updated)
	assert.True(t, m.cancel)
}

// TestRemoveModel_SearchFiltersRowsAcrossSections: a search that matches
// only the global item should collapse the view to just the Global
// section header + its matching item.
func TestRemoveModel_SearchFiltersRowsAcrossSections(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	for _, r := range "global" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mustRemoveModel(t, updated)
	}
	// visible: Global header + global-helper row = 2.
	require.Len(t, m.visible, 2)
	assert.Equal(t, rowSectionHeader, m.visible[0].kind)
	assert.Equal(t, agent.ScopeGlobal, m.visible[0].section)
	assert.Equal(t, "global-helper", m.visible[1].item.Name)
}

// TestRemoveModel_EmptySectionIsHidden: with only project items, no
// Global header should appear in the visible rows.
func TestRemoveModel_EmptySectionIsHidden(t *testing.T) {
	items := []agent.InstalledItem{
		{Name: "writer", Kind: agent.InstalledSkill, Scope: agent.ScopeProject,
			Locations: []agent.InstalledLocation{{Agent: "claude-code", Path: "/x"}}},
	}
	m := NewRemoveModel(items)
	for _, r := range m.visible {
		assert.NotEqual(t, agent.ScopeGlobal, r.section,
			"no Global section should render when there are no global items")
	}
}

// TestRemoveModel_ViewRendersHeadersAndAgents confirms the row layout
// carries the section headers and the agent summary.
func TestRemoveModel_ViewRendersHeadersAndAgents(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	v := m.View()
	assert.Contains(t, v, "Project")
	assert.Contains(t, v, "Global")
	assert.Contains(t, v, "writer")
	assert.Contains(t, v, "helper")
	assert.Contains(t, v, "reviewer")
	assert.Contains(t, v, "(claude-code)", "project writer row lists claude-code only")
	assert.Contains(t, v, "(antigravity, antigravity-cli)",
		"project helper row lists its two agents")
}

// TestRemoveModel_ViewRendersDescription asserts the per-row format from
// the design: name + dim description + (agents), with no [skill] tag and
// no em-dash separator. The description sits on the same line as the
// name so a single screen shows as many rows as possible.
func TestRemoveModel_ViewRendersDescription(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	v := m.View()

	assert.Contains(t, v, "Writes things fluently.",
		"description should appear inline after the skill name")
	assert.Contains(t, v, "Reviews PRs skeptically.",
		"subagent description should also appear inline")
	assert.NotContains(t, v, "[skill]",
		"the [skill]/[subagent] kind tag is dropped from the row")
	assert.NotContains(t, v, "[subagent]",
		"the [skill]/[subagent] kind tag is dropped from the row")
	assert.NotContains(t, v, "— claude-code",
		"the em-dash separator is dropped")
}

// TestRemoveModel_ViewEmptyItems shows a friendly empty state when there
// is nothing installed (or nothing matches the search).
func TestRemoveModel_ViewEmptyItems(t *testing.T) {
	m := NewRemoveModel(nil)
	v := m.View()
	assert.Contains(t, v, "no matching installed items")

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
	for i := 0; i < 5; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = mustRemoveModel(t, updated)
	}

	// Apply a filter that narrows to one row.
	for _, r := range "reviewer" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mustRemoveModel(t, updated)
	}
	require.Len(t, m.visible, 2, "Project header + reviewer row")

	// Cursor was 5; after the filter it must clamp to a real row. Walk
	// to the reviewer row explicitly before toggling.
	for i, r := range m.visible {
		if r.kind == rowItem {
			m.cursor = i
			break
		}
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mustRemoveModel(t, updated)
	require.Len(t, m.Selection().Items, 1)
	assert.Equal(t, "reviewer", m.Selection().Items[0].Name)
}

// TestRemoveModel_SearchIsCaseInsensitive protects against a regression
// where uppercase queries miss lower-case names.
func TestRemoveModel_SearchIsCaseInsensitive(t *testing.T) {
	m := NewRemoveModel(sampleRemoveItems())
	for _, r := range strings.ToUpper("WRITER") {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mustRemoveModel(t, updated)
	}
	// visible: Project header + writer row = 2.
	require.Len(t, m.visible, 2)
	assert.Equal(t, "writer", m.visible[1].item.Name)
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
	for _, r := range m.visible {
		if r.kind != rowItem {
			continue
		}
		assert.Equal(t, "helper", r.item.Name)
	}
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