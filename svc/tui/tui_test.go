package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bizshuk/skills/model"
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
				Skills: []model.Skill{
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
				Skills: []model.Skill{
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
						Skills:     []model.Skill{{Name: "helper", Path: "/p/helper"}},
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
	// 2026-07-11-skills-add-fold-plugin: local roots now start folded;
	// expand the only header so the skill rows are reachable.
	expanded := mustModel(t, sendKey(m, tea.KeyRight))

	m2 := mustModel(t, sendKey(expanded, tea.KeyDown))
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
				Skills: []model.Skill{
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
// transitions on a nested sub-plugin tree. With fold-everything-by-default
// the inner sub-plugin is hidden until the user expands the OUTER header
// (cascade); Right on outer reveals outer + inner + helper; Left on outer
// hides the whole subtree again.
//
//	step 0: initial       → 1 row (outer header only; inner hidden)
//	step 1: Right on outer → 3 rows (outer + inner + helper, cascade unfold)
//	step 2: Left on outer  → 1 row (cascade fold hides the whole subtree)
func TestRightArrowExpandsAndLeftFolds(t *testing.T) {
	cat := oneNestedCatalog()
	m := NewModel(cat, nil)
	require.Equal(t, 1, len(m.rows), "only outer header is visible; inner is hidden behind folded outer")

	expanded := mustModel(t, sendKey(m, tea.KeyRight))
	require.Equal(t, 3, len(expanded.rows), "Right on outer cascades unfold: outer + inner + helper")

	reFolded := mustModel(t, sendKey(expanded, tea.KeyLeft))
	require.Equal(t, 1, len(reFolded.rows), "Left on outer cascades fold: only outer header remains")
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
				Skills:     []model.Skill{{Name: "root-skill", Path: "/p/root"}},
				Children: []*plugin.Category{
					{
						PluginName: "inner",
						FetchOK:    true,
						Skills:     []model.Skill{{Name: "nested-skill", Path: "/p/nested"}},
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

// TestNewModelHidesNestedChildrenWhenParentFolded locks in the default-view
// policy: a brand-new Model shows ONLY the root header — its child header
// rows and any deeper sub-plugins are hidden until the user expands the
// root with Right-arrow (cascade). This is what makes "folded" visually
// mean "everything below this header is hidden"; without the gate, fold
// state only hid skill/subagent rows and child headers leaked through,
// making a folded root look identical to an expanded one.
//
// Verified two ways:
//  1. The inner plugin's skill ("helper") must not appear in the rendered View.
//  2. The inner plugin header itself must NOT appear — the only way to
//     reach it is Right-arrow on the root to cascade unfold.
func TestNewModelHidesNestedChildrenWhenParentFolded(t *testing.T) {
	cat := oneNestedCatalog()
	m := NewModel(cat, nil)

	require.Equal(t, 1, len(m.rows), "only the root header is visible; nested children are hidden until cascade unfold")

	view := m.View()
	assert.Contains(t, view, "outer", "root header must render")
	assert.NotContains(t, view, "inner", "nested sub-plugin header must stay hidden until the root is expanded")
	assert.NotContains(t, view, "helper", "nested skill row must NOT render until the root is expanded")
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
				Skills: []model.Skill{
					{Name: "apple", Path: "/x/apple"},
					{Name: "zulu", Path: "/x/zulu"},
				},
			},
			{
				PluginName: "beta",
				FetchOK:    true,
				Skills:     []model.Skill{{Name: "charlie", Path: "/x/charlie"}},
			},
			{
				PluginName: "gamma",
				FetchOK:    true,
				Skills:     []model.Skill{{Name: "delta", Path: "/x/delta"}},
			},
		},
	}
	m := NewModel(cat, nil)
	filtered := mustModel(t, typeRune(m, 'z')) // type "z" — matches only zulu skill
	// 2026-07-11-skills-add-fold-plugin: search hits a folded root's skill
	// but the matched row stays hidden. Expand the matching header.
	expanded := mustModel(t, sendKey(filtered, tea.KeyRight))
	require.GreaterOrEqual(t, len(expanded.rows), 2, "filtered view retains alpha header + zulu skill")
	// Cursor pinned at 0 after the row set shrinks (rebuildVisible clamps).
	assert.Equal(t, 0, expanded.cursor)
	filtered = expanded

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
				Skills: []model.Skill{
					{Name: "alpha", Path: "/x/alpha"},
					{Name: "beta", Path: "/x/beta"},
				},
			},
		},
	}
	m := NewModel(cat, nil)
	// 2026-07-11-skills-add-fold-plugin: local roots now start folded;
	// expand the only header so the skill rows are visible.
	expanded := mustModel(t, sendKey(m, tea.KeyRight))
	fullView := expanded.View()
	assert.Contains(t, fullView, "alpha")
	assert.Contains(t, fullView, "beta")

	// Type "alpha" — only that skill matches.
	updated := mustModel(t, typeRune(expanded, 'a'))
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
				Skills: []model.Skill{
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
	// 2026-07-11-skills-add-fold-plugin: local roots now start folded;
	// expand the only header so the 5 skill rows join the header row.
	expanded := mustModel(t, sendKey(m, tea.KeyRight))
	m = expanded
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
				Skills: []model.Skill{
					{Name: "shorty", Path: "/x/shorty",
						Description: "Use when fooing the bar"},
					{Name: "emptyy", Path: "/x/emptyy",
						Description: ""},
				},
			},
		},
	}
	m := NewModel(cat, nil)
	// 2026-07-11-skills-add-fold-plugin: local roots now start folded;
	// expand the only header so the skill rows render.
	expanded := mustModel(t, sendKey(m, tea.KeyRight))
	view := expanded.View()

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
				Skills: func() []model.Skill {
					out := make([]model.Skill, 11)
					for i := range out {
						out[i] = model.Skill{
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
	// 2026-07-11-skills-add-fold-plugin: local roots now start folded;
	// expand the only header so the 11 skill rows join the header row.
	expanded := mustModel(t, sendKey(m, tea.KeyRight))
	m = expanded
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

// TestAllRootPluginsAreFoldedByDefault verifies the contract introduced by
// 2026-07-11-skills-add-fold-plugin: every root plugin (regardless of
// OwnerRepo) starts folded so skills are hidden until the user expands the
// header with Right-arrow. Local and remote roots share the same fold key.
func TestAllRootPluginsAreFoldedByDefault(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "local-plugin",
				FetchOK:    true,
				Skills:     []model.Skill{{Name: "local-skill", Path: "/p/local"}},
			},
			{
				PluginName: "remote-plugin",
				OwnerRepo:  "owner/repo",
				FetchOK:    true,
				Skills:     []model.Skill{{Name: "remote-skill", Path: "/p/remote"}},
			},
		},
	}
	m := NewModel(cat, nil)
	view := m.View()
	// Every root starts folded — skills stay hidden until the user expands.
	assert.NotContains(t, view, "local-skill",
		"local root plugin skill must NOT render since all roots now start folded")
	assert.NotContains(t, view, "remote-skill",
		"remote root plugin skill must NOT render since all roots now start folded")
	// Headers remain visible so the user can navigate to and expand each.
	assert.Contains(t, view, "local-plugin", "local root header must remain visible")
	assert.Contains(t, view, "remote-plugin", "remote root header must remain visible")
	assert.Contains(t, view, "owner/repo", "remote root header keeps showing OwnerRepo")
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
				Skills: []model.Skill{
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
	// 2026-07-11-skills-add-fold-plugin: local roots now start folded;
	// expand the only header first so the skill row becomes reachable.
	expanded := mustModel(t, sendKey(m, tea.KeyRight))
	view1 := expanded.View()
	assert.Contains(t, view1, "long-skill — This is an extremely long description that definitely exceed...",
		"folded view (skill-level) should truncate the description to 60 characters with ellipsis")

	// Move cursor to the skill (row 1, since row 0 is the category header)
	mDown := mustModel(t, sendKey(expanded, tea.KeyDown))
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

// TestAllRootsFolded_BothRequireExpansion covers the contract for the new
// fold-everything-by-default policy: a fixture with one local and one
// remote root starts with no skills visible, expanding either one
// individually surfaces only its own skill, and expanding both surfaces
// both. This pins the per-root cascade-expand path for the homogeneous
// root fold state introduced by 2026-07-11-skills-add-fold-plugin.
func TestAllRootsFolded_BothRequireExpansion(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "local-plugin",
				FetchOK:    true,
				Skills:     []model.Skill{{Name: "local-skill", Path: "/p/local"}},
			},
			{
				PluginName: "remote-plugin",
				OwnerRepo:  "owner/repo",
				FetchOK:    true,
				Skills:     []model.Skill{{Name: "remote-skill", Path: "/p/remote"}},
			},
		},
	}
	m := NewModel(cat, nil)
	require.Equal(t, 0, m.cursor, "cursor must land on the first header")

	// Both roots start folded: zero skills rendered, only the two headers.
	view0 := m.View()
	assert.NotContains(t, view0, "local-skill", "local skill hidden initially")
	assert.NotContains(t, view0, "remote-skill", "remote skill hidden initially")
	require.Equal(t, 2, len(m.rows), "only the two root headers should be visible")

	// Right on local root only.
	mExpandLocal := mustModel(t, sendKey(m, tea.KeyRight))
	view1 := mExpandLocal.View()
	assert.Contains(t, view1, "local-skill", "Right on local exposes local skill")
	assert.NotContains(t, view1, "remote-skill", "Right on local keeps remote skill hidden")

	// Cursor on the remote header; Right on remote only.
	mDown := mustModel(t, sendKey(m, tea.KeyDown))
	mExpandRemote := mustModel(t, sendKey(mDown, tea.KeyRight))
	view2 := mExpandRemote.View()
	assert.Contains(t, view2, "local-skill", "Right on remote keeps local skill visible (local already expanded)")
	assert.Contains(t, view2, "remote-skill", "Right on remote exposes remote skill")
}

