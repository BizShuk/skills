package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bizshuk/skills/svc/agent"
	"github.com/bizshuk/skills/svc/plugin"
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
func sampleCatalog() *plugin.Catalog {
	return &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "docs",
				FetchOK:    true,
				Skills: []plugin.Skill{
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
func twoSkillCatalog() *plugin.Catalog {
	return &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "docs",
				FetchOK:    true,
				Skills: []plugin.Skill{
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
func oneNestedCatalog() *plugin.Catalog {
	return &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "outer",
				FetchOK:    true,
				Children: []*plugin.Category{
					{
						PluginName: "inner",
						FetchOK:    true,
						Skills:     []plugin.Skill{{Name: "helper", Path: "/p/helper"}},
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

func TestNewModelNoneCheckedByDefault(t *testing.T) {
	m := NewModel(sampleCatalog(), nil)
	sel := m.Selection()
	assert.Empty(t, sel.SkillPaths, "all skills start unchecked; user must opt-in with space")
}

// TestSpaceOnHeaderChecksAllDescendants: with cursor on row 0 (the docs
// header), Space recursively toggles every descendant skill. With
// unchecked-by-default, Space CHECKS all skills.
func TestSpaceOnHeaderChecksAllDescendants(t *testing.T) {
	m := NewModel(sampleCatalog(), nil)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m2 := mustModel(t, updated)
	sel := m2.Selection().SkillPaths
	require.Len(t, sel, 1, "Space on header checks the only descendant skill")
	assert.Equal(t, "/x/writer", sel[0])
}

func TestViewIncludesUnableToFetch(t *testing.T) {
	m := NewModel(sampleCatalog(), nil)
	view := m.View()
	assert.Contains(t, view, "unable to fetch", "the failed plugin should announce its fetch failure in the rendered tree")
}

// TestDownMovesCursor: the cursor starts on row 0 (the docs header).
// Down moves to row 1 (writer), then row 2 (reader). Space on a skill
// row CHECKS it (default-unchecked → opt-in). Header is not affected.
func TestDownMovesCursor(t *testing.T) {
	m := NewModel(twoSkillCatalog(), nil)

	m2 := mustModel(t, sendKey(m, tea.KeyDown))
	m3 := mustModel(t, sendKey(m2, tea.KeyDown))
	m4 := mustModel(t, sendKey(m3, tea.KeyUp))
	require.Equal(t, 1, m4.cursor, "Down twice then Up once should land on row 1 (writer)")

	// Space on row 1 (writer): checks it since default is unchecked.
	writerChecked := mustModel(t, sendKey(m4, tea.KeySpace))

	// Down to reader (row 2) then Space to check it.
	m5 := mustModel(t, sendKey(writerChecked, tea.KeyDown))
	readerChecked := mustModel(t, sendKey(m5, tea.KeySpace))

	got := readerChecked.Selection().SkillPaths
	require.Len(t, got, 2, "both skills are now checked via individual Space presses")
	assert.Contains(t, got, "/x/writer")
	assert.Contains(t, got, "/x/reader")
}

// sendKey feeds a synthesized keypress through Update and unwraps the
// (tea.Model, tea.Cmd) return — defined as a free function (not a Model
// method) so it composes cleanly inside `mustModel(...)`.
func sendKey(m Model, k tea.KeyType) tea.Model {
	out, _ := m.Update(tea.KeyMsg{Type: k})
	return out
}

// TestSpaceOnCategoryHeaderChecksAllDescendants: one category with two
// skills, cursor on row 0 (the header). Space checks all descendant
// skills (unchecked-by-default). Second Space unchecks them all.
func TestSpaceOnCategoryHeaderChecksAllDescendants(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "docs",
				FetchOK:    true,
				Skills: []plugin.Skill{
					{Name: "writer", Path: "/x/writer"},
					{Name: "reader", Path: "/x/reader"},
				},
			},
		},
	}
	m := NewModel(cat, nil)
	require.Equal(t, 0, m.cursor, "cursor should land on the first header")

	// First Space: both skills flip from unchecked → checked.
	m1 := mustModel(t, sendKey(m, tea.KeySpace))
	sel1 := m1.Selection().SkillPaths
	require.Len(t, sel1, 2, "header Space should check all descendant skills")

	// Second Space: both skills flip back to unchecked.
	m2 := mustModel(t, sendKey(m1, tea.KeySpace))
	assert.Empty(t, m2.Selection().SkillPaths, "second header Space should uncheck all descendant skills")
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
	m := NewModel(cat, nil)
	require.Equal(t, 2, len(m.rows), "outer + inner headers, inner folded")

	mDown := mustModel(t, sendKey(m, tea.KeyDown))
	require.Equal(t, 1, mDown.cursor, "Down moves cursor to the inner header")

	expanded := mustModel(t, sendKey(mDown, tea.KeyRight))
	require.Equal(t, 3, len(expanded.rows), "Right on inner header should reveal its helper skill")

	reFolded := mustModel(t, sendKey(expanded, tea.KeyLeft))
	require.Equal(t, 2, len(reFolded.rows), "Left on inner header should re-hide its helper skill")
}

// TestSpaceOnCategoryHeaderChecksNestedSubtree verifies that the
// "header Space" recursion descends past intermediate headers into
// nested subtrees: root "outer" with one skill + one child "inner"
// with one skill; Space on the root header checks BOTH skills.
func TestSpaceOnCategoryHeaderChecksNestedSubtree(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "outer",
				FetchOK:    true,
				Skills:     []plugin.Skill{{Name: "root-skill", Path: "/p/root"}},
				Children: []*plugin.Category{
					{
						PluginName: "inner",
						FetchOK:    true,
						Skills:     []plugin.Skill{{Name: "nested-skill", Path: "/p/nested"}},
					},
				},
			},
		},
	}
	m := NewModel(cat, nil)
	require.Equal(t, 0, m.cursor, "cursor must be on the outermost header")

	m1 := mustModel(t, sendKey(m, tea.KeySpace))
	sel := m1.Selection().SkillPaths
	require.Len(t, sel, 2, "Space on root header should check both root and nested subtree skills")
	assert.Contains(t, sel, "/p/root")
	assert.Contains(t, sel, "/p/nested")
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
	m := NewModel(cat, nil)

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

// TestSearchFiltersRows: a 3-plugin catalog where only the skill "zulu"
// matches a search for "z". Alpha survives as the skill's container;
// beta and gamma disappear entirely.
func TestSearchFiltersRows(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "alpha",
				FetchOK:    true,
				Skills: []plugin.Skill{
					{Name: "apple", Path: "/x/apple"},
					{Name: "zulu", Path: "/x/zulu"},
				},
			},
			{
				PluginName: "beta",
				FetchOK:    true,
				Skills:     []plugin.Skill{{Name: "charlie", Path: "/x/charlie"}},
			},
			{
				PluginName: "gamma",
				FetchOK:    true,
				Skills:     []plugin.Skill{{Name: "delta", Path: "/x/delta"}},
			},
		},
	}
	m := NewModel(cat, nil)
	filtered := mustModel(t, typeRune(m, 'z')) // type "z" — matches only zulu skill
	require.GreaterOrEqual(t, len(filtered.rows), 2, "filtered view retains alpha header + zulu skill")
	// Cursor pinned at 0 after the row set shrinks (rebuildVisible clamps).
	assert.Equal(t, 0, filtered.cursor)

	view := filtered.View()
	assert.Contains(t, view, "○ zulu", "zulu skill is visible")
	// No other skill checkbox rows appear.
	assert.NotContains(t, view, "○ apple", "apple row absent from filtered view")
	assert.NotContains(t, view, "○ charlie", "charlie row absent from filtered view")
	assert.NotContains(t, view, "○ delta", "delta row absent from filtered view")
	// Alpha header stays as the skill's container.
	assert.Contains(t, view, "alpha", "alpha header remains because it contains the matching zulu skill")
	// Beta and gamma have no matching descendants and no matching name.
	assert.NotContains(t, view, "beta", "beta header has no 'z' match and must be hidden")
	assert.NotContains(t, view, "gamma", "gamma header has no 'z' match and must be hidden")
}

