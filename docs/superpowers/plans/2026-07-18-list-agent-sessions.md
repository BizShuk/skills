# List Agent Sessions Implementation Plan

> For agentic workers: REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

Goal: Add `skills session` to list agent sessions whose recorded working directory matches the current folder.

Architecture: Provider JSON files own all session roots through `sessionDirs`. The `svc/session` package discovers and normalizes sessions through small source-specific parsers, while `cmd/session.go` only resolves the current directory and wires Cobra output. The model remains data-only and the formatter writes through an injected `io.Writer`.

Tech Stack: Go 1.26.3, Cobra 1.10.2, standard-library JSON/JSONL parsing, `filepath`, `text/tabwriter`, and existing `svc/agent` provider embedding.

## Global Constraints

- Keep all agent-specific session paths in `svc/agent/providers/*.json`; do not add `~/.claude`, `~/.codex`, or other agent home literals to `svc/session`.
- Use `RunE`, `cmd.OutOrStdout()`, wrapped errors, and no process exits inside library or command packages.
- Compare normalized absolute paths; only include a session with explicit working-directory evidence matching the current folder.
- Missing configured roots and unsupported/invalid session records are skipped without aborting other providers.
- New exported Go types and functions receive doc comments beginning with their names.
- Follow red-green-refactor: every production behavior is preceded by a failing focused test.
- Preserve the existing provider ordering and install behavior.
- Keep user-facing repository documentation in Traditional Chinese with English technical terms in parentheses where appropriate; use backticks instead of bold emphasis.

---

### Task 1: Add provider-configured session roots

Files:

- Modify: `svc/agent/agent.go`
- Modify: `svc/agent/agents.go`
- Modify: `svc/agent/agent_test.go`
- Modify: `svc/agent/providers/antigravity-cli.json`
- Modify: `svc/agent/providers/antigravity.json`
- Modify: `svc/agent/providers/claude-code.json`
- Modify: `svc/agent/providers/codex.json`
- Modify: `svc/agent/providers/grok.json`
- Modify: `svc/agent/providers/hermes-agent.json`
- Modify: `svc/agent/providers/opencode.json`
- Modify: `svc/agent/providers/pi.json`

Interfaces:

- `agent.Provider.SessionDirs []string` decodes JSON `sessionDirs` values exactly as configured.
- `agent.Agent.SessionDirs []string` contains the same paths after `~/` expansion.
- `agent.Agents()` returns a fresh `SessionDirs` slice so callers cannot mutate embedded provider state.

- [ ] Step 1: Write the failing provider schema test.

Extend `TestProviderFieldsRoundTripViaJSON` with the explicit assertion that
the loaded provider has a non-nil session directory slice, and add a focused
test for home expansion:

```go
func TestProviderSessionDirsExpandHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	homedir.DisableCache = true

	for _, got := range Agents() {
		provider, ok := Find(Type(got.Type))
		require.True(t, ok)
		require.Len(t, got.SessionDirs, len(provider.SessionDirs))
		for i, raw := range provider.SessionDirs {
			expected, err := homedir.Expand(raw)
			require.NoError(t, err)
			assert.Equal(t, expected, got.SessionDirs[i])
		}
	}
}
```

Update the JSON validity test with:

```go
assert.Contains(t, raw, "sessionDirs")
```

- [ ] Step 2: Run the focused test and verify it fails because `Provider` and `Agent` do not yet expose `SessionDirs`.

Run: `go test ./svc/agent -run 'TestProviderSessionDirsExpandHome|TestProviderJSONFilesAreValid' -count=1`

Expected: `FAIL` with an undefined `SessionDirs` field or missing `sessionDirs` JSON key.

- [ ] Step 3: Add the provider and translated-agent fields.

Add the field to `Provider`:

```go
SessionDirs []string `json:"sessionDirs"`
```

Add the field to `Agent`:

```go
SessionDirs []string // absolute session roots after `~/` expansion
```

In `Agents()`, expand each provider root into a fresh slice:

