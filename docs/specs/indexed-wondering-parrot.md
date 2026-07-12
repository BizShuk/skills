# Plan: Add subagent support to the skills CLI "add list" with distinct icon

## Context

The `skills add` CLI discovers plugins, shows a TUI for selecting skills, and installs them into agent directories. The agent provider data model already declares `projectAgentsDir` / `userAgentsDir` columns (see all 6 JSON files in `svc/agent/providers/`), and the `Agent` struct in `svc/agent/agents.go` carries those fields — but discovery never scans for subagents, the TUI never shows them, and `Apply` never installs them. This change wires subagents end-to-end through discovery → TUI selection → install, with a diamond (◇/◆) icon to visually distinguish subagents from the circle (○/●) icon used for skills.

## Subagent vs Skill — structural difference

- **Skill**: A directory with a `SKILL.md` file inside: `skills/<name>/SKILL.md`
- **Subagent**: A flat `.md` file directly under an `agents/` directory: `agents/<name>.md` (or `.claude/agents/<name>.md`, `.agents/agents/<name>.md`)

So `Subagent.Path` is the absolute path to the `.md` file itself (not a directory), and `Subagent.Name` is the filename without the `.md` extension.

Example plugin structure:

```
plugin/
├── agents/                    ← scanned for subagents
│   ├── code-reviewer.md       → Subagent{Name:"code-reviewer", Path:".../code-reviewer.md"}
│   └── test-runner.md         → Subagent{Name:"test-runner", Path:".../test-runner.md"}
├── .claude/agents/            ← also scanned
│   └── security-scanner.md
├── skills/                    ← scanned for skills (existing)
│   └── my-skill/
│       └── SKILL.md           → Skill{Name:"my-skill", Path:".../my-skill/"}
```

## Files to modify

### 1. `svc/plugin/types.go` — Add Subagent type

- Add `Subagent` struct:

  ```go
  type Subagent struct {
      Name        string // filename without .md extension
      Path        string // absolute path to the .md file
      Description string
  }
  ```

- Add `Subagents []Subagent` field to `LocalPlugin`.

### 2. `svc/plugin/manifest.go` — Add subagent scanning

- In `scanSkills` (line ~267), after the conventional skills loop, add a subagent scanning block. Subagents are `.md` files (not directories) found under `agents/`, `.claude/agents/`, `.agents/agents/`:

  ```go
  agentDirs := []string{
      filepath.Join(lp.Base, "agents"),
      filepath.Join(lp.Base, ".claude", "agents"),
      filepath.Join(lp.Base, ".agents", "agents"),
  }
  ```

  For each dir, `os.ReadDir`, then for each entry:
    - Skip directories (subagents are flat files)
    - Skip files without `.md` extension
    - `name := strings.TrimSuffix(e.Name(), ".md")`
    - `path := filepath.Join(agentDir, e.Name())`
    - `desc := readDescription(path)` — reuse the existing YAML-frontmatter + first-body-line extractor
    - Deduplicate by `name` (same name in multiple agent dirs → first one wins) using a `seenAgent map[string]bool`
    - Append `Subagent{Name: name, Path: path, Description: desc}` to `lp.Subagents`

### 3. `svc/plugin/discover.go` — Propagate Subagents through Category

- Add `Subagents []Subagent` field to `Category` struct.
- In `Walk()` (the inner `g.Go` closure, line ~141), when absorbing a local plugin's skills into a parent placeholder, also append the local plugin's `Subagents`:

  ```go
  n.parent.Skills = append(n.parent.Skills, lp.Skills...)
  n.parent.Subagents = append(n.parent.Subagents, lp.Subagents...)
  ```

- When creating a fresh `Category` for a local plugin, copy both `Skills` and `Subagents`.
- In `Catalog.AllSkills()` — optionally add `AllSubagents()` if needed, but not strictly required since the TUI walks the tree directly.

### 4. `svc/tui/tui.go` — TUI row, icons, View, Selection

**Row extension.** Extend the `row` struct:

```go
type row struct {
    node      *plugin.Category
    skill     *plugin.Skill    // nil when row is header or subagent
    subagent  *plugin.Subagent // nil when row is header or skill
    depth     int
    isHeader  bool
}
```

**New glyphs.** Add subagent checkbox glyphs:

```go
const (
    glyphUnchecked        = "○"
    glyphChecked          = "●"
    glyphIndeterminate    = "▣"
    glyphSAChecked        = "◆"       // diamond, filled — checked subagent
    glyphSAUnchecked      = "◇"       // diamond, hollow — unchecked subagent
)
```

**Subagent style.** Add a distinct style for subagent names (e.g., magenta/purple):

```go
subagentNameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("171"))
```

**Model extension.** Add a separate checked map for subagents:

```go
checkedSubagent map[string]bool  // keyed by Subagent.Path
```

Initialized as empty map in `NewModel`.

**rebuildVisible.** In the skill-rendering loop (line ~259-266), immediately after appending skill rows for an expanded category, also append subagent rows when unfolded:

```go
if !m.folded[c] {
    // ... existing skill loop ...
    for i := range c.Subagents {
        sa := &c.Subagents[i]
        if q == "" || subagentMatchesQuery(sa, q) {
            self = append(self, row{node: c, subagent: sa, depth: depth + 1})
        }
    }
}
```

