// Package tui renders a discovered discover.Catalog as an interactive
// tree and returns the user's selection. Every Category in the catalog
// becomes one header row in the rendered tree; every Skill becomes a leaf
// row indented under its owning category header. Pressing space on a
// header recursively toggles every descendant Skill in one shot; pressing
// space on a skill toggles just that skill. Left/right arrows fold and
// unfold the category under the cursor (left on a skill jumps to its
// parent header).
//
// Categories whose remote fetch failed still show as a header, suffixed
// with an "unable to fetch" marker (plus the underlying FetchErr in
// parens when it's a real error rather than the synthetic marker), and
// contribute no skill rows of their own.
//
// The Model intentionally keeps no I/O and no bubbletea runtime state,
// which is what lets the package be unit-tested by feeding synthesized
// tea.KeyMsg values directly into Update without ever calling
// tea.NewProgram(...).Run(). Run is the only place that actually starts
// the bubbletea program; everything else is pure state transitions.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/skills/svc/discover"
	"github.com/bizshuk/skills/svc/install"
)

// row is one visible line in the rendered tree. Headers and skills share
// a row shape so the cursor / scroll logic can treat them uniformly; the
// isHeader / skill fields disambiguate.
type row struct {
	node     *discover.Category // non-nil for both header and skill rows
	skill    *discover.Skill    // nil for header rows, non-nil for skill rows
	depth    int                 // nesting depth (0 for top-level plugin headers)
	isHeader bool               // true for category header rows, false for skill rows
}

// Model is the bubbletea program state. Cursor, rows, fold map and
// per-skill checked map are kept on the value receiver because bubbletea
// treats the model as immutable; mutations inside Update assign back to
// a local copy and return it.
type Model struct {
	cat     *discover.Catalog
	rows    []row
	cursor  int
	global  bool
	done    bool
	folded  map[*discover.Category]bool // fold state keyed by Category pointer
	checked map[string]bool             // checked state keyed by Skill.Path
}

// NewModel materializes the model's rows from the given catalog. Every
// skill starts checked — the user can opt out with space — and failed
// plugins still contribute a header row (to surface the fetch failure)
// but no skill rows of their own.
//
// Fold state: only the top-level (Roots) plugins start expanded. Every
// nested sub-plugin starts folded so the user is not overwhelmed by a
// deeply nested marketplace on first glance; they can drill in with the
// right-arrow key.
func NewModel(cat *discover.Catalog) Model {
	m := Model{
		cat:     cat,
		folded:  map[*discover.Category]bool{},
		checked: map[string]bool{},
	}
	for _, s := range cat.AllSkills() {
		m.checked[s.Path] = true
	}
	// Pre-fold every nested sub-plugin. Roots stay expanded. We do this in
	// a second pass after checked is populated so that rebuildRows sees a
	// stable fold state when it walks the tree.
	var foldNested func(parent *discover.Category)
	foldNested = func(parent *discover.Category) {
		for _, ch := range parent.Children {
			m.folded[ch] = true
			foldNested(ch)
		}
	}
	for _, root := range cat.Roots {
		foldNested(root)
	}
	m.rebuildRows()
	return m
}

