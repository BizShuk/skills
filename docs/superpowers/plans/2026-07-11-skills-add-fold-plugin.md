# Skills Add — Fold All Plugins By Default Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reverse the `skills add` TUI's default fold state so every root plugin — local and remote — starts folded (skills hidden) instead of mixed behavior.

**Architecture:** Single edit to `svc/tui/tui.go::NewModel`'s fold-init loop: drop the `if root.OwnerRepo != ""` guard so every root unconditionally enters `folded`, matching the existing `foldNested` policy for children. Drive the change via TDD: rename + flip assertions in the existing `TestRemoteRootPluginsAreFoldedByDefault` first, watch it fail, then fix `NewModel`, then add positive cascade tests.

**Tech Stack:** Go 1.x, bubbletea/lipgloss (already vendored via `go.mod`), `github.com/stretchr/testify` for assertions.

**Spec:** `docs/superpowers/specs/2026-07-11-skills-add-fold-plugin-design.md`

## Global Constraints

- Go: keep using `gofmt`-formatted style with the existing `model`, `agent`, `plugin`, `tui` package boundaries.
- Tests: drive by `github.com/stretchr/testify/{assert,require}`; reuse the `sendKey` + `mustModel` helpers from `svc/tui/tui_test.go`.
- No new dependencies.
- No header fold-icon affordance; no new key bindings.
- Commits follow the repo's `<scope>(<unit>): <verb> <object>` style. Use `feat(tui):` or `test(tui):` for the commits in this plan.
- All commits must end with:
  ```
  Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
  ```

---

### Task 1: Rewrite the default-fold test (watch it fail)

**Files:**
- Modify: `svc/tui/tui_test.go:635-657` (the `TestRemoteRootPluginsAreFoldedByDefault` block)
- Test: `svc/tui/tui_test.go` (same file; rename existing test)

**Interfaces:**
- Consumes: existing `plugin.Catalog{Roots: []*plugin.Category{...}}` fixture shape with `PluginName`, `OwnerRepo`, `Skills`, `Path`.
- Produces: a renamed test `TestAllRootPluginsAreFoldedByDefault` covering the new contract.

- [ ] **Step 1: Rename and flip the existing test**

In `svc/tui/tui_test.go`, replace the existing `TestRemoteRootPluginsAreFoldedByDefault` block (line 635–657, including its leading docstring) with:

```go
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
```

- [ ] **Step 2: Run the failing test**

Run: `go test ./svc/tui -run TestAllRootPluginsAreFoldedByDefault -v`
Expected: FAIL — output mentions `local-skill` is rendered, contradicting the assertion. Capture the exact failure message; it should read something like `AssertionError: local root plugin skill must NOT render since all roots now start folded`.

- [ ] **Step 3: Commit the failing test**

```bash
git add svc/tui/tui_test.go
git commit -m "test(tui): assert all root plugins start folded

Renames TestRemoteRootPluginsAreFoldedByDefault to
TestAllRootPluginsAreFoldedByDefault and flips the contract so both
local and remote roots are expected to start with skills hidden.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Make NewModel fold every root

**Files:**
- Modify: `svc/tui/tui.go:152-164` (the `NewModel` fold-init loop)

**Interfaces:**
- Consumes: `m.folded map[*plugin.Category]bool` already initialized in `NewModel` (line 137).
- Produces: every `cat.Roots[*]` is unconditionally in `m.folded` after `NewModel` returns; nested descendants are still folded by the unchanged `foldNested` closure.

- [ ] **Step 1: Replace the conditional with unconditional fold**

In `svc/tui/tui.go`, replace the loop at lines 159–164:

```go
for _, root := range cat.Roots {
    if root.OwnerRepo != "" {
        m.folded[root] = true
    }
    foldNested(root)
}
```

with:

```go
for _, root := range cat.Roots {
    // Every root starts folded regardless of OwnerRepo; nested
    // descendants are folded by foldNested below. Right-arrow on a
    // header remains the way the user expands a subtree (cascade).
    m.folded[root] = true
    foldNested(root)
}
```

- [ ] **Step 2: Run the renamed test — it should pass**

Run: `go test ./svc/tui -run TestAllRootPluginsAreFoldedByDefault -v`
Expected: PASS.

- [ ] **Step 3: Run the whole TUI package — confirm zero regressions**

Run: `go test ./svc/tui -v`
Expected: all TUI tests pass. Pay particular attention to:
- `TestRightArrowExpandsAndLeftFolds` (nested fold/unfold unchanged)
- `TestNewModelFoldsNestedSubPluginsByDefault` (nested behavior unchanged)
- `TestCascadeUnfold_ParentShowsAllDescendants` (cascade path unchanged)

- [ ] **Step 4: Commit the implementation**

```bash
git add svc/tui/tui.go
git commit -m "feat(tui): fold all root plugins by default in skills add