```go
sessionDirs := make([]string, 0, len(p.SessionDirs))
for _, dir := range p.SessionDirs {
	sessionDirs = append(sessionDirs, expand(dir))
}

out = append(out, Agent{
	Type:              p.Type,
	DisplayName:       p.DisplayName,
	ProjectSkillsDir:  p.ProjectSkillsDir,
	UserSkillsDir:     expand(p.UserSkillsDir),
	ProjectAgentsDir:  p.ProjectAgentsDir,
	UserAgentsDir:     expand(p.UserAgentsDir),
	DetectDir:         expand(p.DetectDir),
	SessionDirs:       sessionDirs,
})
```

Add these JSON values:

```json
"sessionDirs": ["~/.gemini/antigravity-cli/brain"]
"sessionDirs": ["~/.gemini/antigravity-ide/brain"]
"sessionDirs": ["~/.claude/projects"]
"sessionDirs": ["~/.codex/sessions", "~/.codex/archived_sessions"]
"sessionDirs": ["~/.grok/sessions"]
"sessionDirs": ["~/.hermes/sessions"]
"sessionDirs": ["~/.local/share/opencode/storage"]
"sessionDirs": ["~/.pi/agent/sessions"]
```

- [ ] Step 4: Run the focused provider tests and verify they pass.

Run: `go test ./svc/agent -run 'TestProviderSessionDirsExpandHome|TestProviderJSONFilesAreValid|TestProviderFieldsRoundTripViaJSON' -count=1`

Expected: `PASS`.

- [ ] Step 5: Run `gofmt` on changed Go files and inspect the diff.

Run: `gofmt -w svc/agent/agent.go svc/agent/agents.go svc/agent/agent_test.go && git diff --check`

Expected: no whitespace errors.

### Task 2: Add the session value model

Files:

- Create: `model/session.go`
- Create: `model/session_test.go`

Interfaces:

- `model.AgentSession` stores `Agent`, `ID`, `StartedAt`, `LastActivity`, and `Path`.

- [ ] Step 1: Write the failing model test.

Create `model/session_test.go`:

```go
package model

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAgentSessionStoresListingMetadata(t *testing.T) {
	started := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	last := started.Add(15 * time.Minute)
	s := AgentSession{
		Agent:        "codex",
		ID:           "session-1",
		StartedAt:    started,
		LastActivity: last,
		Path:         filepath.Join("/tmp", "session.jsonl"),
	}

	assert.Equal(t, "codex", s.Agent)
	assert.Equal(t, "session-1", s.ID)
	assert.Equal(t, started, s.StartedAt)
	assert.Equal(t, last, s.LastActivity)
	assert.Equal(t, filepath.Join("/tmp", "session.jsonl"), s.Path)
}
```

- [ ] Step 2: Run the model test and verify it fails because `AgentSession` is undefined.

Run: `go test ./model -run TestAgentSessionStoresListingMetadata -count=1`

Expected: `FAIL` with `undefined: AgentSession`.

- [ ] Step 3: Add the data-only model.

Create `model/session.go`:

```go
package model

import "time"

// AgentSession is one agent session associated with a working directory.
type AgentSession struct {
	Agent        string
	ID           string
	StartedAt    time.Time
	LastActivity time.Time
	Path         string
}
```

- [ ] Step 4: Run the model test and verify it passes.

Run: `go test ./model -run TestAgentSessionStoresListingMetadata -count=1`

Expected: `PASS`.

### Task 3: Implement common path, timestamp, and JSON metadata helpers

Files:

- Create: `svc/session/helpers.go`
- Create: `svc/session/helpers_test.go`

Interfaces:

- `normalizePath(path string) (string, error)` returns a cleaned absolute path and resolves symlinks when possible.
- `samePath(left, right string) bool` compares normalized paths.
- `parseTimestamp(value any) (time.Time, bool)` accepts RFC3339/RFC3339Nano strings and Unix seconds or milliseconds.
- `workingDirectories(value any) []string` recursively extracts values whose keys are `cwd`, `Cwd`, `working_directory`, `workdir`, or `workingDirectory`.
- `sessionMetadata` accumulates ID, matching cwd evidence, earliest timestamp, and latest timestamp while a file is scanned.

- [ ] Step 1: Write failing helper tests covering path normalization, timestamp formats, nested cwd fields, and invalid input.

Create `svc/session/helpers_test.go` with these cases:

```go
func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
}

func TestSamePathResolvesEquivalentAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(root, link))
	assert.True(t, samePath(root, filepath.Join(link, ".")))
}

func TestParseTimestampSupportsRFC3339AndUnixUnits(t *testing.T) {
	want := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	for _, value := range []any{
		want.Format(time.RFC3339Nano),
		float64(want.Unix()),
		float64(want.UnixMilli()),
	} {
		got, ok := parseTimestamp(value)
		require.True(t, ok)
		assert.Equal(t, want, got)
	}
	_, ok := parseTimestamp("not-a-time")
	assert.False(t, ok)
}

func TestWorkingDirectoriesFindsNestedSupportedKeys(t *testing.T) {
	value := map[string]any{
		"payload": map[string]any{
			"args": map[string]any{
				"Cwd": "/workspace/project",
			},
		},
	}
	assert.Equal(t, []string{"/workspace/project"}, workingDirectories(value))
}
```

- [ ] Step 2: Run the helper tests and verify the expected failures.

Run: `go test ./svc/session -run 'TestSamePath|TestParseTimestamp|TestWorkingDirectories' -count=1`

Expected: `FAIL` because the package and helper functions do not exist.

- [ ] Step 3: Implement the helpers with standard-library types only.

Use `filepath.Abs`, `filepath.Clean`, and `filepath.EvalSymlinks` with an
absolute-path fallback. Treat numeric timestamps above `1e12` as milliseconds
and smaller values as seconds. Recursively walk only JSON objects and arrays
when collecting supported working-directory keys; do not inspect arbitrary
string values or prompt content.

The metadata accumulator must expose these operations to source parsers:

```go
type sessionMetadata struct {
	ID           string
	MatchesCWD   bool
	StartedAt    time.Time
	LastActivity time.Time
}

func (m *sessionMetadata) addID(id string)
func (m *sessionMetadata) addWorkingDirectories(values []string, cwd string)
func (m *sessionMetadata) addTimestamp(value any)
func (m sessionMetadata) session(agentName, path, fallbackID string) (model.AgentSession, bool)
```

- [ ] Step 4: Run the helper tests and verify they pass.

Run: `go test ./svc/session -run 'TestSamePath|TestParseTimestamp|TestWorkingDirectories' -count=1`

Expected: `PASS`.

### Task 4: Add Claude and Codex session discoverers

Files:

- Create: `svc/session/claude.go`
- Create: `svc/session/codex.go`
- Create: `svc/session/claude_test.go`
- Create: `svc/session/codex_test.go`

Interfaces:

- `discoverClaude(root, cwd string) ([]model.AgentSession, error)` scans all `.jsonl` files recursively, including `subagents/` files.
- `discoverCodex(root, cwd string) ([]model.AgentSession, error)` scans all `.jsonl` files recursively and uses `session_meta.payload` metadata.
- Both functions silently return an empty slice for a missing root and skip malformed lines or files with no matching cwd evidence.

- [ ] Step 1: Write the failing Claude fixture test.

The fixture must include one matching session, one different cwd, a malformed
line, and a nested subagent file. Assert that only the matching parent and
subagent sessions are returned, with earliest/latest timestamps.

```go
func TestDiscoverClaudeFiltersByCWDAndIncludesSubagents(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "project", "subagents"), 0o755))

	writeJSONL(t, filepath.Join(root, "project", "parent.jsonl"),
		`{"sessionId":"parent","cwd":"`+cwd+`","timestamp":"2026-07-18T08:00:00Z"}`,
		`not-json`,
		`{"sessionId":"parent","cwd":"`+cwd+`","timestamp":"2026-07-18T08:05:00Z"}`,
	)
	writeJSONL(t, filepath.Join(root, "project", "other.jsonl"),
		`{"sessionId":"other","cwd":"/other","timestamp":"2026-07-18T08:10:00Z"}`,
	)
	writeJSONL(t, filepath.Join(root, "project", "subagents", "child.jsonl"),
		`{"sessionId":"child","cwd":"`+cwd+`","timestamp":"2026-07-18T08:03:00Z"}`,
	)

	got, err := discoverClaude(root, cwd)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.ElementsMatch(t, []string{"child", "parent"}, []string{got[0].ID, got[1].ID})
	startedByID := map[string]time.Time{}
	lastByID := map[string]time.Time{}
	for _, item := range got {
		startedByID[item.ID] = item.StartedAt
		lastByID[item.ID] = item.LastActivity
	}
	assert.Equal(t, time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC), startedByID["parent"])
	assert.Equal(t, time.Date(2026, 7, 18, 8, 5, 0, 0, time.UTC), lastByID["parent"])
}
```

