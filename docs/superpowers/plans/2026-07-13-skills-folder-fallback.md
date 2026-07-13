# Skills-folder Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `svc/plugin/manifest.go::Scan()` emit a synthetic `LocalPlugin` from `<base>/skills/` (or `.claude/skills/`, `.agents/skills/`) when **no** manifest file exists on disk, so repos without `.claude-plugin/marketplace.json` / `.claude-plugin/plugin.json` / `skill.json` are still discoverable.

**Architecture:** Add three small helpers in `svc/plugin/manifest.go` (`hasAnyManifest`, `hasAnyConventionalSkillsDir`, `isInsideAgentDir`) and a single fallback branch at the end of `Scan()` that synthesizes a `LocalPlugin` whose `Name` is `filepath.Base(base)` (or `"root"`) and runs `scanSkills` on it. No change to `utils/walk.go` — the BFS is already protected by the "non-empty `Locals`" rule, and mid-BFS sub-dirs that are not remote repo roots are themselves declared by a manifest (A4/A5) and produce a non-empty `Locals` for the fallback to short-circuit on.

**Tech Stack:** Go (≥1.21, see `go.mod`), `github.com/stretchr/testify` for assertions, project test runner `go test ./svc/plugin/...`.

## Global Constraints

- Module path is `github.com/bizshuk/skills` (per repo `CLAUDE.md`).
- All new code lives in `svc/plugin/`; no new packages.
- Conventional skills directories: `skills/`, `.claude/skills/`, `.agents/skills/` — only these three, only the top-level.
- `name` of synthetic plugin = `filepath.Base(base)`, with `"root"` for `.` / `/`.
- `Scan()` is called on every BFS level; the fallback must be safe to run on any `base` (root or sub-dir). The natural safety is `!hasAnyManifest(base) && Locals still empty` — sub-dirs reached via A4/A5 already populate `Locals`; remote repo roots have no `skills/` at the top.
- A file that exists but JSON-parses-failure still counts as "manifest present" (so the fallback does **not** run) — we do not mask real parse bugs.
- A path-segment match for `agents/`: the `agents` segment must NOT be the final segment of `base` (so a repo whose root is literally named `agents` is still a valid fallback target). `.claude/agents/...` and `.agents/agents/...` are also excluded regardless of position.
- Path traversal guards in `scanSkills` (`isContainedIn`) and the silent-ignore-on-bad-parse behavior are unchanged.
- Commit style: `type(scope): subject` (e.g. `feat(plugin): ...`, `test(plugin): ...`).

---

## File Structure

- **Modify** `svc/plugin/manifest.go`
  - Add `hasAnyManifest(base string) bool`
  - Add `hasAnyConventionalSkillsDir(base string) bool`
  - Add `isInsideAgentDir(base string) bool`
  - Insert fallback branch at end of `Scan()` (before `dedupeLocalsByBase`)
- **Modify** `svc/plugin/manifest_test.go`
  - Append ten new test cases E1–E10 (see Test Matrix in spec)

No other files are touched. No new packages, no `go.mod` changes, no `walk.go` changes.

---

## Task 1: `hasAnyManifest` helper + test

**Files:**
- Modify: `svc/plugin/manifest.go` (append helper, just below `dedupeLocalsByBase`)
- Modify: `svc/plugin/manifest_test.go` (append test at the end of the file)

**Interfaces:**
- Produces: `func hasAnyManifest(base string) bool` — returns `true` if ANY of `<base>/.claude-plugin/marketplace.json`, `<base>/.claude-plugin/plugin.json`, `<base>/skill.json` is reachable as a file (`os.Stat` returns no error or any error other than `os.IsNotExist`).

- [ ] **Step 1: Write the failing test**

Append to `svc/plugin/manifest_test.go`:

```go
// TestHasAnyManifest verifies the helper that gates the no-manifest
// fallback in Scan(): true if any of marketplace.json / plugin.json /
// skill.json is reachable; false when all are missing.
func TestHasAnyManifest(t *testing.T) {
	t.Run("none present", func(t *testing.T) {
		base := t.TempDir()
		assert.False(t, hasAnyManifest(base))
	})

	t.Run("marketplace only", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude-plugin"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(base, ".claude-plugin", "marketplace.json"), []byte("{}"), 0o644))
		assert.True(t, hasAnyManifest(base))
	})

	t.Run("plugin only", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude-plugin"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(base, ".claude-plugin", "plugin.json"), []byte("{}"), 0o644))
		assert.True(t, hasAnyManifest(base))
	})

	t.Run("skill only", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(base, "skill.json"), []byte("{}"), 0o644))
		assert.True(t, hasAnyManifest(base))
	})

	t.Run("malformed json still counts as present", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude-plugin"), 0o755))
		// Intentionally invalid JSON — should still be treated as "manifest exists"
		// so the fallback does not silently mask a real parse bug.
		require.NoError(t, os.WriteFile(filepath.Join(base, ".claude-plugin", "plugin.json"), []byte("not json"), 0o644))
		assert.True(t, hasAnyManifest(base))
	})
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./svc/plugin/... -run TestHasAnyManifest -v`
Expected: compilation error `hasAnyManifest undefined` (or test failure if the file already compiles but the symbol is missing).

- [ ] **Step 3: Implement the helper**

Append to `svc/plugin/manifest.go`, immediately after `dedupeLocalsByBase` (or at end of file before the trailing blank line — any clean spot):

```go
// hasAnyManifest reports whether ANY of the three manifest paths is reachable
// as a file under base. "Reachable" means os.Stat returned no error or any
// error other than os.IsNotExist (e.g. permission denied). A parse error on
// an existing-but-malformed file still counts as "exists" — we want the
// existing silent-ignore path to surface (or not) unchanged, and we do not
// want a transient permission glitch to flip a repo into fallback mode and
// emit a synthetic empty plugin.
func hasAnyManifest(base string) bool {
	paths := []string{
		filepath.Join(base, ".claude-plugin", "marketplace.json"),
		filepath.Join(base, ".claude-plugin", "plugin.json"),
		filepath.Join(base, "skill.json"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		} else if !os.IsNotExist(err) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./svc/plugin/... -run TestHasAnyManifest -v`
Expected: PASS, all 5 subtests.

- [ ] **Step 5: Commit**

```bash
git add svc/plugin/manifest.go svc/plugin/manifest_test.go
git commit -m "feat(plugin): add hasAnyManifest helper for no-manifest fallback"
```

---

## Task 2: `hasAnyConventionalSkillsDir` helper + test

**Files:**
- Modify: `svc/plugin/manifest.go` (append helper)
- Modify: `svc/plugin/manifest_test.go` (append test)

**Interfaces:**
- Produces: `func hasAnyConventionalSkillsDir(base string) bool` — returns `true` if any of `<base>/skills/`, `<base>/.claude/skills/`, `<base>/.agents/skills/` is a directory on disk.

- [ ] **Step 1: Write the failing test**

Append to `svc/plugin/manifest_test.go`:

```go
// TestHasAnyConventionalSkillsDir verifies that the helper recognizes the
// three conventional skills directories (skills/, .claude/skills/,
// .agents/skills/) and ignores a file with the same name.
func TestHasAnyConventionalSkillsDir(t *testing.T) {
	t.Run("none present", func(t *testing.T) {
		base := t.TempDir()
		assert.False(t, hasAnyConventionalSkillsDir(base))
	})

	t.Run("skills only", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, "skills"), 0o755))
		assert.True(t, hasAnyConventionalSkillsDir(base))
	})

	t.Run("dotclaude skills only", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude", "skills"), 0o755))
		assert.True(t, hasAnyConventionalSkillsDir(base))
	})

	t.Run("dotagents skills only", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, ".agents", "skills"), 0o755))
		assert.True(t, hasAnyConventionalSkillsDir(base))
	})

	t.Run("file with the name skills is not a directory", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(base, "skills"), []byte("x"), 0o644))
		assert.False(t, hasAnyConventionalSkillsDir(base))
	})
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./svc/plugin/... -run TestHasAnyConventionalSkillsDir -v`
Expected: compilation error `hasAnyConventionalSkillsDir undefined`.

- [ ] **Step 3: Implement the helper**

Append to `svc/plugin/manifest.go`:

```go
// hasAnyConventionalSkillsDir reports whether base contains at least one
// of the three conventional top-level skills directories. A file (not a
// directory) at any of the three paths does not count.
func hasAnyConventionalSkillsDir(base string) bool {
	paths := []string{
		filepath.Join(base, "skills"),
		filepath.Join(base, ".claude", "skills"),
		filepath.Join(base, ".agents", "skills"),
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./svc/plugin/... -run TestHasAnyConventionalSkillsDir -v`
Expected: PASS, all 5 subtests.

