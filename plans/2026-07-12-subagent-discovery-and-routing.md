# Plan: Subagent Discovery and Routing for `skills add`

This plan maps the changes needed so the Go rewrite of `skills add` can
discover subagents (alongside skills) and route them to each agent's
`projectAgentsDir` / `userAgentsDir` instead of the skills dir.

The plan covers:

1. `svc/plugin/manifest.go` — extending `scanSkills` to also scan for
   subagents.
2. `svc/agent/agents.go` / `svc/agent/agent.go` — the existing
   `ProjectAgentsDir` / `UserAgentsDir` columns in the Agent table.
3. `svc/agent/install.go` — extending the `Selection` type and the
   `Apply` function so subagent directories are copied to the right root.
4. The provider JSON files — already have the right fields.
5. Git history findings (relevant prior work on agent/subagent support).
6. README context (project purpose).

---

## 1. Current state of skill discovery (`svc/plugin/manifest.go`)

### What `scanSkills` does today (lines 257–322)

The function fills `lp.Skills` (a `[]Skill` on `LocalPlugin`) from two
sources:

a. **Conventional layout** — three directories are scanned, each
   contributes a skill if it contains a sub-directory holding
   `SKILL.md`:

   ```go
   // Conventional: <lp.Base>/skills/<name>/SKILL.md,
   //               <lp.Base>/.claude/skills/<name>/SKILL.md,
   //               <lp.Base>/.agents/skills/<name>/SKILL.md
   conventionalDirs := []string{
       filepath.Join(lp.Base, "skills"),
       filepath.Join(lp.Base, ".claude", "skills"),
       filepath.Join(lp.Base, ".agents", "skills"),
   }
   for _, conv := range conventionalDirs {
       if entries, err := os.ReadDir(conv); err == nil {
           for _, e := range entries {
               if !e.IsDir() {
                   continue
               }
               skillDir := filepath.Join(conv, e.Name())
               skillFile := filepath.Join(skillDir, "SKILL.md")
               if _, err := os.Stat(skillFile); err != nil {
                   continue
               }
               add(skillDir)
           }
       }
   }
   ```

b. **Additive layout** — paths declared in the manifest's `skills`
   array. Each entry is treated as a path to `SKILL.md`; the parent
   directory is the skill directory. Path-traversal escapes of `base`
   are silently dropped (via `isContainedIn`).

   ```go
   for _, sp := range additive {
       if !strings.HasPrefix(sp, "./") {
           continue
       }
       candidate := filepath.Join(lp.Base, sp)
       skillDir := filepath.Dir(candidate)
       if !isContainedIn(skillDir, base) {
           continue
       }
       skillFile := filepath.Join(skillDir, "SKILL.md")
       if _, err := os.Stat(skillFile); err != nil {
           continue
       }
       add(skillDir)
   }
   ```

`add` de-duplicates by `skillDir` and appends a `Skill{Name, Path,
Description}`. `Description` is read from the first non-heading body
line of `SKILL.md` (with YAML frontmatter support) — see
`readDescription` at lines 333–426.

### What needs to change to add subagent discovery

We want to ALSO scan the parallel `agents/` directories for subagent
directories. A "subagent" is a sub-directory containing a marker file
(per the task description: `AGENT.md` or similar). The marker should be
configurable; for the first pass `AGENT.md` is a sensible default and
matches the `SKILL.md` convention.

#### 1a. Introduce a `Subagent` value type

In `svc/plugin/types.go` add a sibling to `Skill`:

```go
// Subagent is a single subagent directory within a local plugin.
// The directory must contain a marker file (AGENT.md) to be
// recognized, parallel to how Skill requires SKILL.md.
type Subagent struct {
    Name        string // directory name (e.g. "code-reviewer")
    Path        string // absolute path to the subagent directory
    Description string // short summary for TUI rendering (may be empty)
}
```

Then add `Subagents []Subagent` to `LocalPlugin`:

```go
type LocalPlugin struct {
    Name      string
    Base      string
    Skills    []Skill
    Subagents []Subagent // union of conventional + additive subagent dirs
}
```

#### 1b. Add a marker-file constant

```go
// agentMarker is the file whose presence in a sub-directory flags it
// as a subagent, parallel to SKILL.md for skills. Lower-case to match
// filesystem case-sensitivity on Linux.
const agentMarker = "AGENT.md"
```