// TestSearchClearsOnEsc: typing something makes the row set shrink;
// Esc with a non-empty search clears the search rather than quitting,
// and the full row set reappears.
func TestSearchClearsOnEsc(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "p1",
				FetchOK:    true,
				Skills: []plugin.Skill{
					{Name: "alpha", Path: "/x/alpha"},
					{Name: "beta", Path: "/x/beta"},
				},
			},
		},
	}
	m := NewModel(cat, nil)
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
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "p1",
				FetchOK:    true,
				Skills: []plugin.Skill{
					{Name: "s1", Path: "/x/s1"},
					{Name: "s2", Path: "/x/s2"},
					{Name: "s3", Path: "/x/s3"},
					{Name: "s4", Path: "/x/s4"},
					{Name: "s5", Path: "/x/s5"},
				},
			},
		},
	}
	m := NewModel(cat, nil)
	require.Equal(t, 6, len(m.rows), "header + 5 skills")

	m.viewportHeight = 3
	view := m.View()

	// Body row count is exactly the viewport.
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	// lines[0]=title, [1]=summary, [2]="Search: ...", [3]="↑↓ ...", [4]="", then body.
	// Body rows are lines[5:8]. The "↓ N more" must appear at the bottom.
	bodyEnd := 5 + 3
	require.GreaterOrEqual(t, len(lines), bodyEnd+1, "View must include the '↓ N more' line after 3 body rows")
	assert.Equal(t, "↓ 3 more", strings.TrimSpace(lines[bodyEnd]),
		"footer reports 6-3=3 rows still off-screen")
}

