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
// fetch (so the "unable to fetch" marker has somewhere to render). In
// the tree shape, "docs" is at the root, "remote" is a sibling root
// (failed fetch) — they don't nest because both are reached from the
// walk's root, not from each other.
func sampleCatalog() *discover.Catalog {
	return &discover.Catalog{
		Roots: []*discover.Category{
			{
				PluginName: "docs",
				FetchOK:    true,
				Skills: []discover.Skill{
					{Name: "writer", Path: "/x/writer"},
				},
			},
			{
				PluginName: "remote",
				FetchOK:    false,
				FetchErr:   "unable to fetch",
			},
		},
	}
}

// twoSkillCatalog is used by TestDownMovesCursor to prove the Down key
// actually advances the cursor past the header: Space on a header
// toggles both skills, but Space on the second skill row toggles only
// that skill.
func twoSkillCatalog() *discover.Catalog {
	return &discover.Catalog{
		Roots: []*discover.Category{
			{
				PluginName: "docs",
				FetchOK:    true,
				Skills: []discover.Skill{
					{Name: "writer", Path: "/x/writer"},
					{Name: "reader", Path: "/x/reader"},
				},
			},
		},
	}
}

// oneNestedCatalog: one root "outer" with one child "inner" which has
// one skill "helper". Used by fold tests — expanded View should show
// outer header + inner header + helper (3 rows); folded should collapse
// to just the outer header (1 row).
func oneNestedCatalog() *discover.Catalog {
	return &discover.Catalog{
		Roots: []*discover.Category{
			{
				PluginName: "outer",
				FetchOK:    true,
				Children: []*discover.Category{
					{
						PluginName: "inner",
						FetchOK:    true,
						Skills:     []discover.Skill{{Name: "helper", Path: "/p/helper"}},
					},
				},
			},
		},
	}
}

// mustModel unwraps the (tea.Model, tea.Cmd) Update return into a Model.
// Fails the test if Update ever returns a non-Model tea.Model.
func mustModel(t *testing.T, mi tea.Model) Model {
	t.Helper()
	m, ok := mi.(Model)
	require.True(t, ok, "Update must return a Model, got %T", mi)
	return m
}

func TestNewModelAllChecked(t *testing.T) {
	m := NewModel(sampleCatalog())
	sel := m.Selection()
	require.Len(t, sel.SkillPaths, 1, "only the docs/writer skill is discoverable; remote plugin has no skills")
	assert.Equal(t, "/x/writer", sel.SkillPaths[0])
}

// TestSpaceTogglesRow: with cursor on row 0 (the docs header), Space
// recursively toggles every descendant skill. There's exactly one
// descendant skill in this catalog, so toggling the header leaves the
// selection empty.
func TestSpaceTogglesRow(t *testing.T) {
	m := NewModel(sampleCatalog())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m2 := mustModel(t, updated)
	assert.Empty(t, m2.Selection().SkillPaths, "toggling the header flips the only descendant skill off")
}

func TestViewIncludesUnableToFetch(t *testing.T) {
	m := NewModel(sampleCatalog())
	view := m.View()
	assert.Contains(t, view, "unable to fetch", "the failed plugin should announce its fetch failure in the rendered tree")
}

// TestDownMovesCursor: the cursor starts on row 0 (the docs header).
// Down moves to row 1 (writer). Space on a skill row toggles only that
// skill — the header on row 0 is not affected, so reader (row 2) stays
// checked and only reader ends up in Selection.
func TestDownMovesCursor(t *testing.T) {
	m := NewModel(twoSkillCatalog())

	// Sanity: cursor begins on the header, not a skill. Toggle the header
	// would flip both; we want to prove row-by-row granularity.
	assert.Equal(t, 0, m.cursor)
	require.GreaterOrEqual(t, len(m.rows), 3, "twoSkillCatalog should produce header + 2 skill rows")

	// Down twice: 0 (header) -> 1 (writer) -> 2 (reader), then up once.
	m2 := mustModel(t, sendKey(m, tea.KeyDown))
	m3 := mustModel(t, sendKey(m2, tea.KeyDown))
	m4 := mustModel(t, sendKey(m3, tea.KeyUp))
	require.Equal(t, 1, m4.cursor, "Down twice then Up once should land on row 1 (writer)")

	// Space on row 1: toggles just writer (a skill, not a header).
	toggled := mustModel(t, sendKey(m4, tea.KeySpace))

	got := toggled.Selection().SkillPaths
	require.Len(t, got, 1, "after toggling row 1, only row 2 (reader) should remain selected")
	assert.Equal(t, "/x/reader", got[0], "writer (row 1) must be off; reader (row 2) must still be on")
}

// sendKey feeds a synthesized keypress through Update and unwraps the
// (tea.Model, tea.Cmd) return — defined as a free function (not a Model
// method) so it composes cleanly inside `mustModel(...)`.
func sendKey(m Model, k tea.KeyType) tea.Model {
	out, _ := m.Update(tea.KeyMsg{Type: k})
	return out
}

