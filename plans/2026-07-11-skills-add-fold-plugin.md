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

    ```text
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

### Task 3: Add per-root expansion test covering local + remote

**Files:**

- Modify: append to `svc/tui/tui_test.go` (near `TestRemoteRootPluginsAreFoldedByDefault`'s former location)

**Interfaces:**

- Consumes: `mustModel(t, sendKey(m, tea.KeyType))` helper (line 144).
- Produces: a new test `TestAllRootsFolded_BothRequireExpansion` covering three scenarios on a single fixture (local + remote root): initial hidden → expand one → expand other → expand both.

- [ ] **Step 1: Add the test**

Append to `svc/tui/tui_test.go`:

```go
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
```

- [ ] **Step 2: Run the new test**

Run: `go test ./svc/tui -run TestAllRootsFolded_BothRequireExpansion -v`
Expected: PASS — confirms Task 2's policy change is correctly coupled with the per-root cascade expand path.

- [ ] **Step 3: Commit**

```bash
git add svc/tui/tui_test.go
git commit -m "test(tui): cover per-root expansion on local + remote

Pins the homogeneous root fold policy: a fixture with one local and
one remote root starts with both skills hidden, expanding either one
individually surfaces only its own skill, and expanding both surfaces
both. Regression guard for 2026-07-11-skills-add-fold-plugin.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Confirm cascade path symmetry between local and remote roots

**Files:**

- Modify: append to `svc/tui/tui_test.go`

**Interfaces:**

- Consumes: same `sendKey` / `mustModel` helpers; mirrors the existing `TestCascadeUnfold_ParentShowsAllDescendants` (line 813) but with one local and one remote root instead of a nested remote chain.
- Produces: a new test confirming Right-arrow produces the same `len(rows)` and same skill visibility on a local root as on a remote root — i.e. the local root runs through the same cascade code path, not a separate shortcut.

- [ ] **Step 1: Add the test**

Append to `svc/tui/tui_test.go`:

```go
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
```

- [ ] **Step 2: Run the new test**

Run: `go test ./svc/tui -run TestCascadeUnfold_LocalAndRemoteRootsSymmetric -v`
Expected: PASS — both `len(rows)` values match (1 → 2) and both `View()` outputs contain their own skill name, proving the local root shares the cascade path with the remote root.

- [ ] **Step 3: Commit**