// TestCascadeUnfold_LocalAndRemoteRootsSymmetric guarantees that the
// homogeneous fold policy uses a single code path for every root —
// pressing Right on a local root must produce exactly the same row
// transition as pressing Right on a remote root. If future refactors
// accidentally introduce a separate "local-only" shortcut (e.g.
// skipping the cascade helpers), this test catches it.
func TestCascadeUnfold_LocalAndRemoteRootsSymmetric(t *testing.T) {
	makeCatalog := func(localName string, localPath string) *plugin.Catalog {
		return &plugin.Catalog{
			Roots: []*plugin.Category{
				{
					PluginName: localName,
					FetchOK:    true,
					Skills:     []model.Skill{{Name: localName + "-skill", Path: localPath}},
				},
			},
		}
	}

	// Local root: no OwnerRepo.
	mLocal := NewModel(makeCatalog("local-plugin", "/p/local"), nil)
	require.Equal(t, 1, len(mLocal.rows), "local root alone: 1 header row initially")
	mLocalExp := mustModel(t, sendKey(mLocal, tea.KeyRight))
	require.Equal(t, 2, len(mLocalExp.rows),
		"Right on local root: header + 1 skill row (same shape as remote)")

	// Remote root: with OwnerRepo.
	mRemote := NewModel(makeCatalog("remote-plugin", "/p/remote"), nil)
	// Manually inject OwnerRepo after construction so a single helper drives both.
	mRemote.cat.Roots[0].OwnerRepo = "owner/repo"
	require.Equal(t, 1, len(mRemote.rows), "remote root alone: 1 header row initially")
	mRemoteExp := mustModel(t, sendKey(mRemote, tea.KeyRight))
	require.Equal(t, 2, len(mRemoteExp.rows),
		"Right on remote root: header + 1 skill row (same shape as local)")

	// Both views render their respective skill line; pre/post row counts
	// are identical, proving the fold key is the same and the cascade
	// branch is reached for both.
	viewLocal := mLocalExp.View()
	viewRemote := mRemoteExp.View()
	assert.Contains(t, viewLocal, "local-plugin-skill")
	assert.Contains(t, viewRemote, "remote-plugin-skill")
}



