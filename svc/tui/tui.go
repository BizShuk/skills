// Package tui renders a discovered plugin.Catalog as an interactive
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

	"github.com/bizshuk/skills/model"
	"github.com/bizshuk/skills/svc/agent"
	"github.com/bizshuk/skills/svc/plugin"
)

// defaultViewportHeight is the number of body rows (headers + skills)
// the TUI shows at once. The actual value can be raised or lowered on
// the Model by tests or a future WindowSizeMsg handler.
const defaultViewportHeight = 20

// row is one visible line in the rendered tree. Headers and skills share
// a row shape so the cursor / scroll logic can treat them uniformly; the
// isHeader / skill fields disambiguate.
type row struct {
	node     *plugin.Category // non-nil for both header and skill rows
	skill    *model.Skill     // nil for header rows, non-nil for skill rows
	subagent *model.Subagent  // nil for header and skill rows, non-nil for subagent rows
	depth    int              // nesting depth (0 for top-level plugin headers)
	isHeader bool             // true for category header rows, false for skill rows
}

// phase values drive which screen the TUI renders.
const (
	phaseSkills = iota
	phaseAgents
	phaseLevel
)

// agentRow is one line in the agent-selection phase. When multiple agents
// share the same install location (ProjectSkillsDir + ProjectAgentsDir),
// they're collapsed into a single row so the user toggles them as one —
// checking the row installs into the shared directory for every member.
// Members of a singleton row are stored in a 1-element slice.
type agentRow struct {
	agents   []agent.Agent
	checked  bool
	detected bool // true if any member's DetectDir exists on disk
}

// displayName returns the human-readable label for this row. Singleton
// rows use the agent's DisplayName directly; multi-agent groups show the
// first member followed by the rest joined in parens, e.g.
// "Antigravity (Antigravity CLI, Codex, OpenCode)".
func (r agentRow) displayName() string {
	if len(r.agents) == 0 {
		return ""
	}
	if len(r.agents) == 1 {
		return r.agents[0].DisplayName
	}
	names := make([]string, len(r.agents))
	for i, a := range r.agents {
		names[i] = a.DisplayName
	}
	return names[0] + " (" + strings.Join(names[1:], ", ") + ")"
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
	// Skill-phase fields.
	cat    *plugin.Catalog
	rows   []row // post-filter visible rows; rebuildVisible() repopulates
	cursor int
	offset int

	viewportHeight int

	search      textinput.Model // inline search field
	searchQuery string          // cached lower-cased trimmed query

	global          bool
	done            bool
	folded          map[*plugin.Category]bool // fold state keyed by Category pointer
	checked         map[string]bool           // checked state keyed by Skill.Path
	checkedSubagent map[string]bool           // checked state keyed by Subagent.Path
	skillUnfolded   map[string]bool           // unfolded state keyed by Skill.Path

	// Agent-phase fields.
	phase       int        // phaseSkills, phaseAgents, or phaseLevel
	agents      []agentRow // agent list for phase 2
	agentCursor int
	agentOffset int

	// Level-phase fields (phase 3: Project vs Global install location).
	levelCursor int // 0 = Project row highlighted, 1 = Global row highlighted
}

// NewModel materializes the model's rows from the given catalog. Every
// skill starts unchecked — the user opts in with space. Failed plugins
// still contribute a header row (to surface the fetch failure) but no
// skill rows of their own.
//
// Fold state: every header starts folded (roots and nested sub-plugins).
// Right-arrow unfolds only the current header (one level); its children
// stay folded so a large marketplace (e.g. voltagent sub-plugins) does
// not dump every leaf at once. Left-arrow cascade-folds the whole
// subtree under the cursor.
//
// Search state: the search input starts focused and empty, so the user
// can immediately type to filter.
//
// Agent phase: agents are shown in phase 2 with detected agents pre-checked.
func NewModel(cat *plugin.Catalog, agents []agent.Agent) Model {
	m := Model{
		cat:             cat,
		folded:          map[*plugin.Category]bool{},
		checked:         map[string]bool{},
		checkedSubagent: map[string]bool{},
		skillUnfolded:   map[string]bool{},
		viewportHeight:  defaultViewportHeight,
		search:          textinput.New(),
		global:          true,
		phase:           phaseSkills,
		agents:          makeAgents(agents),
	}
	m.search.Prompt = ""
	m.search.Placeholder = ""
	m.search.Focus()
	// Pre-fold every node (roots + nested). Right-arrow only unfolds the
	// current header; children keep their folded entries so the user must
	// drill in level by level. rebuildVisible walks this map.
	var foldNested func(parent *plugin.Category)
	foldNested = func(parent *plugin.Category) {
		for _, ch := range parent.Children {
			m.folded[ch] = true
			foldNested(ch)
		}
	}
	for _, root := range cat.Roots {
		m.folded[root] = true
		foldNested(root)
	}
	m.rebuildVisible()
	m.ensureCursorVisible()
	return m
}

