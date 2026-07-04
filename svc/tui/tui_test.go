package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bizshuk/skills/svc/discover"
	tea "github.com/charmbracelet/bubbletea"
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
//	step 0: initial       → 2 rows (outer header + inner header; inner folded)
//	step 1: Down to inner → cursor 1
//	step 2: Right on inner → 3 rows (+ helper skill)
//	step 3: Left on inner  → 2 rows (helper re-hidden)
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
//  1. The inner plugin's skill ("helper") must not appear in the rendered View.
//  2. The inner plugin header itself IS visible (so it can be navigated to
//     and Right-arrowed). Asserted via presence of "inner" in the View.
func TestNewModelFoldsNestedSubPluginsByDefault(t *testing.T) {
	cat := oneNestedCatalog()
	m := NewModel(cat)

	require.GreaterOrEqual(t, len(m.rows), 2, "Roots + nested sub-plugin header should both be visible (folded but reachable)")

	view := m.View()
	assert.Contains(t, view, "outer", "root header must render")
	assert.Contains(t, view, "inner", "nested sub-plugin header must remain visible so the user can navigate to and unfold it")
	assert.NotContains(t, view, "helper", "nested skill row must NOT render until the user expands the inner plugin")
}

// typeRune feeds a single printable character through Update using the
// same message shape bubbletea produces for a real keystroke (Type
// KeyRunes with a one-element Runes slice). It's free-standing (not a
// method on Model) so it composes inside `mustModel(...)` like sendKey.
func typeRune(m Model, r rune) tea.Model {
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return out
}

// TestSearchFiltersRows: a 3-plugin catalog where only skill B matches
// a search for "b". The non-B plugins disappear entirely (the alpha
// header survives because its child "B" matches; the beta and gamma
// headers have no matching descendants and so are dropped).
func TestSearchFiltersRows(t *testing.T) {
	cat := &discover.Catalog{
		Roots: []*discover.Category{
			{
				PluginName: "alpha",
				FetchOK:    true,
				Skills: []discover.Skill{
					{Name: "A", Path: "/x/a"},
					{Name: "B", Path: "/x/b"},
				},
			},
			{
				PluginName: "beta",
				FetchOK:    true,
				Skills:     []discover.Skill{{Name: "C", Path: "/x/c"}},
			},
			{
				PluginName: "gamma",
				FetchOK:    true,
				Skills:     []discover.Skill{{Name: "D", Path: "/x/d"}},
			},
		},
	}
	m := NewModel(cat)
	filtered := mustModel(t, typeRune(m, 'b')) // type "b"
	require.GreaterOrEqual(t, len(filtered.rows), 2, "filtered view retains alpha header + skill B")
	// Cursor pinned at 0 after the row set shrinks (rebuildVisible clamps).
	assert.Equal(t, 0, filtered.cursor)

	view := filtered.View()
	assert.Contains(t, view, "B ", "skill B is visible")
	// Each filtered-out skill appears only on its own row; absence of
	// the standalone letter surrounded by spaces is direct evidence.
	assert.NotContains(t, view, " A ", "skill A row absent from filtered view")
	assert.NotContains(t, view, " C ", "skill C row absent from filtered view")
	assert.NotContains(t, view, " D ", "skill D row absent from filtered view")
	// The header for the plugin containing matching skill B stays.
	assert.Contains(t, view, "alpha", "header for the plugin containing matching skill B remains visible")
	// Plugins with no matching descendants disappear entirely.
	assert.NotContains(t, view, "beta ", "beta header has no matching descendants, must be hidden")
	assert.NotContains(t, view, "gamma ", "gamma header has no matching descendants, must be hidden")
}

// TestSearchClearsOnEsc: typing something makes the row set shrink;
// Esc with a non-empty search clears the search rather than quitting,
// and the full row set reappears.
func TestSearchClearsOnEsc(t *testing.T) {
	cat := &discover.Catalog{
		Roots: []*discover.Category{
			{
				PluginName: "p1",
				FetchOK:    true,
				Skills: []discover.Skill{
					{Name: "alpha", Path: "/x/alpha"},
					{Name: "beta", Path: "/x/beta"},
				},
			},
		},
	}
	m := NewModel(cat)
	fullView := m.View()
	assert.Contains(t, fullView, "alpha")
	assert.Contains(t, fullView, "beta")

	// Type "alpha" — only that skill matches.
	updated := mustModel(t, typeRune(m, 'a'))
	for _, r := range "lpha" {
		updated = mustModel(t, typeRune(updated, r))
	}
	filtered := updated.View()
	assert.Contains(t, filtered, "alpha")
	assert.NotContains(t, filtered, "beta", "search should hide the unmatched skill")

	// First Esc with non-empty search should clear, not quit.
	cleared := mustModel(t, sendKey(updated, tea.KeyEsc))
	require.Equal(t, "alpha", updated.search.Value(),
		"sanity: search field still has 'alpha' before Esc")
	restored := cleared.View()
	assert.Contains(t, restored, "alpha")
	assert.Contains(t, restored, "beta", "after Esc clears, both skills should be visible again")

	// Second Esc on empty search → quits (returns tea.Quit cmd).
	_, cmd := cleared.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.NotNil(t, cmd, "second Esc with empty search should produce a quit command")
}