// TestDescriptionRendered: a skill with a populated Description field
// shows it in parens (`name (description)`). Truncation itself is
// tested in the manifest package — the TUI just renders whatever it
// receives.
func TestDescriptionRendered(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "p1",
				FetchOK:    true,
				Skills: []plugin.Skill{
					{Name: "shorty", Path: "/x/shorty",
						Description: "Use when fooing the bar"},
					{Name: "emptyy", Path: "/x/emptyy",
						Description: ""},
				},
			},
		},
	}
	m := NewModel(cat, nil)
	view := m.View()

	assert.Contains(t, view, "shorty — Use when fooing the bar",
		"description rendered after em-dash following the skill name")
	assert.NotContains(t, view, "emptyy —",
		"missing description omits the em-dash separator entirely")
}

// TestCursorStaysVisible: when the cursor moves below the viewport, the
// rendered View must include the focused row (the `> ` prefix appears
// somewhere within the visible window — not cut off).
func TestCursorStaysVisible(t *testing.T) {
	// Build a 12-row catalog so several steps past viewportHeight (3)
	// still have rows to scroll into.
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "p1",
				FetchOK:    true,
				Skills: func() []plugin.Skill {
					out := make([]plugin.Skill, 11)
					for i := range out {
						out[i] = plugin.Skill{
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
	m := NewModel(cat, nil)
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
		if strings.Contains(ln, "> ○ s08") {
			focused = ln
			break
		}
	}
	require.NotEmpty(t, focused,
		"a body line containing '> ○ s08' must appear (cursor row should be in view); view=\n%s", view)
}

// twoAgents is a small fixture for the agent- and level-phase tests that
// don't care about checked/detected state (phase-transition and level
// tests). makeAgents computes checked/detected via agent.Detect() against
// the real $HOME rather than these DetectDir values, so tests that DO care
// about checked state must control $HOME themselves (see
// TestAgentPhaseSpaceTogglesAgent).
func twoAgents() []agent.Agent {
	return []agent.Agent{
		{Type: "claude-code", DisplayName: "Claude Code", DetectDir: "/home/user/.claude"},
		{Type: "codex", DisplayName: "Codex", DetectDir: ""},
	}
}

// TestEnterAdvancesThroughAllThreePhases is the core regression test for the
// wizard flow: Enter on the skills screen must move to the agent screen,
// Enter there must move to the level screen, and only Enter on the level
// screen actually quits the program. Before this test existed, Enter quit
// immediately from every phase because the transition was never wired up.
func TestEnterAdvancesThroughAllThreePhases(t *testing.T) {
	m := NewModel(sampleCatalog(), twoAgents())
	require.Equal(t, phaseSkills, m.phase)

	m1 := mustModel(t, sendKey(m, tea.KeyEnter))
	assert.Equal(t, phaseAgents, m1.phase, "Enter on skills phase must advance to agent phase, not quit")

	m2, cmd2 := m1.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2m := mustModel(t, m2)
	assert.Equal(t, phaseLevel, m2m.phase, "Enter on agent phase must advance to level phase, not quit")
	assert.Nil(t, cmd2, "advancing phases must not quit the program")

	_, cmd3 := m2m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.NotNil(t, cmd3, "Enter on the level phase must finally quit the program")
}

// TestEscStepsBackThroughPhases verifies Esc is the inverse of Enter: from
// the level phase it returns to the agent phase, and from the agent phase
// it returns to the skills phase, without quitting or losing state.
func TestEscStepsBackThroughPhases(t *testing.T) {
	m := NewModel(sampleCatalog(), twoAgents())
	m1 := mustModel(t, sendKey(m, tea.KeyEnter)) // -> phaseAgents
	m2 := mustModel(t, sendKey(m1, tea.KeyEnter)) // -> phaseLevel
	require.Equal(t, phaseLevel, m2.phase)

	back1 := mustModel(t, sendKey(m2, tea.KeyEsc))
	assert.Equal(t, phaseAgents, back1.phase, "Esc from level phase should return to agent phase")

	back2 := mustModel(t, sendKey(back1, tea.KeyEsc))
	assert.Equal(t, phaseSkills, back2.phase, "Esc from agent phase should return to skill phase")
}

// TestLevelPhaseDefaultsFromGlobalFlag verifies that entering the level
// phase pre-highlights whichever row matches the model's current Global
// setting (as set by Run from the caller's --global flag), so a user who
// passed --global sees Global already highlighted rather than having to
// navigate to it.
func TestLevelPhaseDefaultsFromGlobalFlag(t *testing.T) {
	m := NewModel(sampleCatalog(), twoAgents())
	m.global = true

	m1 := mustModel(t, sendKey(m, tea.KeyEnter))  // -> phaseAgents
	m2 := mustModel(t, sendKey(m1, tea.KeyEnter)) // -> phaseLevel

	assert.Equal(t, 1, m2.levelCursor, "levelCursor should default to the Global row (index 1) when m.global is true")
}

// TestLevelPhaseSpaceCommitsSelection verifies that Space in the level
// phase sets m.global to match whichever row is highlighted, and that the
// final Selection().Global reflects that choice.
func TestLevelPhaseSpaceCommitsSelection(t *testing.T) {
	m := NewModel(sampleCatalog(), twoAgents())
	m.global = false // starts on Project

	m1 := mustModel(t, sendKey(m, tea.KeyEnter))  // -> phaseAgents
	m2 := mustModel(t, sendKey(m1, tea.KeyEnter)) // -> phaseLevel, levelCursor=0 (Project)
	require.Equal(t, 0, m2.levelCursor)

	// Move down to the Global row and commit it with Space.
	m3 := mustModel(t, sendKey(m2, tea.KeyDown))
	require.Equal(t, 1, m3.levelCursor, "Down should move the highlight to the Global row")

	m4 := mustModel(t, sendKey(m3, tea.KeySpace))
	assert.True(t, m4.global, "Space on the Global row should set m.global = true")
	assert.True(t, m4.Selection().Global, "Selection().Global should reflect the committed choice")
}

// TestLevelPhaseViewShowsBothOptions verifies the level-phase View() text
// includes both Project and Global options with the checked glyph on
// whichever one is currently selected.
func TestLevelPhaseViewShowsBothOptions(t *testing.T) {
	m := NewModel(sampleCatalog(), twoAgents())
	m.global = false
	m1 := mustModel(t, sendKey(m, tea.KeyEnter))
	m2 := mustModel(t, sendKey(m1, tea.KeyEnter))

	view := m2.View()
	assert.Contains(t, view, "Project", "level view must list the Project option")
	assert.Contains(t, view, "Global", "level view must list the Global option")
	assert.Contains(t, view, "Install at Project or Global level?", "level view must show its phase header")
}

// TestAgentPhaseSpaceTogglesAgent verifies Space in the agent phase toggles
// the checked state of the agent under the cursor, and that state survives
// into the final Selection().AgentTypes. $HOME is pointed at a tempdir with
// a ~/.claude sentinel so claude-code starts genuinely detected+checked.
func TestAgentPhaseSpaceTogglesAgent(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o755))
	t.Setenv("HOME", home)

	m := NewModel(sampleCatalog(), twoAgents())
	m1 := mustModel(t, sendKey(m, tea.KeyEnter)) // -> phaseAgents
	require.Equal(t, phaseAgents, m1.phase)
	require.True(t, m1.agents[0].checked, "claude-code should start checked: it's a default type AND its folder exists")
	require.False(t, m1.agents[1].checked, "codex should start unchecked: not a default-checked type")

	// Toggle codex on via Down + Space.
	m2 := mustModel(t, sendKey(m1, tea.KeyDown))
	m3 := mustModel(t, sendKey(m2, tea.KeySpace))

	got := m3.Selection().AgentTypes
	require.Len(t, got, 2, "both claude-code and codex should now be selected")
}