- [ ] Step 2: Run the Claude test and verify it fails because `discoverClaude` is undefined.

Run: `go test ./svc/session -run TestDiscoverClaudeFiltersByCWDAndIncludesSubagents -count=1`

Expected: `FAIL` with `undefined: discoverClaude`.

- [ ] Step 3: Implement Claude discovery.

Walk `root` using `filepath.WalkDir`. For every regular `.jsonl` file, scan
each line with a scanner buffer of at least 10 MiB, unmarshal into
`map[string]any`, add `sessionId`, `cwd`, and `timestamp` to metadata, then
emit one `model.AgentSession` only when the metadata has a non-empty ID and
`MatchesCWD` is true. Use the absolute file path in `Path` and `claude-code`
in `Agent`.

- [ ] Step 4: Run the Claude test and verify it passes.

Run: `go test ./svc/session -run TestDiscoverClaudeFiltersByCWDAndIncludesSubagents -count=1`

Expected: `PASS`.

- [ ] Step 5: Write the failing Codex fixture test.

Create a normal session file and an archived session file with
`session_meta.payload.cwd` set to the target. Add a different-cwd session and
assert both matching roots are returned, while malformed lines are ignored.

```go
func TestDiscoverCodexScansArchivedRootAndUsesSessionMeta(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested"), 0o755))

	writeJSONL(t, filepath.Join(root, "nested", "rollout.jsonl"),
		`{"type":"session_meta","payload":{"id":"active","cwd":"`+cwd+`"}}`,
		`{"type":"event_msg","timestamp":"2026-07-18T08:20:00Z"}`,
	)
	writeJSONL(t, filepath.Join(root, "archived.jsonl"),
		`{"type":"session_meta","payload":{"id":"archived","cwd":"`+cwd+`"}}`,
		`{"type":"event_msg","timestamp":"2026-07-18T07:20:00Z"}`,
	)

	got, err := discoverCodex(root, cwd)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.ElementsMatch(t, []string{"active", "archived"}, []string{got[0].ID, got[1].ID})
	for _, item := range got {
		assert.Equal(t, "codex", item.Agent)
	}
}
```

- [ ] Step 6: Run the Codex test and verify it fails.

Run: `go test ./svc/session -run TestDiscoverCodexScansArchivedRootAndUsesSessionMeta -count=1`

Expected: `FAIL` because `discoverCodex` is undefined.

- [ ] Step 7: Implement Codex discovery and verify it passes.

Use the same JSONL walker as Claude, but read `payload.id` and
`payload.cwd` only from records with `type == "session_meta"`. Timestamps may
come from any valid record in the file. Use the filename without `.jsonl` as
the fallback ID and `codex` as the agent value.

Run: `go test ./svc/session -run TestDiscoverCodexScansArchivedRootAndUsesSessionMeta -count=1`

Expected: `PASS`.

### Task 5: Add Grok and structured-metadata discoverers

Files:

- Create: `svc/session/grok.go`
- Create: `svc/session/structured.go`
- Create: `svc/session/grok_test.go`
- Create: `svc/session/structured_test.go`

Interfaces:

- `discoverGrok(root, cwd string) ([]model.AgentSession, error)` handles URL-escaped project directory names and child session directories.
- `discoverStructured(root, cwd, agentName string) ([]model.AgentSession, error)` handles transcript/session files for Antigravity, Hermes, OpenCode, and Pi when structured cwd metadata is present.

- [ ] Step 1: Write the failing Grok fixture test.

Create a project directory named with `url.PathEscape(cwd)` and two child
session directories. Put `working_directory`, `created_at`, and `updated_at`
values in `prompt_context.json` and `summary.json`. Add a second project root
and assert only the target project is returned in descending last-activity
order:

```go
func TestDiscoverGrokFiltersEscapedProjectRoot(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	project := filepath.Join(root, url.PathEscape(cwd))
	other := filepath.Join(root, url.PathEscape(filepath.Join(t.TempDir(), "other")))
	require.NoError(t, os.MkdirAll(filepath.Join(project, "session-a"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(project, "session-b"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(other, "session-other"), 0o755))

	writeJSONL(t, filepath.Join(project, "session-a", "summary.json"),
		`{"created_at":"2026-07-18T08:00:00Z","updated_at":"2026-07-18T08:10:00Z"}`,
	)
	writeJSONL(t, filepath.Join(project, "session-b", "summary.json"),
		`{"created_at":"2026-07-18T08:05:00Z","updated_at":"2026-07-18T08:20:00Z"}`,
	)
	writeJSONL(t, filepath.Join(project, "session-b", "prompt_context.json"),
		`{"working_directory":"`+cwd+`"}`,
	)
	writeJSONL(t, filepath.Join(other, "session-other", "summary.json"),
		`{"created_at":"2026-07-18T09:00:00Z","updated_at":"2026-07-18T09:30:00Z"}`,
	)

	got, err := discoverGrok(root, cwd)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.ElementsMatch(t, []string{"session-a", "session-b"}, []string{got[0].ID, got[1].ID})
	for _, item := range got {
		assert.Equal(t, "grok", item.Agent)
	}
}
```

- [ ] Step 2: Run the Grok test and verify it fails because `discoverGrok` is undefined.

Run: `go test ./svc/session -run TestDiscoverGrokFiltersEscapedProjectRoot -count=1`

Expected: `FAIL`.

- [ ] Step 3: Implement Grok discovery and verify it passes.

Decode each first-level project directory with `url.PathUnescape`, compare it
with the normalized cwd, and scan each child directory's JSON metadata. Use
the child directory name as the session ID. Set `Path` to the session
directory. Accept `prompt_context.working_directory` only when it matches the
target cwd.

Run: `go test ./svc/session -run TestDiscoverGrokFiltersEscapedProjectRoot -count=1`

Expected: `PASS`.

- [ ] Step 4: Write the failing structured-metadata test.

Create a transcript fixture with nested `tool_calls[].args.Cwd`, timestamps,
and an ID inferred from its enclosing directory. Add another fixture where the
cwd appears only in arbitrary prompt text; assert that the latter is ignored.

```go
func TestDiscoverStructuredUsesExplicitCwdKeysOnly(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "workspace")
	brain := filepath.Join(root, "brain-1", ".system_generated", "logs")
	require.NoError(t, os.MkdirAll(brain, 0o755))
	writeJSONL(t, filepath.Join(brain, "transcript.jsonl"),
		`{"created_at":"2026-07-18T08:00:00Z","tool_calls":[{"args":{"Cwd":"`+cwd+`"}}]}`,
		`{"created_at":"2026-07-18T08:05:00Z","content":"`+cwd+` was mentioned"}`,
	)

	got, err := discoverStructured(root, cwd, "antigravity")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "brain-1", got[0].ID)
}
```

- [ ] Step 5: Run the structured test and verify it fails.

Run: `go test ./svc/session -run TestDiscoverStructuredUsesExplicitCwdKeysOnly -count=1`

Expected: `FAIL` because `discoverStructured` is undefined.

- [ ] Step 6: Implement structured discovery and verify it passes.

Walk configured roots. Group records by the nearest session directory: for a
transcript under `.system_generated/logs`, use the brain directory; otherwise
use the file path as the session path. Parse JSON objects recursively through
the helper's supported key names, collect timestamps, and emit only groups
with explicit matching cwd evidence and a stable ID. Ignore arbitrary string
content and malformed lines.

Run: `go test ./svc/session -run TestDiscoverStructuredUsesExplicitCwdKeysOnly -count=1`

Expected: `PASS`.

### Task 6: Add service aggregation, sorting, and output formatting

Files:

- Create: `svc/session/service.go`
- Create: `svc/session/format.go`
- Create: `svc/session/service_test.go`
- Create: `svc/session/format_test.go`

Interfaces:

- `List(cwd string) ([]model.AgentSession, error)` loads expanded provider session roots and dispatches to source discoverers.
- `Format(w io.Writer, cwd string, sessions []model.AgentSession) error` renders the stable table or empty-result message.

- [ ] Step 1: Write failing tests for aggregation and stable sorting.

Use temporary `$HOME` roots containing a Claude fixture and a Codex fixture,
call `List` with the current test directory, and assert results are sorted by
last activity descending, then agent and ID. Add a test that missing roots do
not return an error.

```go
func TestListSortsByLastActivityThenAgentAndID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	homedir.DisableCache = true
	cwd := filepath.Join(t.TempDir(), "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex", "sessions"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex", "archived_sessions"), 0o755))

	writeJSONL(t, filepath.Join(home, ".claude", "projects", "claude-a.jsonl"),
		`{"sessionId":"claude-a","cwd":"`+cwd+`","timestamp":"2026-07-18T08:00:00Z"}`,
	)
	writeJSONL(t, filepath.Join(home, ".claude", "projects", "claude-b.jsonl"),
		`{"sessionId":"claude-b","cwd":"`+cwd+`","timestamp":"2026-07-18T08:00:00Z"}`,
	)
	writeJSONL(t, filepath.Join(home, ".codex", "sessions", "codex-new.jsonl"),
		`{"type":"session_meta","payload":{"id":"codex-new","cwd":"`+cwd+`"}}`,
		`{"timestamp":"2026-07-18T09:00:00Z"}`,
	)
	writeJSONL(t, filepath.Join(home, ".codex", "archived_sessions", "codex-old.jsonl"),
		`{"type":"session_meta","payload":{"id":"codex-old","cwd":"`+cwd+`"}}`,
		`{"timestamp":"2026-07-18T07:00:00Z"}`,
	)

	got, err := List(cwd)
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, []string{"codex-new", "claude-a", "claude-b", "codex-old"}, []string{
		got[0].ID, got[1].ID, got[2].ID, got[3].ID,
	})
}
```

- [ ] Step 2: Run the service test and verify it fails because `List` is undefined.

Run: `go test ./svc/session -run TestListSortsByLastActivityThenAgentAndID -count=1`

Expected: `FAIL`.

- [ ] Step 3: Implement `List`.

Use `agent.Agents()` and each `Agent.SessionDirs`. Dispatch known types to
`discoverClaude`, `discoverCodex`, `discoverGrok`, or
`discoverStructured`. Skip empty roots and providers without a discoverer.
Deduplicate by `(agent, ID)`; when duplicate records exist, retain the entry
with the newer `LastActivity`. Sort with:

```go
sort.Slice(sessions, func(i, j int) bool {
	if !sessions[i].LastActivity.Equal(sessions[j].LastActivity) {
		return sessions[i].LastActivity.After(sessions[j].LastActivity)
	}
	if sessions[i].Agent != sessions[j].Agent {
		return sessions[i].Agent < sessions[j].Agent
	}
	return sessions[i].ID < sessions[j].ID
})
```

- [ ] Step 4: Run the service test and verify it passes.

Run: `go test ./svc/session -run TestListSortsByLastActivityThenAgentAndID -count=1`

Expected: `PASS`.

- [ ] Step 5: Write failing formatter tests.

Assert the header, both session rows, local timestamp layout, source paths,
and empty-result message. Use a `bytes.Buffer` as the writer.

```go
func TestFormatEmptyResult(t *testing.T) {
	var out bytes.Buffer
	err := Format(&out, "/workspace/project", nil)
	require.NoError(t, err)
	assert.Equal(t, "no agent sessions found for /workspace/project\n", out.String())
}
```

- [ ] Step 6: Run formatter tests and verify they fail because `Format` is undefined.

Run: `go test ./svc/session -run TestFormat -count=1`

Expected: `FAIL`.

- [ ] Step 7: Implement and verify `Format`.

Use `text/tabwriter` with columns `AGENT`, `SESSION ID`, `STARTED`, `LAST ACTIVITY`,
and `PATH`. Render zero timestamps as `-`; otherwise use the local timezone and
layout `2006-01-02 15:04:05`. Check errors from `fmt.Fprintln` and
`tabwriter.Flush`.

