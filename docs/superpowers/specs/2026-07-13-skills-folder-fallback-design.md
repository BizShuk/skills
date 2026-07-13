<!-- markdownlint-disable MD040 MD060 -->

# Skills-folder Fallback — Design Spec

- Date: 2026-07-13
- Status: Draft (awaiting user review)
- Scope: `svc/plugin/manifest.go::Scan()` and helpers

## Problem

`Scan(base)` today requires **at least one** of these manifest files to exist
on disk before it emits any `LocalPlugin`:

- `<base>/.claude-plugin/marketplace.json`
- `<base>/.claude-plugin/plugin.json`
- `<base>/skill.json`

A repo that ships only a `skills/` (or `.claude/skills/`, `.agents/skills/`)
directory with no manifest is silently ignored. The Walk BFS in
`utils/walk.go` then produces a `Catalog` with `Roots=[]` and the user sees
no skills, no error, no hint that the path even contained a skill.

This is a usability bug for "drop a skill into a folder and try it" workflows
that don't yet have a manifest.

## Goals

- Scan skills in `<base>/skills/`, `<base>/.claude/skills/`, `<base>/.agents/skills/`
  even when **no** manifest file is present.
- Keep existing manifest-driven behavior identical (no double-counting,
  no re-scanning, no swallowing of real errors).
- Apply the fallback only at the **given project root** — not on any
  sub-plugin / sub-folder reached through BFS.
- Skip the fallback when the base sits inside an `agents/` directory,
  so the conventional subagent layout is not mistaken for a plugin.

## Non-Goals