// TestMakeAgentsSkipsUndetectedDefaultAgent verifies that claude-code is
// NOT pre-checked when its folder doesn't exist on disk — being one of the
// three default-checked types isn't enough by itself; the folder must also
// be genuinely detected.
func TestMakeAgentsSkipsUndetectedDefaultAgent(t *testing.T) {
	home := t.TempDir() // empty — no ~/.claude, no ~/.gemini/antigravity*
	t.Setenv("HOME", home)

	rows := makeAgents([]agent.Agent{
		{Type: "claude-code", DisplayName: "Claude Code"},
		{Type: "antigravity", DisplayName: "Antigravity"},
	})

	for _, r := range rows {
		assert.False(t, r.checked, "%s should start unchecked: default type but folder not detected", r.agent.Type)
		assert.False(t, r.detected, "%s should not be marked detected: folder doesn't exist", r.agent.Type)
	}
}

// TestMakeAgentsSkipsNonDefaultDetectedAgent verifies that an agent OUTSIDE
// the three default-checked types (e.g. codex) stays unchecked even when
// its folder IS detected on disk — detection alone isn't enough; the type
// must also be in defaultCheckedAgentTypes.
func TestMakeAgentsSkipsNonDefaultDetectedAgent(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o755))
	t.Setenv("HOME", home)

	rows := makeAgents([]agent.Agent{
		{Type: "codex", DisplayName: "Codex"},
	})

	require.Len(t, rows, 1)
	assert.True(t, rows[0].detected, "codex folder exists, so it should be marked detected")
	assert.False(t, rows[0].checked, "codex is detected but not a default-checked type, so it must stay unchecked")
}