// TestViewportClipsToHeight: with viewportHeight set below the number
// of visible rows, View shows only that many body rows and appends a
// `↓ N more` footer for the remainder.
func TestViewportClipsToHeight(t *testing.T) {
	cat := &discover.Catalog{
		Roots: []*discover.Category{
			{
				PluginName: "p1",
				FetchOK:    true,
				Skills: []discover.Skill{
					{Name: "s1", Path: "/x/s1"},
					{Name: "s2", Path: "/x/s2"},
					{Name: "s3", Path: "/x/s3"},
					{Name: "s4", Path: "/x/s4"},
					{Name: "s5", Path: "/x/s5"},
				},
			},
		},
	}
	m := NewModel(cat)
	require.Equal(t, 6, len(m.rows), "header + 5 skills")

	m.viewportHeight = 3
	view := m.View()

	// Body row count is exactly the viewport.
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	// lines[0]=title, [1]="Search: ...", [2]="↑↓ ...", [3]="", then body.
	// Body rows are lines[4:7]. The "↓ N more" must appear at the bottom.
	bodyEnd := 4 + 3
	require.GreaterOrEqual(t, len(lines), bodyEnd+1, "View must include the '↓ N more' line after 3 body rows")
	assert.Equal(t, "↓ 3 more", strings.TrimSpace(lines[bodyEnd]),
		"footer reports 6-3=3 rows still off-screen")
}

// TestDescriptionRendered: a skill with a populated Description field
// shows it in parens (`name (description)`). Truncation itself is
// tested in the manifest package — the TUI just renders whatever it
// receives.
func TestDescriptionRendered(t *testing.T) {
	cat := &discover.Catalog{
		Roots: []*discover.Category{
			{
				PluginName: "p1",
				FetchOK:    true,
				Skills: []discover.Skill{
					{Name: "shorty", Path: "/x/shorty",
						Description: "Use when fooing the bar"},
					{Name: "emptyy", Path: "/x/emptyy",
						Description: ""},
				},
			},
		},
	}
	m := NewModel(cat)
	view := m.View()

	assert.Contains(t, view, "shorty (Use when fooing the bar)",
		"description rendered verbatim inside parens after the name")
	assert.Contains(t, view, "emptyy ()",
		"missing description renders as empty parens for visual rhythm")
}

// TestCursorStaysVisible: when the cursor moves below the viewport, the
// rendered View must include the focused row (the `> ` prefix appears
// somewhere within the visible window — not cut off).
func TestCursorStaysVisible(t *testing.T) {
	// Build a 12-row catalog so several steps past viewportHeight (3)
	// still have rows to scroll into.
	cat := &discover.Catalog{
		Roots: []*discover.Category{
			{
				PluginName: "p1",
				FetchOK:    true,
				Skills: func() []discover.Skill {
					out := make([]discover.Skill, 11)
					for i := range out {
						out[i] = discover.Skill{
							Name:        fmt.Sprintf("s%02d", i+1),
							Path:        fmt.Sprintf("/x/s%02d", i+1),
							Description: ".",
						}
					}
					return out
				}(),
			},
		},
	}
	m := NewModel(cat)
	m.viewportHeight = 3
	require.Equal(t, 12, len(m.rows)) // header + 11 skills

	// Press Down enough times to push cursor well past the viewport.
	cur := m
	for i := 0; i < 8; i++ {
		cur = mustModel(t, sendKey(cur, tea.KeyDown))
	}
	require.Equal(t, 8, cur.cursor, "cursor at row 8 after 8 Down presses (header at 0, s01 at 1, ...")

	view := cur.View()

	// Find the body line containing '> ' (cursor glyph) and verify it
	// names the cursor's skill. Skill rows are indented by 2 spaces
	// before the cursor glyph, so a substring match is more reliable
	// than a prefix match. With offset=6 and h=3 the window covers rows
	// 6,7,8 → s06, s07, s08 — s08 is focused.
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	var focused string
	for _, ln := range lines {
		// Skip header / footer / search / blank / counter lines.
		if strings.HasPrefix(strings.TrimSpace(ln), "↓") {
			continue
		}
		if strings.Contains(ln, "> ● s08") {
			focused = ln
			break
		}
	}
	require.NotEmpty(t, focused,
		"a body line containing '> ● s08' must appear (cursor row should be in view); view=\n%s", view)
}