```bash
git add svc/tui/tui_test.go
git commit -m "test(tui): local and remote roots share the cascade path

Mirrors TestCascadeUnfold_ParentShowsAllDescendants but with a flat
local-vs-remote pair so any future refactor that introduces a
local-only shortcut (or skips the cascade helpers for any root) is
caught before it ships.

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
mkdir -p "$TMP/demo/skills/writer"
cat > "$TMP/demo/manifest.json" <<JSON
{ "plugin": "demo", "version": "0.0.1" }
JSON
cat > "$TMP/demo/skills/writer/SKILL.md" <<MD
---
name: writer
description: writes things fluently
---
content
MD
./bin/skills add "$TMP/demo" --yes --agent claude-code --project
RC=$?
rm -rf "$TMP"
exit $RC
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

### Task 6: Hide nested children when parent root is folded (regression)

**Files:**
- Modify: `svc/tui/tui.go` (the `walk` closure inside `rebuildVisible`, ~line 258)
- Modify: `svc/tui/tui_test.go` (3 existing tests renamed/updated, 1 new test added)
- Modify: `docs/superpowers/specs/2026-07-11-skills-add-fold-plugin-design.md` (lines 33-40, plus a new amendment block at the end of `互動`)

**Interfaces:**
- Consumes: existing `plugin.Catalog` + `plugin.Category{Children}` shapes; `m.folded` map already initialized in `NewModel`.
- Produces: when a node is folded AND there's no active search query, `walk` no longer descends into `c.Children` — the folded header truly hides its subtree.

**Context.** Tasks 1-5 made `NewModel` mark every root as folded. Visual smoke testing caught a follow-up bug: a remote root whose `marketplace.json` declares ~10 sub-plugins (e.g. `voltagent/awesome-claude-code-subagents`) still rendered every child header in the initial view, because `walk` descended into `c.Children` unconditionally. `m.folded[c]` only gated the node's own skill/subagent rows; child header rows leaked through, so a "folded" root looked identical to an expanded one.

- [ ] **Step 1: Gate child walk on fold state**

In `svc/tui/tui.go`, inside `rebuildVisible`'s `walk` closure, change:

```go
childStart := len(out)
for _, ch := range c.Children {
    walk(ch, depth+1)
}
childCount := len(out) - childStart
```

to:

```go
childStart := len(out)
// Descend only when this node is expanded OR an active search query
// needs to find matches in folded subtrees. Without this gate, fold
// state hides only the node's own skill/subagent rows but child header
// rows still leak through — a "folded" parent would then look identical
// to an expanded one.
if q != "" || !m.folded[c] {
    for _, ch := range c.Children {
        walk(ch, depth+1)
    }
}
childCount := len(out) - childStart
```

- [ ] **Step 2: Rename `TestNewModelFoldsNestedSubPluginsByDefault` → `TestNewModelHidesNestedChildrenWhenParentFolded`**

The old assertion ("`inner` is visible so user can drill in") contradicts the new contract. Replace the entire test body with the version that asserts `inner` is NOT visible and `len(m.rows) == 1`.

- [ ] **Step 3: Update `TestRightArrowExpandsAndLeftFolds` flow**

Old flow asserted 2 rows initially (outer header + inner header). After the gate, the initial state has only the outer header. Replace the entire test body so initial state is 1 row, Right on outer yields 3 rows (cascade unfold), Left on outer yields 1 row (cascade fold).

- [ ] **Step 4: Update `TestLayer2_FoldToggle` flow**

Old flow assumed layer-2 is reachable after one Down. After the gate, layer-2 is hidden until layer-1 cascades open. Replace the entire test body so it Right-arrows on cc-plugin first to reach layer-2, then tests selective fold of layer-2 (cc-plugin stays expanded, layer-2 subtree hides).

- [ ] **Step 5: Add `TestRemoteMarketplaceWithManyChildren_AllHiddenByDefault`**

Direct regression test for the user-reported screenshot. Mirrors the user's exact catalog (8 sibling roots + 1 remote root with 10 children). Asserts initial `len(m.rows) == 10`, asserts all 10 nested children + `python-pro` are absent from the view, then Right-arrows the remote root and asserts cascade unfold exposes `voltagent-lang` + `python-pro`.

- [ ] **Step 6: Amend the design spec**

In `docs/superpowers/specs/2026-07-11-skills-add-fold-plugin-design.md`:
1. Replace the `互動` table rows for `↓ / ↑` and `鍵入搜尋文字` so the docs match the new behavior (folded parent hides children entirely; search still descends).
2. Append a "修正 (Amendment, 2026-07-11)" block at the end of `互動` summarizing: root cause, the `walk` gate, the four test updates, the spec amendment.

- [ ] **Step 7: Run the full TUI package + race**

Run: `go test -race ./svc/tui/`
Expected: every test passes, including:
- `TestNewModelHidesNestedChildrenWhenParentFolded` (renamed)
- `TestRightArrowExpandsAndLeftFolds` (updated)
- `TestLayer2_FoldToggle` (updated)
- `TestRemoteMarketplaceWithManyChildren_AllHiddenByDefault` (new)
- `TestCascadeUnfold_ParentShowsAllDescendants` / `TestCascadeFold_ParentHidesAllDescendants` / `TestRight_TogglesFoldOnParent` (unchanged — they all start with Right-arrow so they go through the post-fold path)

- [ ] **Step 8: Commit**

```bash
git add svc/tui/tui.go svc/tui/tui_test.go docs/superpowers/specs/2026-07-11-skills-add-fold-plugin-design.md
git commit -m "fix(tui): gate child walk on fold state so nested children hide

User reported voltagent/awesome-claude-code-subagents (a remote root
with ~10 sub-plugin children) showed all child headers in the initial
TUI view despite being folded. Root cause: rebuildVisible's walk()
descended into c.Children unconditionally, so child header rows leaked
through even when m.folded[c] was true. Fold state only gated the
node's own skill/subagent rows, not its children.

Fix: skip the child walk when the parent is folded AND there's no
active search query (search still descends to find matches in folded
subtrees). A folded root now truly hides its entire subtree; the user
expands it via Right-arrow cascade.

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

### Task 6 regression acceptance (2026-07-11)

- `TestNewModelHidesNestedChildrenWhenParentFolded` passes (replaces `TestNewModelFoldsNestedSubPluginsByDefault`); initial view shows 1 row, no `inner` / `helper` visible.
- `TestRightArrowExpandsAndLeftFolds` updated flow passes: initial 1 → Right 3 → Left 1.
- `TestLayer2_FoldToggle` updated flow passes: Right-on-cc-plugin cascades open, then Right-on-layer-2 selectively folds layer-2's subtree.
- `TestRemoteMarketplaceWithManyChildren_AllHiddenByDefault` (new) passes: 10-root fixture, initial `len(m.rows) == 10`, no nested children visible, Right on remote cascades to expose `voltagent-lang` + `python-pro`.
- `svc/tui/tui.go::walk` only descends into `c.Children` when `q != "" || !m.folded[c]`.
- Spec amendment present in `docs/superpowers/specs/2026-07-11-skills-add-fold-plugin-design.md`.
