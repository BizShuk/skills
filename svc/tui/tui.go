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
// The visible tree is filtered by the search input (case-insensitive
// substring match across plugin names, skill names and skill
// descriptions) and clipped to a viewport of viewportHeight rows. The
// filter only changes visibility — selection state for every skill is
// independent of the search query.
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

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/skills/svc/discover"
	"github.com/bizshuk/skills/svc/install"
)

// defaultViewportHeight is the number of body rows (headers + skills)
// the TUI shows at once. The actual value can be raised or lowered on
// the Model by tests or a future WindowSizeMsg handler.
const defaultViewportHeight = 20

// row is one visible line in the rendered tree. Headers and skills share
// a row shape so the cursor / scroll logic can treat them uniformly; the
// isHeader / skill fields disambiguate.
type row struct {
	node     *discover.Category // non-nil for both header and skill rows
	skill    *discover.Skill    // nil for header rows, non-nil for skill rows
	depth    int                // nesting depth (0 for top-level plugin headers)
	isHeader bool               // true for category header rows, false for skill rows
}

// Model is the bubbletea program state. Cursor, rows, fold map, search
// input and per-skill checked map are kept on the value receiver because
// bubbletea treats the model as immutable; mutations inside Update
// assign back to a local copy and return it.
//
// viewportHeight overrides the row count shown per frame; tests typically
// shrink it to exercise the "↓ N more" footer without rendering a giant
// catalog. Zero means "use defaultViewportHeight".
type Model struct {
	cat    *discover.Catalog
	rows   []row // post-filter visible rows; rebuildVisible() repopulates
	cursor int
	offset int

	viewportHeight int

	search      textinput.Model // inline search field
	searchQuery string          // cached lower-cased trimmed query

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
//
// Search state: the search input starts focused and empty, so the user
// can immediately type to filter.
func NewModel(cat *discover.Catalog) Model {
	m := Model{
		cat:            cat,
		folded:         map[*discover.Category]bool{},
		checked:        map[string]bool{},
		viewportHeight: defaultViewportHeight,
		search:         textinput.New(),
	}
	m.search.Prompt = ""
	m.search.Placeholder = ""
	m.search.Focus()
	for _, s := range cat.AllSkills() {
		m.checked[s.Path] = true
	}
	// Pre-fold every nested sub-plugin. Roots stay expanded. We do this in
	// a second pass after checked is populated so that rebuildVisible sees
	// a stable fold state when it walks the tree.
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
	m.rebuildVisible()
	m.ensureCursorVisible()
	return m
}

// rebuildVisible walks the catalog honoring both fold state and the
// current search query, repopulating m.rows. Each category header is
// included when its own name matches the query OR any descendant skill
// (by name or description) matches. Skill rows are included when they
// match directly. With an empty query every row passes through (subject
// only to fold state).
//
// The walk is recursive and orders rows pre-order (parent header, then
// parent's skills, then child's subtree), matching the previous
// rebuildRows semantics. To honor that order while still letting a
// parent decide whether to keep its child's contribution, we splice
// the parent's header+skills in front of any rows its children
// produced at the same level.
func (m *Model) rebuildVisible() {
	q := strings.ToLower(strings.TrimSpace(m.searchQuery))

	var out []row
	var walk func(c *discover.Category, depth int)
	walk = func(c *discover.Category, depth int) {
		if c == nil {
			return
		}

		headerSelfMatch := q == "" ||
			strings.Contains(strings.ToLower(c.PluginName), q) ||
			(c.OwnerRepo != "" && strings.Contains(strings.ToLower(c.OwnerRepo), q))

		// Find direct skill matches (regardless of fold state — even a
		// folded sub-plugin's skills still need to be searched so the
		// user can type to discover hidden skills).
		skillDirectMatch := q == ""
		if q != "" {
			for i := range c.Skills {
				if skillMatchesQuery(&c.Skills[i], q) {
					skillDirectMatch = true
					break
				}
			}
		}

		// Walk children first; remember where their rows start so we can
		// either splice our own rows in front or drop them entirely.
		childStart := len(out)
		for _, ch := range c.Children {
			walk(ch, depth+1)
		}
		childCount := len(out) - childStart

		include := q == "" || headerSelfMatch || skillDirectMatch || childCount > 0
		if !include {
			// Children contributed nothing meaningful (their subtrees were
			// also filtered out). Trim their rows from the accumulator.
			out = out[:childStart]
			return
		}

		// Build this node's self-rows: header (always), then skills (only
		// when expanded and matching).
		self := make([]row, 0, 1+len(c.Skills))
		self = append(self, row{node: c, depth: depth, isHeader: true})
		if !m.folded[c] {
			for i := range c.Skills {
				s := &c.Skills[i]
				if q == "" || skillMatchesQuery(s, q) {
					self = append(self, row{node: c, skill: s, depth: depth + 1})
				}
			}
		}

		// Splice self in front of the children's rows: out =
		// out[:childStart] + self + out[childStart:].
		merged := make([]row, 0, len(out)+len(self))
		merged = append(merged, out[:childStart]...)
		merged = append(merged, self...)
		merged = append(merged, out[childStart:]...)
		out = merged
	}

	for _, root := range m.cat.Roots {
		walk(root, 0)
	}
	m.rows = out

	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// skillMatchesQuery reports whether the skill's name OR description
// contains the lower-cased trimmed query. An empty query matches every
// skill (caller should already short-circuit).
func skillMatchesQuery(s *discover.Skill, q string) bool {
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(s.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(s.Description), q) {
		return true
	}
	return false
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

// findParentHeader returns the closest ancestor header row for r at idx.
// If r is itself a header it returns idx; otherwise it walks backward
// looking for the previous header row.
func (m Model) findParentHeader(idx int) int {
	for i := idx; i >= 0; i-- {
		if m.rows[i].isHeader {
			return i
		}
	}
	return 0
}

// ensureCursorVisible advances m.offset so that m.cursor falls inside
// the [offset, offset+viewportHeight) window. Called whenever the cursor
// moves or the visible-row set changes.
func (m *Model) ensureCursorVisible() {
	h := m.viewportHeight
	if h <= 0 {
		h = defaultViewportHeight
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	maxOffset := len(m.rows) - h
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// Init satisfies the bubbletea Model interface; we have no startup Cmd.
// The search field is already focused in NewModel so there is nothing
// for Init to kick off.
func (m Model) Init() tea.Cmd { return nil }

// Update dispatches key presses to cursor movement, toggle, fold/unfold,
// search filtering, and quit actions. Non-key messages are ignored. The
// returned Model carries the new cursor / checked / fold / search state;
// the returned Cmd is nil except on enter (tea.Quit) and ctrl-c (also
// tea.Quit) or esc-when-search-empty (also tea.Quit).
//
// Esc is overloaded: when the search field is non-empty it clears the
// search instead of quitting, so the user can iteratively narrow; with
// an empty field the second esc quits, matching the conventional
// pattern.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.Type {
	case tea.KeyEsc:
		if m.search.Value() != "" {
			m.search.SetValue("")
			m.searchQuery = ""
			m.rebuildVisible()
			m.cursor = 0
			m.offset = 0
			m.ensureCursorVisible()
			return m, nil
		}
		m.done = true
		return m, tea.Quit
	case tea.KeyEnter, tea.KeyCtrlC:
		m.done = true
		return m, tea.Quit
	}

	// Navigation keys are NOT fed to the search input (otherwise Up/Down
	// would move within the text rather than navigate the tree, and Space
	// would insert a space rather than toggle). Everything else
	// (printable runes, Backspace, Delete, Arrow keys for cursor within
	// the field, etc) goes to the input first.
	switch key.Type {
	case tea.KeyUp, tea.KeyDown, tea.KeySpace, tea.KeyLeft, tea.KeyRight:
		// fall through to navigation handler below
	default:
		prev := m.search.Value()
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		if m.search.Value() != prev {
			m.searchQuery = m.search.Value()
			m.rebuildVisible()
			m.cursor = 0
			m.offset = 0
			m.ensureCursorVisible()
			return m, cmd
		}
		return m, nil
	}

	if len(m.rows) == 0 {
		// Navigation key on an empty list: no-op (still allow Up/Down to
		// keep the cursor pinned at 0).
		switch key.Type {
		case tea.KeyUp, tea.KeyDown:
			// no-op
		}
		return m, nil
	}

	switch key.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			m.ensureCursorVisible()
		}
	case tea.KeyDown:
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			m.ensureCursorVisible()
		}
	case tea.KeySpace:
		r := m.rows[m.cursor]
		if r.isHeader {
			m.toggleSubtree(r.node)
		} else if r.skill != nil {
			m.checked[r.skill.Path] = !m.checked[r.skill.Path]
		}
	case tea.KeyRight:
		r := m.rows[m.cursor]
		if r.isHeader {
			if m.folded[r.node] {
				delete(m.folded, r.node)
				m.rebuildVisible()
				m.ensureCursorVisible()
			}
		} else {
			m.cursor = m.findParentHeader(m.cursor)
			m.ensureCursorVisible()
		}
	case tea.KeyLeft:
		r := m.rows[m.cursor]
		if r.isHeader {
			if !m.folded[r.node] {
				m.folded[r.node] = true
				m.rebuildVisible()
				m.ensureCursorVisible()
			}
		} else {
			m.cursor = m.findParentHeader(m.cursor)
			m.ensureCursorVisible()
		}
	}
	return m, nil
}

