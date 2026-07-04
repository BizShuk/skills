package tui

import (
	"strings"
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

// TestRightArrowExpandsAndLeftFolds verifies that the fold/unfold keys
// change the visible row count. With one root, one child and one skill
// inside the child, the expanded View shows 3 visible rows (header +
// child header + skill); folding the root collapses to 1 visible row
// (just the root header). The test uses strings.Count on the rendered
// View so it would catch a regression in fold rendering, not just in
// the internal rows slice.
func TestRightArrowExpandsAndLeftFolds(t *testing.T) {
	cat := oneNestedCatalog()
	m := NewModel(cat)
	require.Equal(t, 0, m.cursor)
	require.GreaterOrEqual(t, len(m.rows), 3, "expanded default should have at least 3 rows (root header + child header + skill)")

	expandedView := m.View()
	expandedLines := strings.Count(expandedView, "\n")

	// Fold: Left on the root header collapses the subtree.
	folded := mustModel(t, sendKey(m, tea.KeyLeft))
	foldedView := folded.View()
	foldedLines := strings.Count(foldedView, "\n")

	assert.Equal(t, 1, len(folded.rows), "after folding the root, only the root header remains visible")
	assert.Less(t, foldedLines, expandedLines, "folding must reduce the line count in the rendered View")

	// Unfold: Right on the (now visible) root header restores both rows.
	expanded := mustModel(t, sendKey(folded, tea.KeyRight))
	assert.Equal(t, len(m.rows), len(expanded.rows), "after unfolding, the row count matches the initial expanded state")
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