Reverses the root-level fold policy: previously remote roots
(OwnerRepo != \"\") started folded while local roots started expanded.
All roots are now folded on entry to NewModel, matching the existing
nested-children behavior. Right-arrow on any header still expands the
subtree via cascade, so users retain the same drill-in UX.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Add expansion test for local root

**Files:**
- Modify: append to `svc/tui/tui_test.go` (after the tests added in this plan, near `TestRemoteRootPluginsAreFoldedByDefault`)

**Interfaces:**
- Consumes: `mustModel(t, sendKey(m, tea.KeyType))` helper (line 144).
- Produces: a new test `TestLocalRootFoldsThenExpands` covering Right-arrow unlock behavior on a root with no `OwnerRepo`.

- [ ] **Step 1: Add the test**

Append to `svc/tui/tui_test.go`:

```go
// TestLocalRootFoldsThenExpands locks in the Right-arrow unlock behavior
// for a local root plugin (OwnerRepo == ""). Task 2's policy change
// folded local roots too; this test guards the user-visible escape hatch.
func TestLocalRootFoldsThenExpands(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "local-plugin",
				FetchOK:    true,
				Skills:     []model.Skill{{Name: "local-skill", Path: "/p/local"}},
			},
		},
	}
	m := NewModel(cat, nil)
	view0 := m.View()
	assert.NotContains(t, view0, "local-skill",
		"local root starts folded: skill hidden until expanded")

	// Cursor sits on row 0 (the only header). Right expands the subtree.
	expanded := mustModel(t, sendKey(m, tea.KeyRight))
	view1 := expanded.View()
	assert.Contains(t, view1, "local-skill",
		"Right on local root header must cascade-unfold and reveal the skill")

	// Left refolds. The skill row disappears again.
	reFolded := mustModel(t, sendKey(expanded, tea.KeyLeft))
	view2 := reFolded.View()
	assert.NotContains(t, view2, "local-skill",
		"Left on local root header must cascade-fold and re-hide the skill")
}
```

- [ ] **Step 2: Run the new test**

Run: `go test ./svc/tui -run TestLocalRootFoldsThenExpands -v`
Expected: PASS — this confirms Task 2's policy change is correctly coupled with the cascade unfold path.

- [ ] **Step 3: Commit**

```bash
git add svc/tui/tui_test.go
git commit -m "test(tui): cover right-arrow expand on local root

Pin the user-visible escape hatch now that all roots — local
included — start folded. The cascade unfold path is the same one
remote roots use; this verifies there is no separate code path that
would skip local roots.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Confirm batch toggle still works on a folded local root

**Files:**
- Modify: append to `svc/tui/tui_test.go`

**Interfaces:**
- Consumes: the same `sendKey` + `mustModel` helpers; the existing `TestSpaceOnCategoryHeaderChecksAllDescendants` (line 152) as the batch-toggle template.
- Produces: a new test verifying that Space on a folded local header still checks every descendant skill — fold state must not gate selection.

- [ ] **Step 1: Add the test**

Append to `svc/tui/tui_test.go`:

```go
// TestSpaceOnFoldedLocalHeaderChecksDescendants confirms that the fold
// state is purely a view concern: even when a local root starts folded
// (Task 2 policy), Space on the header still checks every descendant
// skill. Otherwise the new default fold would silently make local
// plugins un-selectable.
func TestSpaceOnFoldedLocalHeaderChecksDescendants(t *testing.T) {
	cat := &plugin.Catalog{
		Roots: []*plugin.Category{
			{
				PluginName: "local-plugin",
				FetchOK:    true,
				Skills: []model.Skill{
					{Name: "writer", Path: "/x/writer"},
					{Name: "reader", Path: "/x/reader"},
				},
			},
		},
	}
	m := NewModel(cat, nil)
	require.Equal(t, 0, m.cursor, "cursor must be on the folded local header")

	checked := mustModel(t, sendKey(m, tea.KeySpace))
	sel := checked.Selection().SkillPaths
	require.Len(t, sel, 2,
		"Space on a folded local root header must still check every descendant skill")
	assert.Contains(t, sel, "/x/writer")
	assert.Contains(t, sel, "/x/reader")
}
```

- [ ] **Step 2: Run the new test**

Run: `go test ./svc/tui -run TestSpaceOnFoldedLocalHeaderChecksDescendants -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add svc/tui/tui_test.go
git commit -m "test(tui): batch-select works on folded local root

Fold is a view-only state; selection must still flow through Space on
a header regardless of expand. Without this guard, the new default
fold would silently hide local plugins from the user without making
them un-selectable.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Full regression + smoke

**Files:** none.

- [ ] **Step 1: Run the full test suite with race detector**

Run: `go test -race ./...`
Expected: every package compiles and every test passes. In particular `svc/tui`, `svc/agent`, `svc/plugin` are the directly affected chains; `cmd` exercises end-to-end wiring.

- [ ] **Step 2: Build the binary**

Run: `go build -o bin/skills ./cmd/skills`
Expected: exit 0; `bin/skills` exists.

- [ ] **Step 3: Smoke test the TUI via `--yes` against a tiny local plugin path**

This bypasses the interactive keystroke loop and confirms the policy change did not break the headless pipeline. The path must resolve to a directory containing a `manifest.json` (or skill markdown) that `svc/discover` can resolve.

Run:
```bash
TMP=$(mktemp -d)
mkdir -p "$TMP/demo"
cat > "$TMP/demo/manifest.json" <<JSON
{ "plugin": "demo", "version": "0.0.1" }
JSON
"$TMP/demo/skills/writer/SKILL.md" 2>/dev/null
mkdir -p "$TMP/demo/skills/writer"
cat > "$TMP/demo/skills/writer/SKILL.md" <<MD
---
name: writer
description: writes things fluently
---
content
MD
./bin/skills add "$TMP/demo" --yes --agent claude-code --project
rm -rf "$TMP"
```
Expected: `skills add` reports writing `writer` to `.claude/skills`. Exit 0.

If `--yes` short-circuits the TUI entirely and exercises only `Selection()`, do not consider it a substitute for manual interaction — confirm only that pipeline wiring is intact.

- [ ] **Step 4: Final commit (only if step 3 leaves local changes staged)**

If `bin/skills` is in `.gitignore` (verify with `git status --ignored bin/skills`), step 2 leaves nothing to commit. Otherwise, add a one-line `.gitignore` entry:

```bash
echo "bin/" >> .gitignore
git add .gitignore
git commit -m "chore: ignore built bin/skills binary

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Acceptance Criteria

- `TestAllRootPluginsAreFoldedByDefault` passes; old `TestRemoteRootPluginsAreFoldedByDefault` is gone.
- `TestLocalRootFoldsThenExpands` passes.
- `TestSpaceOnFoldedLocalHeaderChecksDescendants` passes.
- All pre-existing TUI tests still pass with `-race`.
- `svc/tui/tui.go::NewModel` reads with no `if root.OwnerRepo != ""` guard.
- No new dependencies, no new exported symbols, no new public API in `cmd/`.