// rebuildRows re-walks the catalog tree and rebuilds m.rows to reflect
// the current fold state. Called whenever a header's fold status changes.
func (m *Model) rebuildRows() {
	m.rows = m.rows[:0]
	var walk func(c *discover.Category, depth int)
	walk = func(c *discover.Category, depth int) {
		if c == nil {
			return
		}
		m.rows = append(m.rows, row{node: c, depth: depth, isHeader: true})
		if m.folded[c] {
			return
		}
		for i := range c.Skills {
			m.rows = append(m.rows, row{node: c, skill: &c.Skills[i], depth: depth + 1})
		}
		for _, ch := range c.Children {
			walk(ch, depth+1)
		}
	}
	for _, root := range m.cat.Roots {
		walk(root, 0)
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// headerCheckState returns the aggregate check state for a category
// header: true if every skill in the subtree is checked, false if none
// are checked (or the subtree has no skills — empty → unchecked), and
// "partial" otherwise.
func (m Model) headerCheckState(c *discover.Category) (all bool, partial bool) {
	var total, checked int
	var walk func(n *discover.Category)
	walk = func(n *discover.Category) {
		if n == nil {
			return
		}
		for _, s := range n.Skills {
			total++
			if m.checked[s.Path] {
				checked++
			}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(c)
	if total == 0 {
		return false, false
	}
	if checked == total {
		return true, false
	}
	if checked == 0 {
		return false, false
	}
	return false, true
}

// toggleSubtree flips the checked bit for every Skill path under c.
func (m *Model) toggleSubtree(c *discover.Category) {
	if c == nil {
		return
	}
	// "Select all" when none-or-some are checked; "deselect all" only when
	// every descendant is currently on. This matches the conventional
	// checkbox behavior where space cyclically snaps a mixed state to its
	// deterministic anchor.
	all, _ := m.headerCheckState(c)
	target := !all
	var walk func(n *discover.Category)
	walk = func(n *discover.Category) {
		if n == nil {
			return
		}
		for _, s := range n.Skills {
			m.checked[s.Path] = target
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(c)
}

// findRowForCategory returns the index of the header row whose node is
// c, or -1 if the category's header is currently hidden by an ancestor
// fold.
func (m Model) findRowForCategory(c *discover.Category) int {
	for i, r := range m.rows {
		if r.isHeader && r.node == c {
			return i
		}
	}
	return -1
}

// findParentHeader returns the closest ancestor header row for a skill
// row, or the row's own index when it's already a header.
func (m Model) findParentHeader(idx int) int {
	for i := idx; i >= 0; i-- {
		if m.rows[i].isHeader {
			return i
		}
	}
	return 0
}

// Init satisfies the bubbletea Model interface; we have no startup Cmd.
func (m Model) Init() tea.Cmd { return nil }

// Update dispatches key presses to cursor movement, toggle, fold/unfold
// and quit actions. Non-key messages are ignored. The returned Model
// carries the new cursor / checked / fold state; the returned Cmd is nil
// except on enter (tea.Quit) and esc / ctrl-c (also tea.Quit, just
// without confirming).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case tea.KeySpace:
		if len(m.rows) == 0 {
			break
		}
		r := m.rows[m.cursor]
		if r.isHeader {
			m.toggleSubtree(r.node)
		} else if r.skill != nil {
			m.checked[r.skill.Path] = !m.checked[r.skill.Path]
		}
	case tea.KeyRight:
		if len(m.rows) == 0 {
			break
		}
		r := m.rows[m.cursor]
		if r.isHeader {
			if m.folded[r.node] {
				delete(m.folded, r.node)
				m.rebuildRows()
			}
		} else {
			// Right on a skill: jump to its parent header.
			m.cursor = m.findParentHeader(m.cursor)
		}
	case tea.KeyLeft:
		if len(m.rows) == 0 {
			break
		}
		r := m.rows[m.cursor]
		if r.isHeader {
			if !m.folded[r.node] {
				m.folded[r.node] = true
				m.rebuildRows()
			}
		} else {
			// Left on a skill: jump to its parent header (cursor stays
			// on a visible row regardless of fold state).
			m.cursor = m.findParentHeader(m.cursor)
		}
	case tea.KeyEnter:
		m.done = true
		return m, tea.Quit
	case tea.KeyCtrlC, tea.KeyEsc:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

// unableFetchMarker is the literal text we look for in tests; keep it
// spelled exactly like this so the tests don't get fragile.
const unableFetchMarker = "unable to fetch"

// View renders the tree. The legend on the title line matches the keys
// handled in Update. Each category header is `▾ <pluginName>` when
// expanded and `▸ <pluginName>` when folded; if the category failed to
// fetch, the header is suffixed with `  [unable to fetch]`. The
// checkbox glyphs apply to both header rows (aggregate of descendant
// skills) and skill rows: `☑` all-checked, `☐` none-checked, `▣`
// partial (only on headers — a single skill can never be partial).
func (m Model) View() string {
	var b strings.Builder
	b.WriteString("Select skills (space=toggle, ←/→=fold, enter=confirm)\n")

	if len(m.rows) == 0 {
		// Empty catalog — still useful to render so callers don't have
		// to special-case a zero-plugin run.
		return b.String()
	}

	for i, r := range m.rows {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		// Indent: two spaces per depth level for both headers and skills.
		indent := strings.Repeat("  ", r.depth)

		if r.isHeader {
			glyph := "▸"
			if !m.folded[r.node] {
				glyph = "▾"
			}
			box := "☐"
			all, partial := m.headerCheckState(r.node)
			switch {
			case partial:
				box = "▣"
			case all:
				box = "☑"
			}

			header := r.node.PluginName
			if r.node.OwnerRepo != "" {
				header += "  " + r.node.OwnerRepo
			}
			if !r.node.FetchOK {
				header += "  [" + unableFetchMarker + "]"
				// Surface the underlying error only when it carries info
				// beyond the synthetic marker — otherwise we'd just print
				// "unable to fetch (unable to fetch)".
				if r.node.FetchErr != "" && r.node.FetchErr != unableFetchMarker {
					header += " (" + r.node.FetchErr + ")"
				}
			}
			b.WriteString(fmt.Sprintf("%s%s%s %s %s\n", indent, cursor, glyph, box, header))
			continue
		}

		box := "☐"
		if m.checked[r.skill.Path] {
			box = "☑"
		}
		b.WriteString(fmt.Sprintf("%s%s%s %s\n", indent, cursor, box, r.skill.Name))
	}
	return b.String()
}

// Selection returns the paths the user kept checked, paired with the
// current global flag. Agents is left to the caller; the TUI doesn't
// touch agent selection in this version.
func (m Model) Selection() install.Selection {
	paths := make([]string, 0, len(m.checked))
	for path, ok := range m.checked {
		if ok {
			paths = append(paths, path)
		}
	}
	return install.Selection{SkillPaths: paths, Global: m.global}
}

// Run launches the bubbletea program on a fresh Model, blocks until quit,
// then casts the final model back to Model to extract the selection. The
// global flag is taken from the caller (cmd's --global) and re-applied
// to the returned Selection — this also covers the case where the user
// aborted, in which case the model carries the default false and we
// still honor the caller's flag.
func Run(cat *discover.Catalog, global bool) (install.Selection, error) {
	m := NewModel(cat)
	m.global = global
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return install.Selection{}, err
	}
	fm, ok := final.(Model)
	if !ok {
		return install.Selection{}, fmt.Errorf("tui: unexpected final model type %T", final)
	}
	sel := fm.Selection()
	sel.Global = global
	return sel, nil
}
