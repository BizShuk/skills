# `skills remove` — clean installed skills/subagents

## Context

Today the `skills` CLI only grows the install set: `add` installs skills/subagents from a source into one or more agents, `update` re-installs them. There is no way to roll back. Users have to manually `rm -rf` the agent's `.claude/skills/<name>` / `.claude/agents/<name>.md` (or the equivalent under `~/.gemini/antigravity/...`, `~/.codex/...`, etc.) — and then later `skills update` will silently re-create whatever `installs.json` still tracks.

`skills remove` is the missing counterpart:

1. Walk every agent's install dirs (project + global, all 6 agents) and list what's installed.
2. Let the user multi-select via a TUI modeled on the existing `add` flow.
3. Confirm, then delete from disk **and** drop the names from `installs.json` so `update` stays honest.

Default uses an interactive TUI; `--yes` skips the TUI and the confirm prompt for scripted use.

## Approach

Mirror the existing `add` plumbing rather than invent a new stack:

- **Discovery** lives next to `agent.Agents()` in `svc/agent/`. New `DiscoverInstalled() ([]InstalledItem, error)` reads from each known agent's `Project*Dir` + `User*Dir`, splits into skills (subdirs containing `SKILL.md`) and subagents (`.md` files), and groups by `(Name, Kind)` so a skill in three agents shows up once with the agent list as suffix.
- **Removal** lives next to `agent.Apply()` in a new `agent.Remove(sel RemoveSelection) error`. For each picked item, for each agent that has it, delete the skill directory or subagent `.md` file. Errors are aggregated per-path so a single permission glitch doesn't abort the whole batch.
- **Metadata sync** happens inside `agent.Remove`: it loads `update.InstallsFile`, drops the removed names from matching `Entry.Skills` / `Entry.Subagents`, and saves. Entries that lose all of their skills and subagents are removed outright so future `update` runs don't try to fetch them.
- **TUI** is a new simplified bubbletea model in `svc/tui/remove.go`. One phase (no agent/level step — "delete everywhere this skill lives" is the natural semantics of the chosen row granularity). Search filter, space toggle, enter commit, esc cancel. Reuses the `lipgloss` styles and viewport helpers from `svc/tui/tui.go`.
- **Confirm gate** is a stdin `y/N` after the TUI exits — same `bufio.NewReader` pattern as a normal `rm` confirmation, no need to model a 4th TUI phase. `--yes` skips it.

## Files to add / modify

### New files

- `svc/agent/installed.go` — `InstalledKind` (`skill` / `subagent`), `InstalledItem` struct, `DiscoverInstalled()`.
- `svc/agent/remove.go` — `RemoveSelection` struct, `Remove(sel) ([]string, error)` returning the list of paths it deleted.
- `svc/agent/installed_test.go` — table-driven coverage of discovery: skills only, subagents only, mixed, same skill in two agents, missing dirs, missing `SKILL.md`.
- `svc/agent/remove_test.go` — happy-path delete, partial-failure aggregation, installs.json sync (skill drop, subagent drop, drop whole entry when empty, leave other entries alone), idempotency (missing path is not an error).
- `svc/tui/remove.go` — `Model`, `NewModel(items)`, `Run(items) (RemoveSelection, error)`, `View()`. Reuses `defaultViewportHeight`, `lipgloss` styles, `checkedStyle`.
- `svc/tui/remove_test.go` — toggle row, search filter, esc cancel, enter on empty selection, group-by-(name,kind) data flow.

### Modified files

- `cmd/root.go` — register `remove` cobra command. Flags: `--agent` (stringSlice, same as `add`), `--global` / `--project` (mutually exclusive scope filter), `--yes` (skip TUI + confirm). RunE: call `agent.DiscoverInstalled`, filter by `--agent` / scope, hand to `tui.Run` (or auto-select all under `--yes`), print summary, ask y/N, then `agent.Remove`.
- `svc/update/store.go` — add `DropNames(f *InstallsFile, removedSkills, removedSubagents []string) []Entry` returning the entries that were dropped (so callers can log them).
- `README.md` — `## Usage` section for `skills remove`, flag table mirroring the `add` block.
- `README.todo` — new `- [ ] Add `skills remove` command` line under a `## Remove` heading.