// makeAgents builds the agent row list, grouping agents that share the
// same install location so the user sees one checkbox per directory
// instead of one per agent type. detected marks whether any member's
// DetectDir exists on disk (used for the "(detected)" suffix); checked
// marks whether any member's DetectDir is detected on disk. Input order
// is preserved across groups; within a group, members keep the order they
// appeared in the input slice.
func makeAgents(agents []agent.Agent) []agentRow {
	detected := make(map[agent.AgentType]bool)
	for _, d := range agent.Detect() {
		detected[d.Type] = true
	}

	// Bucket agents by (skills dir, agents dir) while preserving the
	// input order across groups for deterministic rendering.
	groups := make(map[string][]agent.Agent)
	order := make([]string, 0, len(agents))
	for _, a := range agents {
		key := a.ProjectSkillsDir + "\x00" + a.ProjectAgentsDir
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], a)
	}

	rows := make([]agentRow, 0, len(order))
	for _, key := range order {
		members := groups[key]
		isDetected := false
		for _, a := range members {
			if detected[a.Type] {
				isDetected = true
				break
			}
		}
		rows = append(rows, agentRow{
			agents:   members,
			detected: isDetected,
			checked:  isDetected,
		})
	}
	return rows
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
	var walk func(c *plugin.Category, depth int)
	walk = func(c *plugin.Category, depth int) {
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

		// Also check subagents for direct matches (search visibility).
		subagentDirectMatch := q == ""
		if q != "" {
			for i := range c.Subagents {
				if subagentMatchesQuery(&c.Subagents[i], q) {
					subagentDirectMatch = true
					break
				}
			}
		}

		// Walk children first; remember where their rows start so we can
		// either splice our own rows in front or drop them entirely.
		//
		// Descend only when this node is expanded OR an active search
		// query needs to find matches in folded subtrees. Without this
		// gate, fold state hides only the node's own skill/subagent rows
		// but child header rows still leak through — a "folded" parent
		// would then look identical to an expanded one. With the gate, a
		// folded header truly hides its entire subtree (cascade), and
		// the user expands it via Right-arrow on the parent.
		childStart := len(out)
		if q != "" || !m.folded[c] {
			for _, ch := range c.Children {
				walk(ch, depth+1)
			}
		}
		childCount := len(out) - childStart

		include := q == "" || headerSelfMatch || skillDirectMatch || subagentDirectMatch || childCount > 0
		if !include {
			// Children contributed nothing meaningful (their subtrees were
			// also filtered out). Trim their rows from the accumulator.
			out = out[:childStart]
			return
		}

		// Build this node's self-rows: header (always), then skills (only
		// when expanded and matching).
		self := make([]row, 0, 1+len(c.Skills)+len(c.Subagents))
		self = append(self, row{node: c, depth: depth, isHeader: true})
		if !m.folded[c] {
			for i := range c.Skills {
				s := &c.Skills[i]
				if q == "" || skillMatchesQuery(s, q) {
					self = append(self, row{node: c, skill: s, depth: depth + 1})
				}
			}
			for i := range c.Subagents {
				sa := &c.Subagents[i]
				if q == "" || subagentMatchesQuery(sa, q) {
					self = append(self, row{node: c, subagent: sa, depth: depth + 1})
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
func skillMatchesQuery(s *model.Skill, q string) bool {
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

// subagentMatchesQuery reports whether the subagent's name OR description
// contains the lower-cased trimmed query. An empty query matches every
// subagent (caller should already short-circuit).
func subagentMatchesQuery(sa *model.Subagent, q string) bool {
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(sa.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(sa.Description), q) {
		return true
	}
	return false
}

// headerCheckState returns the aggregate check state for a category
// header: true if every skill in the subtree is checked, false if none
// are checked (or the subtree has no skills — empty → unchecked), and
// "partial" otherwise.
func (m Model) headerCheckState(c *plugin.Category) (all bool, partial bool) {
	var total, checked int
	var walk func(n *plugin.Category)
	walk = func(n *plugin.Category) {
		if n == nil {
			return
		}
		for _, s := range n.Skills {
			total++
			if m.checked[s.Path] {
				checked++
			}
		}
		for _, sa := range n.Subagents {
			total++
			if m.checkedSubagent[sa.Path] {
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
func (m *Model) toggleSubtree(c *plugin.Category) {
	if c == nil {
		return
	}
	// "Select all" when none-or-some are checked; "deselect all" only when
	// every descendant is currently on. This matches the conventional
	// checkbox behavior where space cyclically snaps a mixed state to its
	// deterministic anchor.
	all, _ := m.headerCheckState(c)
	target := !all
	var walk func(n *plugin.Category)
	walk = func(n *plugin.Category) {
		if n == nil {
			return
		}
		for _, s := range n.Skills {
			m.checked[s.Path] = target
		}
		for _, sa := range n.Subagents {
			m.checkedSubagent[sa.Path] = target
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

// Selection returns the paths the user kept checked, paired with the
// current global flag and the selected agent types.
func (m Model) Selection() agent.Selection {
	paths := make([]string, 0, len(m.checked))
	for path, ok := range m.checked {
		if ok {
			paths = append(paths, path)
		}
	}
	saPaths := make([]string, 0, len(m.checkedSubagent))
	for path, ok := range m.checkedSubagent {
		if ok {
			saPaths = append(saPaths, path)
		}
	}
	agentTypes := make([]agent.AgentType, 0)
	for _, row := range m.agents {
		if row.checked {
			for _, a := range row.agents {
				agentTypes = append(agentTypes, a.Type)
			}
		}
	}
	return agent.Selection{SkillPaths: paths, SubagentPaths: saPaths, AgentTypes: agentTypes, Global: m.global}
}

// Run launches the bubbletea program on a fresh Model, blocks until quit,
// then casts the final model back to Model to extract the selection. The
// global flag is taken from the caller (cmd's --global) only as the
// initial default for the level phase — the user can change it there via
// Space, and the final choice comes back on Selection().Global.
func Run(cat *plugin.Catalog, agents []agent.Agent, global bool) (agent.Selection, error) {
	m := NewModel(cat, agents)
	m.global = global
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return agent.Selection{}, err
	}
	fm, ok := final.(Model)
	if !ok {
		return agent.Selection{}, fmt.Errorf("tui: unexpected final model type %T", final)
	}
	return fm.Selection(), nil
}

// unfoldSubtree removes the fold entry for c only. Nested children keep
// their folded state so Right-arrow is a one-level drill-in: the user
// sees this header's skills/subagents and direct child headers, but not
// grandchildren until they expand those child headers too.
// (Name kept as unfoldSubtree for call-site stability; it no longer
// cascades — cascade was dumping every marketplace leaf on first open.)
func unfoldSubtree(c *plugin.Category, folded *map[*plugin.Category]bool) {
	if c == nil || folded == nil {
		return
	}
	delete(*folded, c)
}

// foldSubtree marks c and all its descendants as folded so collapsing a
// parent truly hides the whole branch (including any child the user had
// previously drilled into).
func foldSubtree(c *plugin.Category, folded *map[*plugin.Category]bool) {
	if c == nil || folded == nil {
		return
	}
	(*folded)[c] = true
	for _, ch := range c.Children {
		foldSubtree(ch, folded)
	}
}