**Header toggle.** `toggleSubtree` (line ~341) must also toggle subagents: add a second loop over `n.Subagents` that sets `m.checkedSubagent[sa.Path] = target`.

**Header check state.** `headerCheckState` (line ~310) must also walk subagents: increment `total`/`checked` for each subagent, looking up `m.checkedSubagent[sa.Path]`.

**View rendering (line ~770).** After the existing skill-row rendering block, add a subagent rendering block:

```go
if r.subagent != nil {
    box := glyphSAUnchecked
    if m.checkedSubagent[r.subagent.Path] {
        box = checkedStyle.Render(glyphSAChecked)
    }
    var desc string
    if r.subagent.Description != "" {
        desc = " — " + r.subagent.Description
    }
    b.WriteString(fmt.Sprintf("%s%s%s %s%s\n",
        indent, cursor, box, subagentNameStyle.Render(r.subagent.Name), desc))
    continue
}
```

**Summary line.** Extend `countSummary()` to also count subagents, and render in the summary line:

```
Plugins: 3 (1 remote), Skills: 10 (2 remote), Subagents: 4
```

**Selection.** Add `SubagentPaths` to the returned `Selection`:

```go
func (m Model) Selection() agent.Selection {
    // ... existing SkillPaths ...
    saPaths := make([]string, 0, len(m.checkedSubagent))
    for path, ok := range m.checkedSubagent {
        if ok { saPaths = append(saPaths, path) }
    }
    return agent.Selection{
        SkillPaths:    paths,
        SubagentPaths: saPaths,
        AgentTypes:    agentTypes,
        Global:        m.global,
    }
}
```

**search matching (line ~226-236).** `rebuildVisible` needs `subagentDirectMatch` logic parallel to `skillDirectMatch` — if any subagent's Name or Description contains the query, the category header stays visible.

### 5. `svc/agent/install.go` — Extend Selection and Apply

- Add `SubagentPaths []string` to `Selection`.
- In `Apply()`, after the skill-copy loop, add a subagent-copy loop. Subagent paths point to `.md` files — `copyTree` already handles single files natively (it calls `copyFile` when `os.Stat` reports a non-directory). The destination is the agent's `AgentsDir`:

  ```go
  agentsRoot := a.ProjectAgentsDir
  if sel.Global {
      agentsRoot = a.UserAgentsDir
  }
  if agentsRoot == "" {
      continue
  }
  if !sel.Global && !filepath.IsAbs(agentsRoot) {
      agentsRoot = filepath.Join(cwd, agentsRoot)
  }
  for _, src := range sel.SubagentPaths {
      // src is /path/to/code-reviewer.md, filepath.Base = "code-reviewer.md"
      dst := filepath.Join(agentsRoot, filepath.Base(src))
      if err := copyTree(src, dst); err != nil {
          return fmt.Errorf("install: copy subagent %s -> %s: %w", src, dst, err)
      }
  }
  ```

### 6. `cmd/root.go` — Wire --yes mode for subagents

In the `--yes` branch (~line 72-79), also collect all subagent paths from the catalog:

```go
if yes {
    for _, s := range cat.AllSkills() {
        sel.SkillPaths = append(sel.SkillPaths, s.Path)
    }
    // NEW: collect subagents
    cat.WalkSubagents(func(sa plugin.Subagent) {
        sel.SubagentPaths = append(sel.SubagentPaths, sa.Path)
    })
    // ... agents ...
}
```

Add a `WalkSubagents` method on `Catalog` (or inline the walk in `root.go`).

### 7. `svc/tui/tui_test.go` — Add subagent tests

- `TestSubagentDistinctIcon`: verify a subagent row renders with ◇ not ○.
- `TestHeaderToggleAlsoTogglesSubagents`: a category with 1 skill + 1 subagent; Space on header checks both; checkedSubagent map is populated.
- `TestSubagentSelection`: verify `Selection().SubagentPaths` returns checked subagent paths.
- `TestSubagentDescriptionRendered`: subagent with description shows it after em-dash.
- Extend existing test fixtures (`sampleCatalog`, etc.) or create new fixtures that include Subagents.

### 8. `svc/agent/install_test.go` — Add subagent install tests

- `TestApplySubagentProjectMode`: creates a temp `.md` file (e.g. `reviewer.md`) as the subagent source, installs to project agents dir (`.claude/agents/reviewer.md`).
- `TestApplySubagentGlobalMode`: installs to user-level agents dir.
- `TestApplySkillAndSubagentTogether`: both `SkillPaths` and `SubagentPaths` set; verify skill goes to `skills/` dir and subagent `.md` file goes to `agents/` dir.

## Verification

1. **Unit tests:** `go test ./svc/...` — all existing tests must pass, new tests cover the subagent paths.
2. **Manual TUI test:** `go run . add ./some-test-plugin` — verify subagents appear with diamond (◇) icon in the TUI, can be selected with space, and get installed to the correct agents directory.
3. **End-to-end with a real plugin:** create a test plugin with both `skills/` and `agents/` directories, run `skills add`, verify both skills and subagents appear in the TUI with distinct icons, select them, confirm they land in the right target directories.

## Icons Summary

| Type | Unchecked | Checked |
|------|-----------|---------|
| Skill | ○ | ● (green) |
| Subagent | ◇ | ◆ (green) |

Subagent names are rendered in a purple/magenta style to further distinguish them from skill names (which use the default style).