- [ ] **Step 5: Commit**

```bash
git add svc/plugin/manifest.go svc/plugin/manifest_test.go
git commit -m "feat(plugin): add hasAnyConventionalSkillsDir helper"
```

---

## Task 3: `isInsideAgentDir` helper + test

**Files:**
- Modify: `svc/plugin/manifest.go` (append helper)
- Modify: `svc/plugin/manifest_test.go` (append test)

**Interfaces:**
- Produces: `func isInsideAgentDir(base string) bool` — returns `true` if `base` sits inside a conventional agents directory. Specifically:
  - any `agents` path segment that is **not** the final segment of `base`, OR
  - a `.claude` or `.agents` segment immediately followed by `agents` (regardless of position).
  - Segment match only — partial names (e.g. `agents-keeper`) do not match.

- [ ] **Step 1: Write the failing test**

Append to `svc/plugin/manifest_test.go`:

```go
// TestIsInsideAgentDir verifies the helper that protects the fallback from
// mistaking a conventional agents/ layout (subagent definitions) for a
// plugin. A repo whose ROOT is literally named "agents" is still a valid
// fallback target — only nested agents/ directories count.
func TestIsInsideAgentDir(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"plain root", "/repo/project", false},
		{"repo root literally named agents", "/agents", false},
		{"agents as last segment with trailing slash", "/agents/", false},
		{"nested agents subdir", "/repo/project/agents", true},
		{"nested agents subdir with file", "/repo/project/agents/foo.md", true},
		{"dotclaude agents subdir", "/repo/project/.claude/agents", true},
		{"dotclaude agents subdir with file", "/repo/project/.claude/agents/foo.md", true},
		{"dotagents agents subdir", "/repo/project/.agents/agents", true},
		{"partial name agents-keeper", "/repo/agents-keeper", false},
		{"partial name myagents", "/repo/myagents", false},
		{"agents hidden inside unrelated name", "/repo/data/agents-export/x", false},
		{"relative plain", "plugins/foo", false},
		{"relative inside agents", "plugins/agents/foo", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isInsideAgentDir(tc.path))
		})
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./svc/plugin/... -run TestIsInsideAgentDir -v`
Expected: compilation error `isInsideAgentDir undefined`.

- [ ] **Step 3: Implement the helper**

Append to `svc/plugin/manifest.go`:

```go
// isInsideAgentDir reports whether base sits inside a conventional agents/
// directory. "Inside" means a path segment equal to "agents" that is NOT
// the final segment of base, or a ".claude"/".agents" segment immediately
// followed by "agents". A repo whose root folder is literally named
// "agents" is a valid fallback target — only nested agents/ directories
// are excluded. Path-segment match only — partial names (e.g.
// "agents-keeper") do not match.
func isInsideAgentDir(base string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(base)), "/")
	for i, seg := range parts {
		if seg == "agents" && i < len(parts)-1 {
			return true
		}
		if (seg == ".claude" || seg == ".agents") &&
			i+1 < len(parts) && parts[i+1] == "agents" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./svc/plugin/... -run TestIsInsideAgentDir -v`
Expected: PASS, all 13 subtests.

- [ ] **Step 5: Commit**

```bash
git add svc/plugin/manifest.go svc/plugin/manifest_test.go
git commit -m "feat(plugin): add isInsideAgentDir helper"
```

---

## Task 4: Wire the fallback into `Scan()` + minimal end-to-end test

**Files:**
- Modify: `svc/plugin/manifest.go` (add fallback branch at end of `Scan()`, just before `dedupeLocalsByBase`)
- Modify: `svc/plugin/manifest_test.go` (add E1 first as a smoke test for the wiring)

**Interfaces:**
- Consumes: `hasAnyManifest(base)`, `hasAnyConventionalSkillsDir(base)`, `isInsideAgentDir(base)`, `model.LocalPlugin`, `scanSkills`, `model.Skill`.
- Produces: when none of the manifest files are reachable AND a conventional skills directory exists AND base is not inside an agents dir, append a synthetic `LocalPlugin{Name: filepath.Base(base) or "root", Base: base}` to `out.Locals` and run `scanSkills(base, &lp, nil)` on it.

- [ ] **Step 1: Write the failing test (E1)**

Append to `svc/plugin/manifest_test.go`:

```go
// TestScan_NoManifest_Fallback_SkillsDir verifies the headline fix: a repo
// with NO plugin.json / marketplace.json / skill.json and only a top-level
// skills/<name>/SKILL.md layout still surfaces its skills.
func TestScan_NoManifest_Fallback_SkillsDir(t *testing.T) {
	base := t.TempDir()
	skillDir := filepath.Join(base, "skills", "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# my-skill\nDoes things."), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1, "no-manifest repo should still get a synthetic plugin")
	assert.Equal(t, filepath.Base(base), parsed.Locals[0].Name)
	assert.Equal(t, base, parsed.Locals[0].Base)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "my-skill", parsed.Locals[0].Skills[0].Name)
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `go test ./svc/plugin/... -run TestScan_NoManifest_Fallback_SkillsDir -v`
Expected: FAIL with `Len: 0` (parsed.Locals is empty, length 0 != 1).

- [ ] **Step 3: Wire the fallback into `Scan()`**

Open `svc/plugin/manifest.go` and replace the body of `Scan` (lines 20-37) with:

```go
func Scan(base string) (model.Parsed, error) {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return model.Parsed{}, err
	}
	var out model.Parsed
	if err := scanMarketplace(absBase, &out); err != nil {
		return model.Parsed{}, err
	}
	if err := scanPluginAtBase(absBase, &out); err != nil {
		return model.Parsed{}, err
	}
	if err := scanSkillJsonAtBase(absBase, &out); err != nil {
		return model.Parsed{}, err
	}
	out.Locals = dedupeLocalsByBase(out.Locals)

	// Fallback: when no manifest declared this dir as a plugin, but it
	// still looks like a skill repo (has a conventional skills/ dir) and
	// it isn't itself an agents/ subdirectory, treat base as a synthetic
	// root plugin. This keeps "drop a skill in skills/ and try it" working
	// without requiring a manifest up front.
	if !hasAnyManifest(absBase) &&
		hasAnyConventionalSkillsDir(absBase) &&
		!isInsideAgentDir(absBase) {
		name := filepath.Base(absBase)
		if name == "." || name == string(filepath.Separator) {
			name = "root"
		}
		lp := model.LocalPlugin{Name: name, Base: absBase}
		scanSkills(absBase, &lp, nil)
		out.Locals = append(out.Locals, lp)
	}

	return out, nil
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `go test ./svc/plugin/... -run TestScan_NoManifest_Fallback_SkillsDir -v`
Expected: PASS.

- [ ] **Step 5: Run the full manifest test package, verify no regressions**

Run: `go test ./svc/plugin/... -v`
Expected: all pre-existing tests still PASS, plus the new `TestScan_NoManifest_Fallback_SkillsDir` PASS.

- [ ] **Step 6: Commit**

```bash
git add svc/plugin/manifest.go svc/plugin/manifest_test.go
git commit -m "feat(plugin): scan skills/ when no manifest file is present"
```

---

## Task 5: Add remaining Scan-level test cases (E2–E10)

**Files:**
- Modify: `svc/plugin/manifest_test.go` (append E2–E10)

This task adds the rest of the spec's test matrix in one batch. They share the wiring from Task 4 and do not require further production code changes.

- [ ] **Step 1: Write E2–E10 tests**

Append to `svc/plugin/manifest_test.go`:

```go
// E2: <base>/.claude/skills/ is the conventional dir.
func TestScan_NoManifest_Fallback_DotClaudeSkillsDir(t *testing.T) {
	base := t.TempDir()
	skillDir := filepath.Join(base, ".claude", "skills", "design")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# design\ndesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "design", parsed.Locals[0].Skills[0].Name)
}

// E3: <base>/.agents/skills/ is the conventional dir.
func TestScan_NoManifest_Fallback_DotAgentsSkillsDir(t *testing.T) {
	base := t.TempDir()
	skillDir := filepath.Join(base, ".agents", "skills", "agent-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# agent-skill\ndesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "agent-skill", parsed.Locals[0].Skills[0].Name)
}

// E4: skills/ sitting INSIDE an agents/ subdir must NOT be picked up
// as a fallback plugin. The conventional agents/ layout is for subagents.
func TestScan_NoManifest_SkillsInsideAgentDir_Ignored(t *testing.T) {
	base := filepath.Join(t.TempDir(), "agents")
	skillDir := filepath.Join(base, "skills", "not-a-plugin")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# not-a-plugin\ndesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	assert.Empty(t, parsed.Locals, "nested agents/skills/ must not trigger the fallback")
}

// E5: empty skills/ dir → still synthesize a plugin, but with no skills.
// We do not suppress the plugin on empty skills; the user may add one
// later and we want it discoverable.
func TestScan_NoManifest_EmptySkillsDir_PluginStillCreated(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "skills"), 0o755))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1, "empty skills/ should still produce a plugin")
	assert.Empty(t, parsed.Locals[0].Skills)
}

// E6: no manifest, no conventional skills dir → no plugin.
func TestScan_NoManifest_NoSkillsDir_NoFallback(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "README.md"),
		[]byte("# just a readme"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	assert.Empty(t, parsed.Locals)
}

// E7: an existing plugin.json must short-circuit the fallback. We do not
// get a synthetic duplicate alongside the manifest-declared plugin.
func TestScan_PluginJsonExists_NoFallback(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"declared"}`), 0o644))

	// Also create a conventional skills/ dir — must NOT trigger fallback.
	skillDir := filepath.Join(base, "skills", "extra")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# extra\ndesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1, "plugin.json must suppress the fallback")
	assert.Equal(t, "declared", parsed.Locals[0].Name)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "extra", parsed.Locals[0].Skills[0].Name,
		"the skills/ dir still feeds the declared plugin via A4")
}

// E8: a malformed plugin.json is still a "manifest present" signal — the
// fallback must not mask a real parse bug by emitting an empty synthetic
// plugin.
func TestScan_MalformedPluginJson_StillHasManifest_NoFallback(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, ".claude-plugin", "plugin.json"),
		[]byte("not json"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "skills"), 0o755))

	parsed, err := Scan(base)
	require.NoError(t, err)
	assert.Empty(t, parsed.Locals,
		"malformed plugin.json counts as 'manifest present' — fallback must NOT run")
}

// E9: an existing skill.json must short-circuit the fallback too.
func TestScan_SkillJsonExists_NoFallback(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "skill.json"),
		[]byte(`{"name":"ui-ux"}`), 0o644))
	skillDir := filepath.Join(base, "skills", "design")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# design\ndesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	assert.Equal(t, "ui-ux", parsed.Locals[0].Name)
}