Run: `go test ./svc/session -run TestFormat -count=1`

Expected: `PASS`.

### Task 7: Wire the Cobra command and synchronize documentation

Files:

- Create: `cmd/session.go`
- Modify: `cmd/root.go`
- Create: `cmd/root_test.go`
- Modify: `README.md`
- Modify: `CLAUDE.md`

Interfaces:

- `sessionCmd() *cobra.Command` registers `Use: "session"`, `cobra.NoArgs`, and a `RunE` handler.
- `newRootCmd() *cobra.Command` returns the command tree; `Execute()` uses it so registration can be tested without process exit.

- [ ] Step 1: Write the failing command registration test.

Create `cmd/root_test.go`:

```go
package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootRegistersSessionCommand(t *testing.T) {
	root := newRootCmd()
	command, _, err := root.Find([]string{"session"})
	require.NoError(t, err)
	require.NotNil(t, command)
	require.Equal(t, "session", command.Name())
	require.NoError(t, command.Args(command, nil))
	require.Error(t, command.Args(command, []string{"unexpected"}))
}
```

- [ ] Step 2: Run the command test and verify it fails because `newRootCmd` and the `session` command do not exist.

Run: `go test ./cmd -run TestRootRegistersSessionCommand -count=1`

Expected: `FAIL`.

- [ ] Step 3: Implement command wiring.

Create `cmd/session.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/bizshuk/skills/svc/session"
	"github.com/spf13/cobra"
)

func sessionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session",
		Short: "List agent sessions for the current directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("session: resolve cwd: %w", err)
			}
			items, err := session.List(cwd)
			if err != nil {
				return fmt.Errorf("session: list: %w", err)
			}
			return session.Format(cmd.OutOrStdout(), cwd, items)
		},
	}
}
```

Refactor `cmd/root.go` so command construction is testable:

```go
func newRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "skills", SilenceUsage: true}
	root.AddCommand(addCmd())
	root.AddCommand(updateCmd())
	root.AddCommand(removeCmd())
	root.AddCommand(stats.StatsCmd())
	root.AddCommand(sessionCmd())
	return root
}

func Execute() error {
	return newRootCmd().Execute()
}
```

- [ ] Step 4: Run the command test and verify it passes.

Run: `go test ./cmd -run TestRootRegistersSessionCommand -count=1`

Expected: `PASS`.

- [ ] Step 5: Add concise `README.md` and `CLAUDE.md` documentation.

Document:

```markdown
## `skills session`

列出目前資料夾中各 agent 的 session：

```bash
skills session
```

命令會讀取 `svc/agent/providers/` 的 `sessionDirs`，只列出 session
metadata 明確指向目前工作目錄的項目；缺少 session 目錄時會顯示空結果。
```

Add `session` to the command overview and add its focused test command to
`CLAUDE.md` without changing existing build conventions.

- [ ] Step 6: Run Markdown and diff checks.

Run: `git diff --check`

Expected: no whitespace errors.

### Task 8: Full verification and live command smoke test

Files:

- Verify: all changed files from Tasks 1–7

- [ ] Step 1: Run focused package tests.

Run: `go test ./svc/agent ./model ./svc/session ./cmd -count=1`

Expected: all listed packages pass.

- [ ] Step 2: Run the complete test suite.

Run: `go test ./... -count=1`

Expected: exit code `0` with no failing packages.

- [ ] Step 3: Run static analysis.

Run: `go vet ./...`

Expected: exit code `0` and no diagnostics.

- [ ] Step 4: Build the CLI to an external output path.

Run: `go build -o /tmp/skills-session .`

Expected: exit code `0`; no repository `bin/` changes.

- [ ] Step 5: Run the live command from the current repository.

Run: `/tmp/skills-session session`

Expected: a table or the exact empty-result message, with no panic and no
hard-coded path error. The command may legitimately list sessions from the
current folder's Claude, Codex, or Grok roots.

- [ ] Step 6: Verify final diff and repository state.

Run: `git diff --check && git status --short && git diff --stat`

Expected: no whitespace errors, only the intended implementation/docs files,
and no generated binaries or temporary fixtures tracked by Git.