// subagentCatalog is a fixture with one skill and one subagent.
func subagentCatalog() *plugin.Catalog {
	return &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "p1",
				FetchOK:    true,
				Skills:     []model.Skill{{Name: "writer", Path: "/x/writer"}},
				Subagents:  []model.Subagent{{Name: "reviewer", Path: "/x/reviewer.md", Description: "Review PRs"}},
			},
		},
	}
}

// TestSubagentDistinctIcon verifies subagent rows render with the diamond
// (◇) icon instead of the circle (○) used for skills.
func TestSubagentDistinctIcon(t *testing.T) {
	m := NewModel(subagentCatalog(), nil)
	// 2026-07-11-skills-add-fold-plugin: local roots now start folded;
	// expand the only header so the subagent row is visible.
	expanded := mustModel(t, sendKey(m, tea.KeyRight))
	view := expanded.View()
	assert.Contains(t, view, "◇", "subagent should render with hollow diamond icon")
	assert.Contains(t, view, "reviewer", "subagent name should be visible")
}

// TestHeaderToggleAlsoTogglesSubagents verifies that pressing Space on a
// category header checks both skills AND subagents under that category.
func TestHeaderToggleAlsoTogglesSubagents(t *testing.T) {
	m := NewModel(subagentCatalog(), nil)
	updated := mustModel(t, sendKey(m, tea.KeySpace))
	sel := updated.Selection()
	assert.Contains(t, sel.SkillPaths, "/x/writer", "Space on header should check the skill")
	assert.Contains(t, sel.SubagentPaths, "/x/reviewer.md", "Space on header should check the subagent")
}