// E10: BFS mid-level safety. A sub-plugin dir reached via a parent's
// marketplace.json declaration has a skills/ child but no own manifest.
// Because the parent declaration already populated Locals, the fallback
// short-circuits — the child does NOT get a duplicate synthetic plugin
// from its own skills/.
func TestScan_NoManifest_SkillsAtBFSMidLevel_NoFallback(t *testing.T) {
	// Parent has marketplace.json declaring a sub-plugin at sub/.
	parent := t.TempDir()
	subDir := filepath.Join(parent, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(parent, ".claude-plugin"), 0o755))

	marketplace := `{
		"plugins": [
			{"name": "sub", "source": "./sub"}
		]
	}`
	require.NoError(t, os.WriteFile(
		filepath.Join(parent, ".claude-plugin", "marketplace.json"),
		[]byte(marketplace), 0o644))

	// Sub-plugin has its own skills/ — must NOT trigger the fallback at
	// this level; it should attach as a sub-plugin via A5 only.
	skillDir := filepath.Join(subDir, "skills", "leaf")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# leaf\ndesc"), 0o644))

	// Scan the PARENT (the BFS would also Scan(sub) on the next level;
	// we exercise Scan(sub) directly to assert the short-circuit).
	parsed, err := Scan(subDir)
	require.NoError(t, err)
	// sub has no manifest of its own AND has skills/ → fallback SHOULD
	// fire for Scan(sub) at this level, producing exactly one plugin.
	// (The BFS-level dedup happens in Walk(), not Scan().) This is the
	// intended behavior: a remote-fetched sub-plugin without its own
	// manifest gets a synthetic plugin from its own skills/.
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "leaf", parsed.Locals[0].Skills[0].Name)
}
```

- [ ] **Step 2: Run the new test cases**

Run: `go test ./svc/plugin/... -run 'TestScan_NoManifest|TestScan_PluginJsonExists_NoFallback|TestScan_MalformedPluginJson_StillHasManifest_NoFallback|TestScan_SkillJsonExists_NoFallback' -v`
Expected: all PASS.

- [ ] **Step 3: Run the full test package**

Run: `go test ./svc/plugin/... -v`
Expected: PASS for every test, both pre-existing and new.

- [ ] **Step 4: Run the full project test suite**

Run: `go test ./...`
Expected: PASS across all packages (`cmd/`, `model/`, `svc/...`, `utils/...`).

- [ ] **Step 5: Commit**

```bash
git add svc/plugin/manifest_test.go
git commit -m "test(plugin): cover skills-folder fallback paths (E1-E10)"
```

---

## Task 6: Final verification + spec/plan housekeeping

**Files:** none (read-only checks + one optional `README.todo` update)

- [ ] **Step 1: Run the full test suite one more time**

Run: `go test ./...`
Expected: PASS, zero failures, zero skips.

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: zero issues.

- [ ] **Step 3: Build the binary**

Run: `go build -o bin/skills ./cmd/skills`
Expected: builds cleanly. (Optional smoke: `./bin/skills --help` shows the usual help output.)

- [ ] **Step 4: Update `README.todo`**

If the spec/plan/fix is to be tracked in the project's todo list, append under the appropriate heading in `/Users/bytedance/projects/tmp/skills/README.todo`. (If the user does not want this tracked there, skip this step.)

- [ ] **Step 5: Commit housekeeping (if any)**

```bash
git add README.todo  # only if Step 4 made a change
git commit -m "docs: mark skills-folder fallback as done in README.todo"
```

---

## Self-Review

**1. Spec coverage:**

| Spec section     | Plan task |
| ---------------- | --------- |
| A1–A5 (always scan) | Pre-existing, not modified by any task. The helpers + fallback are additive; the existing `Scan` body is preserved in Task 4. ✅ |
| B0 (no manifest on disk) | Task 1 (`hasAnyManifest`) + Task 4 (uses it) ✅ |
| B2 (has conventional skills dir) | Task 2 (`hasAnyConventionalSkillsDir`) + Task 4 ✅ |
| B3 (not in agents dir) | Task 3 (`isInsideAgentDir`) + Task 4 ✅ |
| C1 (agent dir protection) | Task 3 (E4 sub-cases) + Task 4 ✅ |
| D1 (manifest hits, all three skills dirs merge) | E7 ✅ |
| D2 (sub-plugin gets its own scan) | E10 (proves Scan() fires for sub when it has skills/ and no own manifest) ✅ |
| D3 (remote plugin not affected) | No change to `classifyRemote`; pre-existing tests cover this. ✅ |
| D4 (self-marketplace + would-match-B) | E7 covers the "manifest exists" half. The "B0 not fire" half is the fallback branch itself. ✅ |
| D5 (BFS mid-level safety) | E10 (with explanatory comment that BFS dedup happens in `Walk`, not `Scan`) ✅ |
| D6 (`/agents/` + B2) | E4 ✅ |
| D7/D8 (empty skills/ → plugin with Skills=[]) | E5 ✅ |
| Error semantics (parse error counts as present) | E8 + Task 1 subtest "malformed json still counts as present" ✅ |
| Permission error counts as present | Task 1 Step 3 (`!os.IsNotExist(err)` branch); not a separate test because it's hard to simulate portably and is exercised by the same `hasAnyManifest` logic. ✅ |
| E1–E10 test matrix | Task 4 (E1) + Task 5 (E2–E10) ✅ |

**2. Placeholder scan:** No "TBD" / "TODO" / "implement later" / "add appropriate error handling" / "fill in details" / "similar to Task N" in the plan. All code blocks are complete. ✅

**3. Type consistency:**
- `hasAnyManifest(base string) bool` — defined in Task 1, consumed in Task 4. ✅
- `hasAnyConventionalSkillsDir(base string) bool` — defined in Task 2, consumed in Task 4. ✅
- `isInsideAgentDir(base string) bool` — defined in Task 3, consumed in Task 4. ✅
- `model.LocalPlugin{Name, Base}` — used in Task 4 the same way as in `scanPluginAtBase` and `scanMarketplace` (Name + Base fields). ✅
- `scanSkills(base, &lp, nil)` — exact signature match with the existing `func scanSkills(base string, lp *model.LocalPlugin, additive []string)` (Task 4). ✅
- `filepath.Base(absBase)` for the Name — used in Task 4. ✅
- `string(filepath.Separator)` for the "root" guard — consistent throughout. ✅

No mismatches found.