// TestSpaceOnCategoryHeaderTogglesAllDescendantSkills verifies the new
// "Space on a header toggles the whole subtree" behavior: one category
// with two skills, cursor on row 0 (the header), Space flips both
// skills off; another Space flips them both back on.
func TestSpaceOnCategoryHeaderTogglesAllDescendantSkills(t *testing.T) {
	cat := &discover.Catalog{
		Roots: []*discover.Category{
			{
				PluginName: "docs",
				FetchOK:    true,
				Skills: []discover.Skill{
					{Name: "writer", Path: "/x/writer"},
					{Name: "reader", Path: "/x/reader"},
				},
			},
		},
	}
	m := NewModel(cat)
	require.Equal(t, 0, m.cursor, "cursor should land on the first header")

	// First Space: both skills flip from checked → unchecked.
	m1 := mustModel(t, sendKey(m, tea.KeySpace))
	assert.Empty(t, m1.Selection().SkillPaths, "header Space should clear all descendant skills")

	// Second Space: both skills flip back.
	m2 := mustModel(t, sendKey(m1, tea.KeySpace))
	sel := m2.Selection().SkillPaths
	assert.Len(t, sel, 2, "header Space again should recheck every descendant skill")
}

// TestRightArrowExpandsAndLeftFolds walks the user through the fold
// transitions on the inner (nested) sub-plugin. Roots start expanded;
// nested sub-plugins start folded but their header remains visible so
// the user can navigate to it and Right-arrow to expand.
//
//   step 0: initial       → 2 rows (outer header + inner header; inner folded)
//   step 1: Down to inner → cursor 1
//   step 2: Right on inner → 3 rows (+ helper skill)
//   step 3: Left on inner  → 2 rows (helper re-hidden)
func TestRightArrowExpandsAndLeftFolds(t *testing.T) {
	cat := oneNestedCatalog()
	m := NewModel(cat)
	require.Equal(t, 2, len(m.rows), "outer + inner headers, inner folded")

	mDown := mustModel(t, sendKey(m, tea.KeyDown))
	require.Equal(t, 1, mDown.cursor, "Down moves cursor to the inner header")

	expanded := mustModel(t, sendKey(mDown, tea.KeyRight))
	require.Equal(t, 3, len(expanded.rows), "Right on inner header should reveal its helper skill")

	reFolded := mustModel(t, sendKey(expanded, tea.KeyLeft))
	require.Equal(t, 2, len(reFolded.rows), "Left on inner header should re-hide its helper skill")
}

// TestSpaceOnCategoryHeaderTogglesNestedSubtree verifies that the
// "header Space" recursion descends past intermediate headers into
// nested subtrees: root "outer" with one skill + one child "inner"
// with one skill; Space on the root header flips BOTH skills off.
func TestSpaceOnCategoryHeaderTogglesNestedSubtree(t *testing.T) {
	cat := &discover.Catalog{
		Roots: []*discover.Category{
			{
				PluginName: "outer",
				FetchOK:    true,
				Skills:     []discover.Skill{{Name: "root-skill", Path: "/p/root"}},
				Children: []*discover.Category{
					{
						PluginName: "inner",
						FetchOK:    true,
						Skills:     []discover.Skill{{Name: "nested-skill", Path: "/p/nested"}},
					},
				},
			},
		},
	}
	m := NewModel(cat)
	require.Equal(t, 0, m.cursor, "cursor must be on the outermost header")

	m1 := mustModel(t, sendKey(m, tea.KeySpace))
	sel := m1.Selection().SkillPaths
	assert.Empty(t, sel, "Space on root header should clear both root and nested subtree skills")
}

// TestNewModelFoldsNestedSubPluginsByDefault locks in the default-view
// policy: a brand-new Model shows only the *first layer of skills* —
// i.e. Roots and their direct skill rows. Nested sub-plugin HEADERS
// remain visible (so the user can drill in with Right-arrow), but their
// skill rows and any deeper sub-plugins are hidden until the user
// expands them.
//
// Verified two ways:
//   1. The inner plugin's skill ("helper") must not appear in the rendered View.
//   2. The inner plugin header itself IS visible (so it can be navigated to
//      and Right-arrowed). Asserted via presence of "inner" in the View.
func TestNewModelFoldsNestedSubPluginsByDefault(t *testing.T) {
	cat := oneNestedCatalog()
	m := NewModel(cat)

	require.GreaterOrEqual(t, len(m.rows), 2, "Roots + nested sub-plugin header should both be visible (folded but reachable)")

	view := m.View()
	assert.Contains(t, view, "outer", "root header must render")
	assert.Contains(t, view, "inner", "nested sub-plugin header must remain visible so the user can navigate to and unfold it")
	assert.NotContains(t, view, "helper", "nested skill row must NOT render until the user expands the inner plugin")
}