// TestSubagentSelectionSpaceOnRow verifies pressing Space directly on a
// subagent row toggles it individually, without affecting skills.
func TestSubagentSelectionSpaceOnRow(t *testing.T) {
	m := NewModel(subagentCatalog(), nil)
	// 2026-07-11-skills-add-fold-plugin: local roots now start folded;
	// expand the only header so the skill/subagent rows are reachable.
	expanded := mustModel(t, sendKey(m, tea.KeyRight))
	// Cursor 0 = header, cursor 1 = skill writer, cursor 2 = subagent reviewer
	// Move cursor to row 2 (subagent reviewer)
	m2 := mustModel(t, sendKey(expanded, tea.KeyDown))  // row 1: skill
	m3 := mustModel(t, sendKey(m2, tea.KeyDown)) // row 2: subagent
	require.Equal(t, 2, m3.cursor)
	// Space on subagent row
	m4 := mustModel(t, sendKey(m3, tea.KeySpace))
	sel := m4.Selection()
	assert.Empty(t, sel.SkillPaths, "skill should still be unchecked")
	assert.Contains(t, sel.SubagentPaths, "/x/reviewer.md", "subagent should be checked")
}

// TestSubagentDescriptionRendered verifies subagent descriptions render
// after the em-dash separator, same as skills.
func TestSubagentDescriptionRendered(t *testing.T) {
	m := NewModel(subagentCatalog(), nil)
	// 2026-07-11-skills-add-fold-plugin: local roots now start folded;
	// expand the only header so the subagent row is visible.
	expanded := mustModel(t, sendKey(m, tea.KeyRight))
	view := expanded.View()
	assert.Contains(t, view, "reviewer — Review PRs",
		"subagent description should render after em-dash")
}