### Reused, do not re-implement

- `agent.Agents()`, `agent.Detect()`, `agent.ExpandHome()` from `svc/agent/agents.go` for the install-dir table.
- `agent.Apply`'s selection → destinations logic — invert it for removal (same `Global` / `Project` branching, same `filepath.Join(cwd, ...)` for relative `Project*Dir`).
- `update.Load`, `update.Save`, `update.Upsert`, `update.Remove` from `svc/update/store.go` (the new `DropNames` joins them).
- `tui`'s lipgloss style palette and `defaultViewportHeight` const.
- `utils/walk.go`'s level-bounded walk (only if we ever need recursive sub-plugin discovery during remove — for v1, single direct read of each install dir is enough).

## Data shapes (sketch)

```go
// svc/agent/installed.go
type InstalledKind string
const (
    InstalledSkill    InstalledKind = "skill"
    InstalledSubagent InstalledKind = "subagent"
)

// InstalledItem is one row in the remove TUI: the union of every location
// where (Name, Kind) is currently installed. Same name in two agents shows
// as a single row; removal deletes ALL copies.
type InstalledItem struct {
    Name       string          // "writer" / "code-reviewer"
    Kind       InstalledKind
    Locations  []InstalledLocation // one per (agent, scope)
}

type InstalledLocation struct {
    Agent AgentType
    Scope update.Scope // "project" | "global"
    Path  string       // absolute path on disk
}

func DiscoverInstalled() ([]InstalledItem, error)

// svc/agent/remove.go
type RemoveSelection struct {
    Items []InstalledItem
}

func Remove(sel RemoveSelection) (deleted []string, err error)
```

## Behavior details

- **Empty catalog** — if `DiscoverInstalled()` returns nothing, exit with `"no installed skills or subagents"` to stderr and code 0 (matches the empty-state behavior in the existing TUI tests).
- **Scope filter** — `--global` excludes project-scope installs from the discovery; `--project` excludes global; default is both.
- **`--agent` filter** — narrows discovery to the named agent types. With no `--agent` and no `--yes`, the TUI still shows everything (matches `add`'s "show every known agent" default); with `--yes`, only the explicitly named agents are touched.
- **Confirm prompt** — TUI commits, then on stderr: `Will delete: writer (skill) from claude-code, antigravity\nDelete 2 items? [y/N]`. Reads from stdin. `--yes` skips.
- **`installs.json` sync** — after disk delete, load the file, scan each entry, drop matching names; entries with empty `Skills` AND empty `Subagents` are removed. Save once at the end. Wrapped in `update.Load` / `update.Save` — same atomic temp-file rename pattern.
- **Partial failures** — if `os.RemoveAll(path)` fails for one location, log `warning: cannot remove <path>: <err>` and continue; other locations of the same item still get deleted. Return non-nil error iff any deletion failed so the CLI exits non-zero and the user notices.

## Verification

```bash
# From a scratch project:
cd "$(mktemp -d)"
GOBIN=$HOME/.local/bin go install ./cmd/skills
mkdir -p .claude/skills/writer .claude/agents
echo "# writer" > .claude/skills/writer/SKILL.md
echo "# reviewer" > .claude/agents/code-reviewer.md

# The TUI should list two rows: writer (skill), code-reviewer (subagent).
~/.local/bin/skills remove

# After quit + y: both gone, .claude/skills and .claude/agents dirs empty (or
# only empty dirs left).
ls .claude/skills .claude/agents

# --yes path non-interactive:
~/.local/bin/skills add ./local/plugins --depth 1   # install something test-able
~/.local/bin/skills remove --yes
~/.local/bin/skills remove                       # should now print no-installed-state

# Unit tests:
cd ~/projects/tmp/skills
go test ./svc/agent/... -run 'Test(DiscoverInstalled|Remove)'
go test ./svc/tui/... -run 'Remove'
go test ./...
GOBIN=$HOME/.local/bin go install ./cmd/skills  # rebuild after edits
```

The install-file sync is the trickiest piece — `TestRemove_DropsNamesFromInstallsFile` should also assert that an unrelated entry is preserved, and that an entry whose only remaining items were removed is itself dropped.