- Auto-generating `plugin.json` / `marketplace.json`. (Out of scope; we just
  read what's there.)
- Recursive walk of every nested subdirectory looking for `SKILL.md`. We
  only consult the three conventional top-level skills directories.
- Inferring a fancier plugin name from SKILL.md frontmatter. Name comes
  from `filepath.Base(base)`, period.

## Approach (chosen: A)

Add a single fallback branch at the end of `Scan()`, before
`dedupeLocalsByBase`. Two new helpers (`hasAnyManifest`,
`hasAnyConventionalSkillsDir`, `isInsideAgentDir`) keep the logic
self-contained and testable.

No change to `utils/walk.go`; the BFS path is already protected by
"non-empty `Locals` → not absorbed into a synthetic root" semantics and
by the "absorb same-dir local into parent placeholder" rule that already
handles remote fetches whose materialized dir matches the parent's base.

## Trigger Decision

```text
Scan(base) runs
 ├─ read marketplace.json (A1)
 ├─ read plugin.json     (A2)
 ├─ read skill.json      (A3)
 │
 └─ none of A1/A2/A3 exists on disk?        (B0, see "Error semantics")
      ├─ yes → run fallback (B)
      └─ no  → no change
```

A file whose JSON parses badly is treated as **existing** — the existing
silent-ignore path still applies, and the fallback does NOT run. Rationale:
a parse failure is a real bug we don't want to mask by emitting an empty
synthetic plugin.

## Conditions

### A. Always scan (existing behavior, unchanged)

| ID  | Condition                                                                                              | Source           |
| --- | ------------------------------------------------------------------------------------------------------ | ---------------- |
| A1  | `<base>/.claude-plugin/marketplace.json` exists → run `scanMarketplace`                                 | `manifest.go:26` |
| A2  | `<base>/.claude-plugin/plugin.json` exists → run `scanPluginAtBase`                                     | `manifest.go:29` |
| A3  | `<base>/skill.json` exists → run `scanSkillJsonAtBase`                                                  | `manifest.go:32` |
| A4  | Any A1/A2/A3 hit → run `scanSkills` for **that** plugin's `lp.Base`                                     | `manifest.go:158, 179, 58` |
| A5  | marketplace.json local sub-plugin (`source: "./..."`) → run `scanSkills` for **that** sub-plugin's Base | `manifest.go:158` |

### B. Fallback scan (new)

| ID  | Condition                                                                                       |
| --- | ------------------------------------------------------------------------------------------------ |
| B0  | A1, A2, A3 **all** return `os.IsNotExist(err)` from `os.Stat` (parse errors do not count)        |
| B1  | `out.Locals` is still empty after A (defensive; B0 already implies this)                          |
| B2  | At least one of `<base>/skills/`, `<base>/.claude/skills/`, `<base>/.agents/skills/` exists      |
| B3  | `base` is **not** inside any conventional `agents/` directory (C1)                               |
| →   | Build `LocalPlugin{Name=filepath.Base(base) (or "root" if base is "."), Base=base}`, run `scanSkills(base, &lp, nil)`, append to `out.Locals` |

### C. Ignore

| ID  | Condition                                                                                            |
| --- | ---------------------------------------------------------------------------------------------------- |
| C1  | `base` contains a path segment equal to `agents`, **or** a `.claude`/`.agents` segment immediately followed by `agents`, **and** that `agents` segment is **not** the final segment of `base` (a repo whose root folder is literally named `agents` is still a valid fallback target — only nested `agents/` directories are excluded). Path-segment match only (no substring on the whole path). |
| C2  | BFS reached this `base` via a parent's marketplace / remote fetch (i.e. `n.parent != nil` in `Walk()`). The existing absorb-into-parent logic covers this. |
| C3  | Skills are scattered in non-conventional subdirectories (`docs/`, `examples/`, `tests/`, ...). Only the three conventional top-level dirs are recognized. |
| C4  | `agents/`, `docs/`, `examples/`, `tests/`, `build/`, `dist/`, etc. — none of these are skill sources unless the manifest names them via A4/A5. |

### D. Edge cases

| ID  | Situation                                                                                       | Behavior                                                                                                |
| --- | ----------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| D1  | A1 hits **and** all three skills dirs are present                                                | They all merge into the **same** plugin's `Skills` (existing `scanSkills` behavior).                   |
| D2  | marketplace lists two local sub-plugins, one of which also has its own `skills/`                 | That sub-plugin's `scanSkills` runs (A5). Fallback does NOT fire there (the sub-plugin's `Locals` is non-empty). |
| D3  | Remote plugin entry (`source: { source: "github", ... }`)                                       | Goes through `classifyRemote` → `out.Remotes`. Fallback not applicable to remote entries.              |
| D4  | `base` is the marketplace `pluginRoot` self-entry (source `"./")` **and** would also match B      | `out.Locals` is non-empty after A → B does not fire. No duplicates.                                     |
| D5  | `base` is reached mid-BFS (e.g. a subdir of a remote fetch) | BFS safety: in practice every BFS-reached dir is either a remote repo root (no `skills/` at top → B2 fails) or a local subdir declared by a manifest (A4/A5 → `Locals` non-empty, B0/B1 suppress the fallback). B does not fire on BFS mid-levels in the wild. E10 nails this down with a synthetic case. |
| D6  | `base` path contains `/agents/` **and** B2 condition holds                                        | C1 wins; the entire fallback is skipped.                                                                |
| D7  | All three skills dirs exist but are empty / contain no `SKILL.md`                                 | `lp.Skills = []`, but the plugin itself is still appended. We do NOT suppress the plugin on empty skills — the user may add one later. |
| D8  | Same as D7 from the user's perspective                                                           | Same as D7.                                                                                            |

## Error semantics

- **File not exists** (`os.IsNotExist(err)`) → counts as "no manifest" → enables fallback.
- **File exists but JSON parse fails** → counts as "manifest present" → fallback suppressed. Existing caller's silent-ignore on parse error is preserved; we do not change that behavior in this spec.
- **Permission error / I/O error reading the file** → counts as "manifest present" (treat as existing). We do not want a transient permission glitch to flip a repo into fallback mode and emit a synthetic empty plugin.
- The `isContainedIn` / path-traversal guards in `scanSkills` continue to apply unchanged.

## Implementation sketch

```go
// In svc/plugin/manifest.go, just before dedupeLocalsByBase(out.Locals):

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
```

New helpers in the same file (or `manifest_helpers.go` if it grows):

```go
// hasAnyManifest reports whether ANY of the three manifest paths is reachable
// as a regular file. "Exists" means `os.Stat` returned either no error or an
// error other than `os.IsNotExist` (e.g. permission denied). A parse error
// of an existing-but-malformed file still counts as "exists" — we want the
// existing silent-ignore path to surface (or not) unchanged, and we don't
// want a transient permission glitch to flip a repo into fallback mode.
func hasAnyManifest(base string) bool {
    for _, p := range []string{
        filepath.Join(base, ".claude-plugin", "marketplace.json"),
        filepath.Join(base, ".claude-plugin", "plugin.json"),
        filepath.Join(base, "skill.json"),
    } {
        if _, err := os.Stat(p); err == nil {
            return true
        } else if !os.IsNotExist(err) {
            return true
        }
    }
    return false
}

func hasAnyConventionalSkillsDir(base string) bool {
    for _, p := range []string{
        filepath.Join(base, "skills"),
        filepath.Join(base, ".claude", "skills"),
        filepath.Join(base, ".agents", "skills"),
    } {
        if info, err := os.Stat(p); err == nil && info.IsDir() {
            return true
        }
    }
    return false
}

// isInsideAgentDir reports whether base sits inside a conventional agents/
// directory. "Inside" means a path segment equal to "agents" that is NOT the
// final segment of base, or a ".claude"/".agents" segment immediately
// followed by "agents". A repo whose root folder is literally named "agents"
// is a valid fallback target — only nested agents/ directories are excluded.
// Path-segment match only — partial names (e.g. "agents-keeper") do not match.
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

## Test matrix (must pass)

| ID  | Test name                                                          | Expected                                                                             |
| --- | ------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| E1  | `TestScan_NoManifest_Fallback_SkillsDir`                           | 1 plugin, has skills from `<base>/skills/<n>/SKILL.md`                               |
| E2  | `TestScan_NoManifest_Fallback_DotClaudeSkillsDir`                  | 1 plugin, has skills from `<base>/.claude/skills/<n>/SKILL.md`                       |
| E3  | `TestScan_NoManifest_Fallback_DotAgentsSkillsDir`                  | 1 plugin, has skills from `<base>/.agents/skills/<n>/SKILL.md`                       |
| E4  | `TestScan_NoManifest_SkillsInsideAgentDir_Ignored`                 | 0 plugins (C1)                                                                       |
| E5  | `TestScan_NoManifest_EmptySkillsDir_PluginStillCreated`            | 1 plugin, `Skills=[]` (D7)                                                           |
| E6  | `TestScan_NoManifest_NoSkillsDir_NoFallback`                       | 0 plugins                                                                            |
| E7  | `TestScan_PluginJsonExists_NoFallback`                             | Existing plugin.json behavior; exactly 1 plugin, no synthetic duplicate              |
| E8  | `TestScan_MalformedPluginJson_StillHasManifest_NoFallback`        | Bad JSON → fallback suppressed; existing silent-ignore path                          |
| E9  | `TestScan_SkillJsonExists_NoFallback`                              | Existing skill.json behavior; exactly 1 plugin, no synthetic duplicate               |
| E10 | `TestScan_NoManifest_SkillsAtBFSMidLevel_NoFallback`              | Synthetic case: a sub-plugin dir reached via marketplace has a `skills/` child but no own manifest. Because the parent marketplace already populated `Locals`, B0→B1 short-circuits — the child does NOT get a duplicate synthetic plugin from its own `skills/`. |

Pre-existing tests (`TestScan_MarketplaceMixedLocalRemote`,
`TestScan_PluginJsonOnly`, `TestScan_SelfMarketplaceAndPluginJsonDedup`,
`TestScan_AdditiveTraversalRejected`, `TestScan_SkipsReadmeMDInAgentsDir`,
`TestScan_NestedMarketplaceSubPlugins_OptInTopLevel`,
`TestScan_TopLevelAgentsDefaultOff`,
`TestScan_NestedMarketplaceSubPluginAgentsDir`,
`TestScan_AgentsFieldInPluginManifest`,
`TestScan_AgentsFieldRejectsMissingFile`,
`TestScan_DescriptionReads*`) must continue to pass without modification.

## Out of scope / future work

- Auto-generate a minimal `plugin.json` when the fallback fires (deferred).
- Inheriting the parent's `TopLevelAgents` / `AgentPaths` into the synthetic
  plugin. Subagents under `agents/` (Source 1 in `scanSubagents`) already
  work because `scanSkills` calls `scanSubagents` unconditionally.
- "Drop a file with just a SKILL.md in the root" detection (would need a
  separate heuristic and is intentionally excluded by C3).