// TestNoSubagentDescriptionNoDash verifies a subagent with empty
// description omits the em-dash, matching skill behavior.
func TestNoSubagentDescriptionNoDash(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{{
			PluginName: "p1",
			FetchOK:    true,
			Subagents:  []model.Subagent{{Name: "clean", Path: "/x/clean.md", Description: ""}},
		}},
	}
	m := NewModel(cat, nil)
	view := m.View()
	assert.NotContains(t, view, "clean —",
		"subagent with no description should not have em-dash")
}

// TestSearchMatchesSubagentName verifies search filtering works on
// subagent names.
func TestSearchMatchesSubagentName(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{{
			PluginName: "tools",
			FetchOK:    true,
			Skills:     []model.Skill{{Name: "apple", Path: "/x/apple"}},
			Subagents:  []model.Subagent{{Name: "code-reviewer", Path: "/x/cr.md", Description: "Check code"}},
		}},
	}
	m := NewModel(cat, nil)
	// Type "review" — should match subagent name
	filtered := mustModel(t, typeRune(m, 'r'))
	for _, r := range "eview" {
		filtered = mustModel(t, typeRune(filtered, r))
	}
	// 2026-07-11-skills-add-fold-plugin: search alone keeps the matched
	// subagent row folded. Expand the matching header so the row renders.
	expanded := mustModel(t, sendKey(filtered, tea.KeyRight))
	view := expanded.View()
	assert.Contains(t, view, "code-reviewer", "subagent matching search should be visible")
	assert.NotContains(t, view, "apple", "non-matching skill should be hidden")
}

// TestSubagentCountSummary verifies the count summary line includes subagents.
func TestSubagentCountSummary(t *testing.T) {
	m := NewModel(subagentCatalog(), nil)
	view := m.View()
	assert.Contains(t, view, "Subagents: 1", "summary should count subagents")
}

// TestCascadeUnfold_ParentShowsAllDescendants verifies that pressing Right on
// a folded parent category header cascades the unfold to all descendants, so
// the user actually sees the contents (rather than just toggling the parent
// state while each descendant remains independently folded and hides its
// own children). This regression-pins the "second-level remote marketplace
// can't fold/unfold" UX bug.
func TestCascadeUnfold_ParentShowsAllDescendants(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "awesome-claude-code-subagents",
				OwnerRepo:  "voltagent/awesome-claude-code-subagents",
				FetchOK:    true,
				Children: []*plugin.Category{
					{PluginName: "voltagent-lang", FetchOK: true,
						Subagents: []model.Subagent{
							{Name: "python-pro", Path: "/tmp/x/python-pro.md"},
						}},
				},
			},
		},
	}

	m := NewModel(cat, nil)
	view := m.View()
	assert.NotContains(t, view, "python-pro", "subagent should be hidden initially (parent + child folded)")

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m2 := mustModel(t, out)
	view2 := m2.View()
	assert.Contains(t, view2, "python-pro",
		"after Right on parent, the subagent should be visible (cascade unfold)")
}

// TestCascadeFold_ParentHidesAllDescendants verifies the inverse: pressing
// Left on an expanded parent cascades the fold to all descendants.
func TestCascadeFold_ParentHidesAllDescendants(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "awesome-claude-code-subagents",
				OwnerRepo:  "voltagent/awesome-claude-code-subagents",
				FetchOK:    true,
				Children: []*plugin.Category{
					{PluginName: "voltagent-lang", FetchOK: true,
						Subagents: []model.Subagent{
							{Name: "python-pro", Path: "/tmp/x/python-pro.md"},
						}},
				},
			},
		},
	}

	m := NewModel(cat, nil)
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m2 := mustModel(t, out)
	view2 := m2.View()
	assert.Contains(t, view2, "python-pro", "after Right, subagent visible")

	out2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m3 := mustModel(t, out2)
	view3 := m3.View()
	assert.NotContains(t, view3, "python-pro",
		"after Left on parent, subagent should be hidden again (cascade fold)")
}