// TestRemoteRootPluginsAreFoldedByDefault verifies that remote root plugins
// start folded by default, while local root plugins start expanded.
func TestRemoteRootPluginsAreFoldedByDefault(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "local-plugin",
				FetchOK:    true,
				Skills:     []plugin.Skill{{Name: "local-skill", Path: "/p/local"}},
			},
			{
				PluginName: "remote-plugin",
				OwnerRepo:  "owner/repo",
				FetchOK:    true,
				Skills:     []plugin.Skill{{Name: "remote-skill", Path: "/p/remote"}},
			},
		},
	}
	m := NewModel(cat, nil)
	view := m.View()
	assert.Contains(t, view, "local-skill", "local root plugin skill must render since it starts expanded")
	assert.NotContains(t, view, "remote-skill", "remote root plugin skill must NOT render since it starts folded")
}

// TestSkillDescriptionFoldUnfold verifies that a skill with a long description
// starts folded (truncated) and can be unfolded with Right-arrow to show the
// full description on the next line, and folded back with Left-arrow.
func TestSkillDescriptionFoldUnfold(t *testing.T) {
	longDesc := "This is an extremely long description that definitely exceeds sixty characters to test the folding behavior."
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "p1",
				FetchOK:    true,
				Skills: []plugin.Skill{
					{
						Name:        "long-skill",
						Path:        "/p/long",
						Description: longDesc,
					},
				},
			},
		},
	}
	m := NewModel(cat, nil)
	view1 := m.View()
	assert.Contains(t, view1, "long-skill — This is an extremely long description that definitely exceed...",
		"folded view should truncate the description to 60 characters with ellipsis")

	// Move cursor to the skill (row 1, since row 0 is the category header)
	mDown := mustModel(t, sendKey(m, tea.KeyDown))
	require.Equal(t, 1, mDown.cursor)

	// Press Right to unfold
	unfolded := mustModel(t, sendKey(mDown, tea.KeyRight))
	view2 := unfolded.View()
	assert.NotContains(t, view2, "long-skill —", "unfolded view should not show description on the name line")
	assert.Contains(t, view2, "This is an extremely long description that", "unfolded view should show the full description")
	assert.Contains(t, view2, "to test the folding behavior.", "unfolded view should show the wrapped/remaining description")

	// Press Left to fold back
	foldedBack := mustModel(t, sendKey(unfolded, tea.KeyLeft))
	view3 := foldedBack.View()
	assert.Contains(t, view3, "long-skill — This is an extremely long description that definitely exceed...",
		"folded-back view should return to the truncated description")
}