#### 1c. Extract a `scanDirs` helper, then call it twice

Refactor the conventional-scan block in `scanSkills` so the same logic
runs once for skills and once for subagents. The simplest refactor is
a closure that takes a marker filename and an `add` callback, then
call it for `SKILL.md` → `add` to `lp.Skills` and for `AGENT.md` →
`add` to `lp.Subagents`:

```go
func scanSkills(base string, lp *LocalPlugin, additive []string) {
    scanConventional := func(parent string, marker string, accept func(dir string)) {
        if entries, err := os.ReadDir(parent); err == nil {
            for _, e := range entries {
                if !e.IsDir() {
                    continue
                }
                dir := filepath.Join(parent, e.Name())
                if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
                    continue
                }
                accept(dir)
            }
        }
    }

    seenSkill := map[string]bool{}
    addSkill := func(dir string) {
        if seenSkill[dir] { return }
        seenSkill[dir] = true
        desc := readDescription(filepath.Join(dir, "SKILL.md"))
        lp.Skills = append(lp.Skills, Skill{
            Name:        filepath.Base(dir),
            Path:        dir,
            Description: desc,
        })
    }

    seenAgent := map[string]bool{}
    addAgent := func(dir string) {
        if seenAgent[dir] { return }
        seenAgent[dir] = true
        desc := readDescription(filepath.Join(dir, "AGENT.md"))
        lp.Subagents = append(lp.Subagents, Subagent{
            Name:        filepath.Base(dir),
            Path:        dir,
            Description: desc,
        })
    }

    // Conventional skills: <Base>/skills, <Base>/.claude/skills, <Base>/.agents/skills
    for _, conv := range []string{
        filepath.Join(lp.Base, "skills"),
        filepath.Join(lp.Base, ".claude", "skills"),
        filepath.Join(lp.Base, ".agents", "skills"),
    } {
        scanConventional(conv, "SKILL.md", addSkill)
    }

    // Conventional subagents: <Base>/agents, <Base>/.claude/agents, <Base>/.agents/agents
    for _, conv := range []string{
        filepath.Join(lp.Base, "agents"),
        filepath.Join(lp.Base, ".claude", "agents"),
        filepath.Join(lp.Base, ".agents", "agents"),
    } {
        scanConventional(conv, "AGENT.md", addAgent)
    }

    // Additive skills (path to SKILL.md, parent is the skill dir)
    for _, sp := range additive {
        if !strings.HasPrefix(sp, "./") { continue }
        candidate := filepath.Join(lp.Base, sp)
        skillDir := filepath.Dir(candidate)
        if !isContainedIn(skillDir, base) { continue }
        if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
            continue
        }
        addSkill(skillDir)
    }

    // Additive subagents: same idea, manifest declares paths to AGENT.md.
    // Decision point: do we (a) add a separate additive array on the
    // manifests, or (b) allow the existing `skills` array to double as a
    // source for AGENT.md paths? (b) is simpler and the marker
    // disambiguates. Recommend (b) for v1: each additive entry is tried
    // as a SKILL.md first, and if absent, as an AGENT.md.
    for _, sp := range additive {
        if !strings.HasPrefix(sp, "./") { continue }
        candidate := filepath.Join(lp.Base, sp)
        subDir := filepath.Dir(candidate)
        if !isContainedIn(subDir, base) { continue }
        if _, err := os.Stat(filepath.Join(subDir, "AGENT.md")); err != nil {
            continue
        }
        addAgent(subDir)
    }
}
```

The current `pluginManifest` (line 94–97) has only `Skills []string`;
that single `Skills` array is re-used for both skill and subagent
additive paths in the design above. If we want a clean separation we
add `Subagents []string` to `pluginManifest` and `marketplacePlugin`,
and the call site at `scanSkills` passes both arrays. (Recommended
once the design is clear — flag for the user to decide.)

#### 1d. Notes / constraints found in the file

- `add` de-dupes by absolute directory path, so a subagent and a
  skill living at the same path (unlikely but possible) collapse to
  one entry. The two new `seenSkill` / `seenAgent` maps keep each list
  independent.
- `isContainedIn` (lines 432–446) is the canonical "is this path
  inside base" check; use it for the additive subagent branch too.