// TestRight_TogglesFoldOnParent verifies that pressing Right on an
// already-expanded parent header folds it back (toggle behavior). The first
// Right unfolds, the second Right folds. This regression-pins the bug
// where Right only worked in the unfold direction.
func TestRight_TogglesFoldOnParent(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "awesome-claude-code-subagents",
				OwnerRepo:  "voltagent/awesome-claude-code-subagents",
				FetchOK:    true,
				Children: []*plugin.Category{
					{PluginName: "voltagent-lang", FetchOK: true,
						Subagents: []model.Subagent{
							{Name: "python-pro", Path: "/tmp/x/python-pro.md"},
						}},
				},
			},
		},
	}

	m := NewModel(cat, nil)
	// 1st Right: unfold
	out1, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m2 := mustModel(t, out1)
	assert.Contains(t, m2.View(), "python-pro", "after 1st Right, subagent visible")

	// 2nd Right: fold back
	out2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRight})
	m3 := mustModel(t, out2)
	assert.NotContains(t, m3.View(), "python-pro", "after 2nd Right, subagent should be hidden again")

	// 3rd Right: unfold again
	out3, _ := m3.Update(tea.KeyMsg{Type: tea.KeyRight})
	m4 := mustModel(t, out3)
	assert.Contains(t, m4.View(), "python-pro", "after 3rd Right, subagent visible again (toggle works)")
}

// TestLayer2_FoldToggle verifies the user-reported three-layer scenario:
// layer-2 (awesome-claude-code-subagents) and its grandchildren become
// reachable only after Right-arrowing through the cascade. Right on
// layer-1 (cc-plugin) cascades everything; Right on layer-2 then
// selectively folds just layer-2's subtree while leaving layer-1
// expanded. Layer-3 subagent visibility is pinned by the cascade tests
// above; this test pins layer-2's selective-fold path.
func TestLayer2_FoldToggle(t *testing.T) {
	root := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "cc-plugin",
				OwnerRepo:  "",
				FetchOK:    true,
				Children: []*plugin.Category{
					{
						PluginName: "awesome-claude-code-subagents",
						OwnerRepo:  "voltagent/awesome-claude-code-subagents",
						FetchOK:    true,
						Children: []*plugin.Category{
							{PluginName: "voltagent-lang", FetchOK: true,
								Subagents: []model.Subagent{
									{Name: "python-pro", Path: "/tmp/x/py.md"},
								}},
						},
					},
				},
			},
		},
	}

	m := NewModel(root, nil)
	require.Equal(t, 1, len(m.rows),
		"layer-2 header must be hidden initially — only cc-plugin header visible")

	// Right on layer-1 cascades unfold to the entire subtree:
	// cc-plugin + awesome-claude-code-subagents + voltagent-lang + python-pro.
	m1 := mustModel(t, sendKey(m, tea.KeyRight))
	viewCascade := m1.View()
	assert.Contains(t, viewCascade, "python-pro",
		"Right on cc-plugin should cascade through to layer-3 subagents")

	// Down moves cursor to layer-2 (awesome-claude-code-subagents).
	m2 := mustModel(t, sendKey(m1, tea.KeyDown))
	require.Equal(t, 1, m2.cursor, "cursor must land on layer-2 header")

	// Right on layer-2: cc-plugin is already expanded, so this selectively
	// folds layer-2 — its header stays visible (it's a direct child of
	// cc-plugin), but its subtree (voltagent-lang + python-pro) hides.
	m3 := mustModel(t, sendKey(m2, tea.KeyRight))
	viewSelective := m3.View()
	assert.NotContains(t, viewSelective, "python-pro",
		"Right on layer-2 should fold its subtree while leaving layer-1 expanded")
	assert.Contains(t, viewSelective, "awesome-claude-code-subagents",
		"layer-2 header itself remains visible (parent is expanded)")

	// Right again on layer-2 — toggle back to unfolded.
	m4 := mustModel(t, sendKey(m3, tea.KeyRight))
	viewReopen := m4.View()
	assert.Contains(t, viewReopen, "python-pro",
		"Right again on layer-2 should unfold its subtree again")
}

