package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/bizshuk/skills/svc/discover"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleCatalog mirrors the spec's reference fixture: one plugin with one
// skill (so the cursor lands on something) plus one plugin that failed to
// fetch (so the "unable to fetch" marker has somewhere to render).
func sampleCatalog() discover.Catalog {
	return discover.Catalog{
		{
			PluginName: "docs",
			FetchOK:    true,
			Skills:     []discover.Skill{{Name: "writer", Path: "/x/writer"}},
		},
		{
			PluginName: "remote",
			FetchOK:    false,
			FetchErr:   "unable to fetch",
		},
	}
}

// twoSkillCatalog is used by TestDownMovesCursor to prove the Down key
// actually advances the cursor: Space on row 1 must only uncheck the
// second skill.
func twoSkillCatalog() discover.Catalog {
	return discover.Catalog{
		{
			PluginName: "docs",
			FetchOK:    true,
			Skills: []discover.Skill{
				{Name: "writer", Path: "/x/writer"},
				{Name: "reader", Path: "/x/reader"},
			},
		},
	}
}

func TestNewModelAllChecked(t *testing.T) {
	m := NewModel(sampleCatalog())
	sel := m.Selection()
	require.Len(t, sel.SkillPaths, 1, "only the docs/writer skill is discoverable; remote plugin has no skills")
	assert.Equal(t, "/x/writer", sel.SkillPaths[0])
}

func TestSpaceTogglesRow(t *testing.T) {
	m := NewModel(sampleCatalog())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m2, ok := updated.(Model)
	require.True(t, ok, "Update must return a Model")
	assert.Empty(t, m2.Selection().SkillPaths, "toggling the only checked row leaves nothing selected")
}

func TestViewIncludesUnableToFetch(t *testing.T) {
	m := NewModel(sampleCatalog())
	view := m.View()
	assert.Contains(t, view, "unable to fetch", "the failed plugin should announce its fetch failure in the rendered tree")
}

func TestDownMovesCursor(t *testing.T) {
	m := NewModel(twoSkillCatalog())

	// Move cursor from row 0 to row 1.
	moved, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2 := moved.(Model)

	// Space now toggles row 1, leaving row 0 checked.
	toggled, _ := m2.Update(tea.KeyMsg{Type: tea.KeySpace})
	m3 := toggled.(Model)

	got := m3.Selection().SkillPaths
	require.Len(t, got, 1, "after toggling row 1, only row 0 should remain selected")
	assert.Equal(t, "/x/writer", got[0], "row 0 (writer) must still be checked; row 1 (reader) must be off")
}