- The conventional-scan block ignores non-directories; we should keep
  that rule (a loose `AGENT.md` at the root of a `skills/` directory
  is not a subagent, by analogy with the skill rule).
- `readDescription` already handles YAML frontmatter and
  non-heading body lines, so we can call it for `AGENT.md` without
  change.

---

## 2. Provider JSON files (already have the right fields)

`svc/agent/providers/` contains 6 files, each declaring both
`projectSkillsDir`/`userSkillsDir` AND `projectAgentsDir`/
`userAgentsDir`:

| File | type | projectSkillsDir | userSkillsDir | projectAgentsDir | userAgentsDir |
| --- | --- | --- | --- | --- | --- |
| `claude-code.json` | claude-code | `.claude/skills` | `~/.claude/skills` | `.claude/agents` | `~/.claude/agents` |
| `antigravity.json` | antigravity | `.agents/skills` | `~/.gemini/antigravity/skills` | `.agents/agents` | `~/.gemini/antigravity/agents` |
| `antigravity-cli.json` | antigravity-cli | `.agents/skills` | `~/.gemini/antigravity-cli/skills` | `.agents/agents` | `~/.gemini/antigravity-cli/agents` |
| `codex.json` | codex | `.agents/skills` | `~/.codex/skills` | `.agents/agents` | `~/.codex/agents` |
| `opencode.json` | opencode | `.agents/skills` | `~/.config/opencode/skills` | `.agents/agents` | `~/.config/opencode/agents` |
| `hermes-agent.json` | hermes-agent | `.hermes/skills` | `~/.hermes/skills` | `.hermes/agents` | `~/.hermes/agents` |

**Constraint**: `TestProviderFieldsRoundTripViaJSON` in
`svc/agent/agent_test.go` (lines 28–45) already asserts that every
provider has non-empty `ProjectAgentsDir` and `UserAgentsDir`, and
that `UserAgentsDir` starts with `~/`. So provider data is already
in place — the install logic just isn't reading those fields yet.

`TestProviderJSONFilesAreValid` (lines 86–107) also asserts that
every embedded JSON contains the keys `projectAgentsDir` /
`userAgentsDir` — same conclusion.

---

## 3. `svc/agent/agent.go` / `agents.go` — Agent value type

`Agent` (in `agents.go` lines 23–31) already carries both
`ProjectAgentsDir` and `UserAgentsDir`, alongside the
`ProjectSkillsDir` / `UserSkillsDir` fields:

```go
type Agent struct {
    Type              AgentType
    DisplayName       string
    ProjectSkillsDir  string // relative to cwd when not absolute
    UserSkillsDir     string // absolute
    ProjectAgentsDir  string // relative to cwd when not absolute
    UserAgentsDir     string // absolute
    DetectDir         string // if this dir exists on disk, the agent is "installed"
}
```

`Agents()` (lines 37–52) translates `Provider` → `Agent` and
expands `~/` for the user fields (via `ExpandHome` in `agent.go`
lines 106–117). No change required here — the install package just
has to start reading the new columns.

---

## 4. `svc/agent/install.go` — `Selection` and `Apply`

### What `Apply` does today (lines 32–75)

```go
type Selection struct {
    SkillPaths []string
    AgentTypes []AgentType
    Global     bool
    Cwd        string
}

func Apply(sel Selection) error {
    cwd := sel.Cwd
    if cwd == "" { cwd, _ = os.Getwd() }

    agentTable := Agents()
    byType := make(map[AgentType]Agent, len(agentTable))
    for _, a := range agentTable { byType[a.Type] = a }

    for _, t := range sel.AgentTypes {
        a, ok := byType[t]
        if !ok { continue }
        destRoot := a.ProjectSkillsDir
        if sel.Global { destRoot = a.UserSkillsDir }
        if destRoot == "" { continue }
        if !sel.Global && !filepath.IsAbs(destRoot) {
            destRoot = filepath.Join(cwd, destRoot)
        }

        for _, src := range sel.SkillPaths {
            name := filepath.Base(src)
            dst := filepath.Join(destRoot, name)
            if err := copyTree(src, dst); err != nil {
                return fmt.Errorf("install: copy %s -> %s: %w", src, dst, err)
            }
        }
    }
    return nil
}
```

Key behaviors that must be preserved for subagents:

- Per-agent-type loop; an agent with an empty `destRoot` in the
  current mode is silently skipped (line 58–61).
- In project mode, a relative `destRoot` is joined to `Cwd`; in
  global mode the user-dir is already absolute.
- `copyTree` (lines 81–90) is reused as-is for subagent trees — it
  copies any file-or-dir to any dst, refuses symlinks (line 105–108),
  preserves source mode (line 117–119).

### How to extend `Selection` and `Apply`

Add a parallel field for subagent source paths, then drive two
destination computations (skills root, agents root) inside the same
agent loop:

```go
type Selection struct {
    SkillPaths   []string // absolute paths to skill directories
    SubagentPaths []string // absolute paths to subagent directories
    AgentTypes   []AgentType
    Global       bool
    Cwd          string
}

func Apply(sel Selection) error {
    // ... same cwd + agentTable setup as before ...

    for _, t := range sel.AgentTypes {
        a, ok := byType[t]
        if !ok { continue }

        // skills destination
        skillsRoot := a.ProjectSkillsDir
        if sel.Global { skillsRoot = a.UserSkillsDir }
        if skillsRoot != "" {
            if !sel.Global && !filepath.IsAbs(skillsRoot) {
                skillsRoot = filepath.Join(cwd, skillsRoot)
            }
            for _, src := range sel.SkillPaths {
                dst := filepath.Join(skillsRoot, filepath.Base(src))
                if err := copyTree(src, dst); err != nil {
                    return fmt.Errorf("install: copy skill %s -> %s: %w", src, dst, err)
                }
            }
        }

        // subagent destination
        agentsRoot := a.ProjectAgentsDir
        if sel.Global { agentsRoot = a.UserAgentsDir }
        if agentsRoot != "" {
            if !sel.Global && !filepath.IsAbs(agentsRoot) {
                agentsRoot = filepath.Join(cwd, agentsRoot)
            }
            for _, src := range sel.SubagentPaths {
                dst := filepath.Join(agentsRoot, filepath.Base(src))
                if err := copyTree(src, dst); err != nil {
                    return fmt.Errorf("install: copy subagent %s -> %s: %w", src, dst, err)
                }
            }
        }
    }
    return nil
}
```

#### Constraints and choices to call out for the user

- **No collision check between skill and subagent basenames.** If
  the user picks a skill called `helper` AND a subagent called
  `helper` for the same agent, the current `Apply` does not detect
  the collision because the two roots are different
  (`<cwd>/.claude/skills/helper` vs `<cwd>/.claude/agents/helper`).
  The two separate roots make a name collision impossible at the
  filesystem level. Good.
- **One error string per copy** — keep the existing `fmt.Errorf`
  format and only change the prefix label from `install: copy` to
  `install: copy skill` / `install: copy subagent` for clearer error
  messages. (Optional but cheap.)
- **Selection's zero value** — keep `SubagentPaths []string` so a
  caller that doesn't set it (the existing TUI/test code) doesn't
  accidentally trigger installs. The new inner loop will simply not
  iterate.
- **Tests to update / add** in `svc/agent/install_test.go`:
    - Mirror `TestApply_ProjectModeCopiesIntoCwdRelativeDir` to assert
    that a subagent lands at
    `<cwd>/.claude/agents/<basename>/AGENT.md` for `claude-code`.
    - Mirror `TestApply_GlobalModeCopiesIntoUserDir` with a
    subagent going to `~/.claude/agents/<basename>/AGENT.md`.
    - Mirror `TestApply_BasenameNamesTheSkill` for subagents.
    - Add a test that combines `SkillPaths` and `SubagentPaths` in
    one `Selection` and verifies both land in the right roots.

---

## 5. Git history findings (relevant prior work)

`git log --oneline -20` on the Go branch:

```
3cc16c4 feat: support multiline YAML descriptions and add TUI fold/unfold capability for long skill descriptions
83924e7 feat: support skill.json format, expand conventional skill locations, parse YAML frontmatter descriptions, and improve TUI with summary stats and folded remote plugins
09c7571 fix: deduplicate and absorb redundant root plugins to prevent nested same-name categories
d4e34b1 feat: add todo list for auto update feature
efe1119 feat: add multi-phase TUI flow with agent selection and installation level configuration
c9432b4 feat(tui): default-unchecked + lipgloss color
c9aaf7d build: add vscode tasks.json with go install as default
f85b4a3 feat(tui): checkbox style with search, viewport, and skill descriptions
59fdd9a feat(tui): nested sub-plugins fold by default
d891301 build: add Makefile with install as default target
b7344c8 refactor: split entry point out of cmd/skills
f8df91a feat: nested plugin tree tui with fold and category toggle
d2e9717 docs: rename README.golang.md to README.md
7237d39 feat: extract agent provider configs into svc/agent package
037622f chore: drop legacy TypeScript implementation
254657a docs: golang skills add usage
7cc080f feat: wire add command end to end
a554f2d feat: bubbletea selection tui
b9f58cd feat: agent install table and skill copy
c31e83b feat: recursive bfs discovery walker
```

`git log --oneline --all --grep="agent\|subagent" -i` shows the
prior, now-removed TypeScript implementation already had subagent
support:

```
efe1119 feat: add multi-phase TUI flow with agent selection and installation level configuration
7237d39 feat: extract agent provider configs into svc/agent package
037622f chore: drop legacy TypeScript implementation
b9f58cd feat: agent install table and skill copy
cb7b7ca Merge pull request #1467 from haydenbleasel/eve-subagent-support
68f0a05 Add Eve subagent support to skills add/list/remove/update
1e3ef4a Merge pull request #1458 from vercel-labs/support-eve
... (older agent / subagent merges from the TS era) ...
```

Notable: the historical commit `68f0a05 Add Eve subagent support to
skills add/list/remove/update` predates the Go port. The Go rewrite
in `7237d39` introduced the `ProjectAgentsDir` / `UserAgentsDir`
fields on providers — anticipating subagent support — but the
subagent discovery + install side of it was never wired up. The
fields are there, the JSON is there, the Agent type is there; only
the scanner, Selection, and Apply need to grow.

---

## 6. README context

`README.md` (78 lines, in Traditional Chinese + English headings) is
the Go-branch usage doc. It explicitly says:

> 重寫後的 CLI 在功能上對應原本 TypeScript 版的 `skills add`：解析
> 來源、走訪遞迴 plugin、以 bubbletea TUI 讓使用者挑選要安裝的
> skills 與目標 agents，最後把選定的 skill 與 subagent 目錄複製到
> 對應的安裝目錄。

— i.e. the documented intent is for the Go rewrite to install both
**skill** AND **subagent** directories. The README's "支援的 Agents"
table even lists `project agents` and `user agents` columns for
all 6 providers, alongside the skill columns. So the README already
advertises subagent support; the missing piece is the implementation.

The README also notes:

- The build can fail with `go build ./...` because of a name clash
  with the `skills/` subdirectory; use `GOBIN=… go install
  ./cmd/skills` or `go build -o bin/skills ./cmd/skills` instead.
- The CLI is `skills add [path]` with `--global`, `--agent`,
  `--depth`, `--yes` flags.
- Module path: `github.com/bizshuk/skills`, entry `cmd/skills/main.go`.
- The spec lives at
  `docs/superpowers/specs/2026-07-04-skills-add-golang-design.md`
  (not read here — worth a follow-up to confirm any
  subagent-related spec clauses).

---

## 7. Open questions to confirm with the user

1. **Marker filename.** The user said "AGENT.md or similar". Should
   the constant be exactly `AGENT.md`, or should we accept a list
   (e.g. `AGENT.md` / `agent.md` / `subagent.md`)? Recommendation:
   start with `AGENT.md` only; can broaden later.
2. **Additive manifest entries for subagents.** Re-use the existing
   `skills[]` array (each entry tried as both `SKILL.md` and
   `AGENT.md`), or add a separate `subagents[]` array on
   `pluginManifest` and `marketplacePlugin`? Recommendation: add a
   separate `subagents[]` array for clarity; the marker is
   unambiguous so reusing the same array is technically fine but
   harder to document.
3. **CLI flag.** Is the TUI going to expose a "subagents" column
   alongside "skills", or do we auto-discover and present them in
   the same category tree? Recommendation: present them in the same
   tree but with a different icon/label so the user can see what
   they're selecting.
4. **Spec check.** Should we re-read
   `docs/superpowers/specs/2026-07-04-skills-add-golang-design.md`
   before changing the scanner, to make sure we don't conflict with
   any design decisions there?