// TestRemoteMarketplaceWithManyChildren_AllHiddenByDefault is the regression
// test for the user-reported bug: a remote root plugin whose marketplace.json
// declares many sub-plugins (the voltagent/awesome-claude-code-subagents
// layout) used to render every child header in the initial view even though
// the root was folded. After gating walk() descent on fold state, the
// initial view shows ONLY the root header; children become visible only
// after Right-arrow on the root.
func TestRemoteMarketplaceWithManyChildren_AllHiddenByDefault(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{PluginName: "tools", FetchOK: true},
			{PluginName: "explore", FetchOK: true},
			{PluginName: "general", FetchOK: true},
			{PluginName: "experiment", FetchOK: true},
			{PluginName: "review", FetchOK: true},
			{PluginName: "media", FetchOK: true},
			{PluginName: "team", FetchOK: true},
			{PluginName: "ultra-explore", FetchOK: true},
			{
				PluginName: "awesome-claude-code-subagents",
				OwnerRepo:  "voltagent/awesome-claude-code-subagents",
				FetchOK:    true,
				Children: []*plugin.Category{
					{PluginName: "voltagent-core-dev", FetchOK: true},
					{PluginName: "voltagent-lang", FetchOK: true,
						Subagents: []model.Subagent{
							{Name: "python-pro", Path: "/x/py.md"},
						}},
					{PluginName: "voltagent-infra", FetchOK: true},
					{PluginName: "voltagent-qa-sec", FetchOK: true},
					{PluginName: "voltagent-data-ai", FetchOK: true},
					{PluginName: "voltagent-dev-exp", FetchOK: true},
					{PluginName: "voltagent-domains", FetchOK: true},
					{PluginName: "voltagent-biz", FetchOK: true},
					{PluginName: "voltagent-meta", FetchOK: true},
					{PluginName: "voltagent-research", FetchOK: true},
				},
			},
			{PluginName: "gosdk", OwnerRepo: "bizshuk/gosdk", FetchOK: true},
		},
	}

	m := NewModel(cat, nil)
	require.Equal(t, 10, len(m.rows),
		"only the 10 root headers should be visible initially — no nested children")

	view := m.View()
	assert.Contains(t, view, "awesome-claude-code-subagents",
		"the remote root header itself must remain visible so the user can navigate to and unfold it")
	for _, child := range []string{
		"voltagent-core-dev", "voltagent-lang", "voltagent-infra",
		"voltagent-qa-sec", "voltagent-data-ai", "voltagent-dev-exp",
		"voltagent-domains", "voltagent-biz", "voltagent-meta", "voltagent-research",
	} {
		assert.NotContains(t, view, child,
			"child header %q must NOT render while its remote-root parent is folded", child)
	}
	assert.NotContains(t, view, "python-pro",
		"subagent under nested child must NOT render while the remote-root parent is folded")

	// Right-arrow on the remote root cascades unfold — children + subagent appear.
	mExp := mustModel(t, sendKey(m, tea.KeyDown)) // cursor moves down toward remote
	// Keep pressing Down until cursor lands on awesome-claude-code-subagents (index 8).
	for mExp.cursor < 8 {
		mExp = mustModel(t, sendKey(mExp, tea.KeyDown))
	}
	require.Equal(t, 8, mExp.cursor)
	mExp = mustModel(t, sendKey(mExp, tea.KeyRight))
	viewExp := mExp.View()
	assert.Contains(t, viewExp, "voltagent-lang",
		"Right on the remote root should cascade unfold and reveal nested children")
	assert.Contains(t, viewExp, "python-pro",
		"Right on the remote root should cascade unfold all the way to nested subagents")
}