// unableFetchMarker is the literal text we look for in tests; keep it
// spelled exactly like this so the tests don't get fragile.
const unableFetchMarker = "unable to fetch"

// checkbox glyphs for header aggregates and individual skills.
const (
	glyphUnchecked     = "○"
	glyphChecked       = "●"
	glyphIndeterminate = "▣"
)

// View renders the tree. The header order matches the spec:
//
//	Select skills to install
//	Search: <input>
//	↑↓ move, space select, enter confirm
//	(blank)
//	<body rows, clipped to viewportHeight, plus "↓ N more" if off-screen>
//
// Each header is `<indent>> <box> <pluginName>` (or `> …` if cursor).
// Each skill row is `<indent>> <box> <name> (<description>)`, with the
// description in parens (empty parens when none). Categories that failed
// to fetch are suffixed with `  [unable to fetch]` plus the underlying
// error when it carries info beyond the marker.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString("Select skills to install\n")
	b.WriteString("Search: ")
	b.WriteString(m.search.View())
	b.WriteString("\n")
	b.WriteString("↑↓ move, space select, enter confirm\n")

	if len(m.rows) == 0 {
		// Treat "no matches" and "empty catalog" both as a one-line hint,
		// so callers don't have to special-case zero rows.
		b.WriteString("\n")
		b.WriteString("(no matching skills)\n")
		return b.String()
	}

	b.WriteString("\n") // spacer between key hints and the tree

	h := m.viewportHeight
	if h <= 0 {
		h = defaultViewportHeight
	}
	start := m.offset
	end := start + h
	if end > len(m.rows) {
		end = len(m.rows)
	}

	for i := start; i < end; i++ {
		r := m.rows[i]
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		// Indent: two spaces per depth level for both headers and skills.
		indent := strings.Repeat("  ", r.depth)

		if r.isHeader {
			box := glyphUnchecked
			all, partial := m.headerCheckState(r.node)
			switch {
			case partial:
				box = glyphIndeterminate
			case all:
				box = glyphChecked
			}
			text := r.node.PluginName
			if r.node.OwnerRepo != "" {
				text += "  " + r.node.OwnerRepo
			}
			if !r.node.FetchOK {
				text += "  [" + unableFetchMarker + "]"
				if r.node.FetchErr != "" && r.node.FetchErr != unableFetchMarker {
					text += " (" + r.node.FetchErr + ")"
				}
			}
			b.WriteString(fmt.Sprintf("%s%s%s %s\n", indent, cursor, box, text))
			continue
		}

		box := glyphUnchecked
		if m.checked[r.skill.Path] {
			box = glyphChecked
		}
		// Description in parens; the `()` placeholder matches the spec's
		// visual rhythm when a skill has no description (rare in practice
		// but allowed).
		desc := "(" + r.skill.Description + ")"
		b.WriteString(fmt.Sprintf("%s%s%s %s %s\n", indent, cursor, box, r.skill.Name, desc))
	}

	remaining := len(m.rows) - end
	if remaining > 0 {
		b.WriteString(fmt.Sprintf("↓ %d more\n", remaining))
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
