# Plugin Skills Directory Implementation Plan

> For agentic workers: use `executing-plans` to implement this plan inline. The
> implementation was initially kept uncommitted; a later explicit user request
> authorized publishing it.

**Goal:** Make `skills add Dev-GOM/claude-code-marketplace` discover and select
`blender-toolkit` when its child `plugin.json` declares `"skills": ["./skills"]`.

**Architecture:** Keep discovery in `svc/plugin/manifest.go`. For a local marketplace
entry, merge skill paths declared by its nested `plugin.json`. Resolve each manifest
skill path by filesystem type: accept a `SKILL.md` file, a directory containing
`SKILL.md`, or a directory containing direct `<name>/SKILL.md` children.

**Tech Stack:** Go, `os`, `path/filepath`, Testify.

## Global Constraints

- Preserve path containment and silent-ignore behavior for invalid paths.
- Do not change TUI, installer, remote fetch, or GitHub subpath behavior.
- Preserve existing direct `./extra/SKILL.md` compatibility.

---

### Task 1: Add failing manifest regression tests

**Files:**

- Modify: `svc/plugin/manifest_test.go`

**Interfaces:**

- Consumes: `Scan(base string) (model.Parsed, error)`.
- Produces: regression coverage for nested plugin manifests and directory skill paths.

- [ ] Add `TestScan_MarketplaceNestedPluginManifestSkillsDirectory`, with a marketplace
  entry pointing to `./plugins/blender-toolkit`, a nested plugin manifest containing
  `"skills": ["./skills"]`, and `skills/SKILL.md`. Assert one local plugin and one skill
  whose path is the `skills` directory.
- [ ] Add `TestScan_PluginManifestSkillCollectionDirectory`, with
  `"skills": ["./custom-skills"]` and `custom-skills/alpha/SKILL.md`. Assert the
  `alpha` skill is discovered.
- [ ] Run:

  ```bash
  go test ./svc/plugin -run 'TestScan_(MarketplaceNestedPluginManifestSkillsDirectory|PluginManifestSkillCollectionDirectory)' -count=1 -v
  ```

  Expected: both tests fail because the current parser ignores nested `plugin.json`
  skills and treats directory paths as file paths.

### Task 2: Resolve nested manifest and directory paths

**Files:**

- Modify: `svc/plugin/manifest.go`

**Interfaces:**

- Consumes: marketplace `pluginBase`, marketplace entry skill paths, and nested
  `pluginManifest.Skills`.
- Produces: a merged additive path list passed to `scanSkills`, plus directory-aware
  path resolution inside `scanSkills`.

- [ ] Extract a small `readPluginManifest(base string) (pluginManifest, bool)` helper
  that returns `false` for missing or malformed manifests.
- [ ] Reuse the helper from `scanPluginAtBase` and from local marketplace processing.
- [ ] Merge marketplace entry paths first and nested plugin paths second without
  changing category naming.
- [ ] In `scanSkills`, resolve each additive path as follows:

  ```text
  regular SKILL.md file       -> add its parent directory
  directory with SKILL.md     -> add that directory
  directory with child skills -> add each direct child containing SKILL.md
  anything else               -> ignore
  ```

- [ ] Run the focused test command from Task 1 and confirm both tests pass.
- [ ] Run `go test ./svc/plugin -count=1` and confirm the package passes.

### Task 3: Verify the original symptom and repository

**Files:**

- No additional source files.

**Interfaces:**

- Consumes: the completed manifest scanner.
- Produces: verification evidence for the original marketplace and full repository.

- [ ] Run `gofmt` on the two modified Go files.
- [ ] Run `go test ./... -count=1`.
- [ ] Run `go build -o /private/tmp/skills-debug .`.
- [ ] Run the built binary against the downloaded marketplace fixture and confirm the
  TUI summary includes one additional skill and `blender-toolkit` can be expanded and
  selected.
- [ ] Run `git diff --check` and inspect `git diff` to confirm only scoped files changed
  in addition to pre-existing user changes.
