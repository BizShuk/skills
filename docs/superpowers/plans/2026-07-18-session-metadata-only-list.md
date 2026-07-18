# Session Metadata-only List Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `skills session` list only current-project sessions without opening or parsing transcript files.

**Architecture:** Keep one goroutine per agent, but replace transcript discovery with provider-specific metadata adapters. Claude and Grok resolve the current project directly from directory layout; Codex queries its read-only SQLite thread index; unsupported providers return no list items until they expose a project path or external index. Existing detail loaders remain lazy and continue parsing only the selected session.

**Tech Stack:** Go 1.26.3, standard-library filesystem APIs, `database/sql`, `modernc.org/sqlite` v1.54.0, Testify.

## Global Constraints

- `List(cwd)` only returns sessions whose project association is proven by path or external metadata index.
- List discovery must not open or parse Claude, Codex, Grok, or structured-provider transcript files.
- No transcript fallback is allowed when metadata is absent or incompatible.
- Agent paths remain in `svc/agent/providers/*.json`; Go source must not hard-code agent home paths.
- Claude list discovery reads only the current project folder's direct `*.jsonl` children.
- Grok list discovery reads only the encoded current-project folder's direct session directories.
- Codex list discovery reads `state_5.sqlite` through a read-only pure-Go driver.
- Antigravity, Antigravity CLI, Hermes, OpenCode, and Pi return no list items for now; their detail loaders remain unchanged.
- Cross-agent scans remain concurrent; multiple `sessionDirs` within a path-backed agent remain sequential.
- Existing user changes in `docs/superpowers/specs/2026-07-18-session-tui-design.md` must be preserved and not staged accidentally.

---

## File Structure

- `svc/agent/agent.go`: embedded provider schema, including optional session index path.
- `svc/agent/agents.go`: expanded runtime provider paths.
- `svc/agent/providers/codex.json`: Codex index location.
- `svc/session/claude.go`: Claude metadata-only list adapter plus unchanged detail parser.
- `svc/session/codex.go`: Codex SQLite list adapter plus unchanged detail parser.
- `svc/session/grok.go`: Grok metadata-only list adapter plus unchanged detail parser.
- `svc/session/service.go`: per-agent source selection, concurrency, dedupe, and sorting.
- `svc/session/*_test.go`: focused RED/GREEN provider and integration tests.
- `README.md`: user-facing metadata-only behavior and supported list providers.
- `docs/superpowers/specs/2026-07-18-session-tui-design.md`: short cross-reference overriding its older list-discovery assumptions.

---

### Task 1: Add the optional provider session index

**Files:**

- Modify: `svc/agent/agent.go`
- Modify: `svc/agent/agents.go`
- Modify: `svc/agent/agent_test.go`
- Modify: `svc/agent/providers/codex.json`

**Interfaces:**

- Consumes: existing `Provider.SessionDirs`, `Agent.SessionDirs`, and `Agents()` home expansion.
- Produces: `Provider.SessionIndex string` and expanded `Agent.SessionIndex string`.

- [ ] **Step 1: Write the failing session-index expansion test**

Add to `svc/agent/agent_test.go`:

```go
func TestProviderSessionIndexExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	homedir.DisableCache = true

	for _, got := range Agents() {
		provider, ok := Find(Type(got.Type))
		require.True(t, ok)
		if got.Type != "codex" {
			assert.Empty(t, provider.SessionIndex)
			assert.Empty(t, got.SessionIndex)
			continue
		}

		assert.Equal(t, "~/.codex/state_5.sqlite", provider.SessionIndex)
		assert.Equal(t, filepath.Join(home, ".codex", "state_5.sqlite"), got.SessionIndex)
	}
}
```

Add `path/filepath` to the test imports. In `TestProviderJSONFilesAreValid`, add:

```go
if raw["type"] == "codex" {
	assert.Contains(t, raw, "sessionIndex")
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
go test ./svc/agent -run 'TestProviderSessionIndexExpandsHome|TestProviderJSONFilesAreValid' -count=1
```

Expected: compile failure because `Provider.SessionIndex` and `Agent.SessionIndex` do not exist.

- [ ] **Step 3: Implement provider and runtime fields**

Add to `Provider` in `svc/agent/agent.go`:

```go
SessionIndex string `json:"sessionIndex,omitempty"`
```

Add to `Agent` in `svc/agent/agents.go`:

```go
SessionIndex string // absolute metadata index after `~/` expansion
```

Add to the `Agent` literal in `Agents()`:

```go
SessionIndex: expand(p.SessionIndex),
```

Add to `svc/agent/providers/codex.json`:

```json
"sessionIndex": "~/.codex/state_5.sqlite",
```

- [ ] **Step 4: Format and verify GREEN**

Run:

```bash
gofmt -w svc/agent/agent.go svc/agent/agents.go svc/agent/agent_test.go
go test ./svc/agent -count=1
git diff --check
```

Expected: all commands pass.

- [ ] **Step 5: Commit the provider schema**

```bash
git add svc/agent/agent.go svc/agent/agents.go svc/agent/agent_test.go svc/agent/providers/codex.json
git commit -m "feat: configure agent session metadata index"
```

---

### Task 2: Make Claude list discovery path-only

**Files:**

- Modify: `svc/session/claude.go`
- Modify: `svc/session/claude_test.go`

**Interfaces:**

- Consumes: `discoverClaude(root, cwd string) ([]model.AgentSession, error)`.
- Produces: `claudeProjectKey(cwd string) string` and metadata-only Claude sessions.

- [ ] **Step 1: Replace the transcript-based fixture with a metadata-only fixture**

Replace `TestDiscoverClaudeFiltersByCWDAndIncludesSubagents` with:

```go
func TestDiscoverClaudeReadsOnlyCurrentProjectSessionEntries(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	projectKey := strings.ReplaceAll(filepath.ToSlash(cwd), "/", "-")
	project := filepath.Join(root, projectKey)
	otherProject := filepath.Join(root, "-other-project")
	require.NoError(t, os.MkdirAll(filepath.Join(project, "session-a", "subagents"), 0o755))
	require.NoError(t, os.MkdirAll(otherProject, 0o755))

	sessionPath := filepath.Join(project, "session-a.jsonl")
	writeJSONL(t, sessionPath, `not-session-json`)
	writeJSONL(t, filepath.Join(project, "session-a", "subagents", "agent-child.jsonl"),
		`{"sessionId":"child","cwd":"`+cwd+`"}`,
	)
	writeJSONL(t, filepath.Join(otherProject, "other.jsonl"),
		`{"sessionId":"other","cwd":"`+cwd+`"}`,
	)

	wantTime := time.Date(2026, 7, 18, 8, 5, 0, 0, time.Local)
	require.NoError(t, os.Chtimes(sessionPath, wantTime, wantTime))

	got, err := discoverClaude(root, cwd)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "session-a", got[0].ID)
	assert.Equal(t, "claude-code", got[0].Agent)
	assert.Equal(t, sessionPath, got[0].Path)
	assert.Equal(t, wantTime, got[0].StartedAt)
	assert.Equal(t, wantTime, got[0].LastActivity)
}
```

Add `strings` to the test imports.

- [ ] **Step 2: Run the test and verify RED**

Run:

```bash
go test ./svc/session -run TestDiscoverClaudeReadsOnlyCurrentProjectSessionEntries -count=1
```

Expected: FAIL because the current implementation parses JSONL and returns no item for `not-session-json`.

- [ ] **Step 3: Implement direct project-directory discovery**

Replace only the list-discovery portion at the top of `svc/session/claude.go` with:

```go
func discoverClaude(root, cwd string) ([]model.AgentSession, error) {
	projectPath := filepath.Join(root, claudeProjectKey(cwd))
	entries, err := os.ReadDir(projectPath)
	if errors.Is(err, os.ErrNotExist) {
		return []model.AgentSession{}, nil
	}
	if err != nil {
		return nil, err
	}

	sessions := make([]model.AgentSession, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}

		path := filepath.Join(projectPath, entry.Name())
		activity := info.ModTime()
		sessions = append(sessions, model.AgentSession{
			Agent:        "claude-code",
			ID:           strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			StartedAt:    activity,
			LastActivity: activity,
			Path:         path,
		})
	}
	return sessions, nil
}

func claudeProjectKey(cwd string) string {
	return strings.ReplaceAll(filepath.ToSlash(cwd), "/", "-")
}
```

Add `errors`, `os`, and `path/filepath` imports. Keep all detail functions unchanged.

- [ ] **Step 4: Format and verify GREEN**

```bash
gofmt -w svc/session/claude.go svc/session/claude_test.go
go test ./svc/session -run 'TestDiscoverClaude|TestLoadClaudeDetail' -count=1
git diff --check
```

Expected: Claude list and detail tests pass.

- [ ] **Step 5: Commit Claude discovery**

```bash
git add svc/session/claude.go svc/session/claude_test.go
git commit -m "perf: list Claude sessions from project metadata"
```

---

### Task 3: Make Grok list discovery path-only

**Files:**

- Modify: `svc/session/grok.go`
- Modify: `svc/session/grok_test.go`

**Interfaces:**

- Consumes: `discoverGrok(root, cwd string) ([]model.AgentSession, error)`.
- Produces: direct child session-directory metadata without opening Grok content.

- [ ] **Step 1: Rewrite the Grok list test around directory metadata**

Replace `TestDiscoverGrokFiltersEscapedProjectRoot` with:

```go
func TestDiscoverGrokReadsOnlyCurrentProjectSessionDirectories(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	project := filepath.Join(root, url.PathEscape(cwd))
	other := filepath.Join(root, url.PathEscape(filepath.Join(t.TempDir(), "other")))
	sessionPath := filepath.Join(project, "session-a")
	require.NoError(t, os.MkdirAll(filepath.Join(sessionPath, "nested"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(other, "session-other"), 0o755))
	writeJSONL(t, filepath.Join(sessionPath, "summary.json"), `not-json`)
	writeJSONL(t, filepath.Join(project, "prompt_history.jsonl"), `not-json`)

	wantTime := time.Date(2026, 7, 18, 8, 20, 0, 0, time.Local)
	require.NoError(t, os.Chtimes(sessionPath, wantTime, wantTime))

	got, err := discoverGrok(root, cwd)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "session-a", got[0].ID)
	assert.Equal(t, "grok", got[0].Agent)
	assert.Equal(t, sessionPath, got[0].Path)
	assert.Equal(t, wantTime, got[0].LastActivity)
}
```

Add `time` to the test imports.

- [ ] **Step 2: Run the test and verify RED**

```bash
go test ./svc/session -run TestDiscoverGrokReadsOnlyCurrentProjectSessionDirectories -count=1
```

Expected: FAIL because the current parser derives no timestamp from malformed content.

- [ ] **Step 3: Replace recursive content discovery with direct directory metadata**

Replace `discoverGrok` and remove the now-unused `walkMetadataFiles` function:

```go
func discoverGrok(root, cwd string) ([]model.AgentSession, error) {
	projectPath := filepath.Join(root, url.PathEscape(cwd))
	entries, err := os.ReadDir(projectPath)
	if errors.Is(err, os.ErrNotExist) {
		return []model.AgentSession{}, nil
	}
	if err != nil {
		return nil, err
	}

	sessions := make([]model.AgentSession, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}

		path := filepath.Join(projectPath, entry.Name())
		activity := info.ModTime()
		sessions = append(sessions, model.AgentSession{
			Agent:        "grok",
			ID:           entry.Name(),
			StartedAt:    activity,
			LastActivity: activity,
			Path:         path,
		})
	}
	return sessions, nil
}
```

Remove `io/fs` from imports. Keep Grok detail loading unchanged.

- [ ] **Step 4: Format and verify GREEN**

```bash
gofmt -w svc/session/grok.go svc/session/grok_test.go
go test ./svc/session -run 'TestDiscoverGrok|TestLoadGrokDetail' -count=1
git diff --check
```

Expected: Grok list and detail tests pass.

- [ ] **Step 5: Commit Grok discovery**

```bash
git add svc/session/grok.go svc/session/grok_test.go
git commit -m "perf: list Grok sessions from directory metadata"
```

---

### Task 4: Query the Codex thread index without reading rollouts

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `svc/session/codex.go`
- Modify: `svc/session/codex_test.go`

**Interfaces:**

- Consumes: `discoverCodex(indexPath, cwd string) ([]model.AgentSession, error)` where `indexPath` is `Agent.SessionIndex`.
- Produces: read-only SQLite thread discovery and `createCodexIndexFixture` for package integration tests.

- [ ] **Step 1: Add the approved pure-Go SQLite dependency**

```bash
go get modernc.org/sqlite@v1.54.0
```

Expected: `go.mod` and `go.sum` include the driver and its transitive dependencies.

- [ ] **Step 2: Write a failing SQLite index fixture test**

Add `database/sql` and `_ "modernc.org/sqlite"` to `svc/session/codex_test.go`, then add:

```go
func createCodexIndexFixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		rollout_path TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		cwd TEXT NOT NULL,
		created_at_ms INTEGER,
		updated_at_ms INTEGER
	)`)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func TestDiscoverCodexUsesThreadIndexWithoutReadingRollouts(t *testing.T) {
	root := t.TempDir()
	indexPath := filepath.Join(root, "state_5.sqlite")
	db := createCodexIndexFixture(t, indexPath)
	cwd := filepath.Join(root, "workspace")
	otherCWD := filepath.Join(root, "other")
	activePath := filepath.Join(root, "active.jsonl")
	archivedPath := filepath.Join(root, "archived.jsonl")
	require.NoError(t, os.WriteFile(activePath, []byte("not-json"), 0o644))
	require.NoError(t, os.WriteFile(archivedPath, []byte("not-json"), 0o644))

	_, err := db.Exec(`INSERT INTO threads
		(id, rollout_path, created_at, updated_at, cwd, created_at_ms, updated_at_ms)
		VALUES
		('active', ?, 10, 20, ?, 10001, 20001),
		('archived', ?, 30, 40, ?, NULL, NULL),
		('other', '/tmp/other.jsonl', 50, 60, ?, 50001, 60001)`,
		activePath, cwd, archivedPath, cwd, otherCWD,
	)
	require.NoError(t, err)

	got, err := discoverCodex(indexPath, cwd)
	require.NoError(t, err)
	require.Len(t, got, 2)
	byID := map[string]model.AgentSession{got[0].ID: got[0], got[1].ID: got[1]}
	assert.Equal(t, activePath, byID["active"].Path)
	assert.Equal(t, time.UnixMilli(10001), byID["active"].StartedAt)
	assert.Equal(t, time.UnixMilli(20001), byID["active"].LastActivity)
	assert.Equal(t, time.UnixMilli(30000), byID["archived"].StartedAt)
	assert.Equal(t, time.UnixMilli(40000), byID["archived"].LastActivity)
}
```

Replace the old transcript-scanning Codex discovery test. Keep all detail tests unchanged.

- [ ] **Step 3: Run the test and verify RED**

```bash
go test ./svc/session -run TestDiscoverCodexUsesThreadIndexWithoutReadingRollouts -count=1
```

Expected: FAIL because `discoverCodex` currently treats the SQLite file as a JSONL root and returns no sessions.

- [ ] **Step 4: Implement the read-only Codex index adapter**

Replace only `discoverCodex` and its list-only imports in `svc/session/codex.go`:

```go
func discoverCodex(indexPath, cwd string) ([]model.AgentSession, error) {
	info, err := os.Stat(indexPath)
	if errors.Is(err, os.ErrNotExist) {
		return []model.AgentSession{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("session index is not a regular file: %s", indexPath)
	}

	dsn := url.URL{Scheme: "file", Path: indexPath}
	query := dsn.Query()
	query.Set("mode", "ro")
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open Codex session index: %w", err)
	}
	defer func() {
		_ = db.Close() // Read-only connection has no pending writes to flush.
	}()
	db.SetMaxOpenConns(1)

	rows, err := db.Query(`SELECT id, rollout_path,
		COALESCE(created_at_ms, created_at * 1000),
		COALESCE(updated_at_ms, updated_at * 1000)
		FROM threads WHERE cwd = ?`, cwd)
	if err != nil {
		if unsupportedCodexIndexSchema(err) {
			return []model.AgentSession{}, nil
		}
		return nil, fmt.Errorf("query Codex session index: %w", err)
	}
	defer rows.Close()

	var sessions []model.AgentSession
	for rows.Next() {
		var id, path string
		var createdAtMS, updatedAtMS int64
		if err := rows.Scan(&id, &path, &createdAtMS, &updatedAtMS); err != nil {
			return nil, fmt.Errorf("scan Codex session index: %w", err)
		}
		if strings.TrimSpace(id) == "" || strings.TrimSpace(path) == "" {
			continue
		}
		sessions = append(sessions, model.AgentSession{
			Agent:        "codex",
			ID:           id,
			StartedAt:    time.UnixMilli(createdAtMS),
			LastActivity: time.UnixMilli(updatedAtMS),
			Path:         path,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Codex session index: %w", err)
	}
	return sessions, nil
}

func unsupportedCodexIndexSchema(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table: threads") ||
		strings.Contains(message, "no such column:")
}
```

Use imports `database/sql`, `errors`, `fmt`, `net/url`, `os`, `sort`, `strings`, `time`, and blank-import `_ "modernc.org/sqlite"`. Remove list-only `path/filepath` if no longer used.

- [ ] **Step 5: Add missing/incompatible/error-path tests**

Add:

```go
func TestDiscoverCodexMissingOrIncompatibleIndexReturnsEmpty(t *testing.T) {
	cwd := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	got, err := discoverCodex(missing, cwd)
	require.NoError(t, err)
	assert.Empty(t, got)

	incompatible := filepath.Join(t.TempDir(), "incompatible.sqlite")
	db, err := sql.Open("sqlite", incompatible)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE unrelated (id TEXT)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	got, err = discoverCodex(incompatible, cwd)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDiscoverCodexRejectsNonFileIndex(t *testing.T) {
	_, err := discoverCodex(t.TempDir(), t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session index is not a regular file")
}
```

- [ ] **Step 6: Format and verify GREEN**

```bash
gofmt -w svc/session/codex.go svc/session/codex_test.go
go test ./svc/session -run 'TestDiscoverCodex|TestLoadCodexDetail' -count=1
git diff --check
```

Expected: Codex list and detail tests pass.

- [ ] **Step 7: Commit Codex index discovery**

```bash
git add go.mod go.sum svc/session/codex.go svc/session/codex_test.go
git commit -m "perf: list Codex sessions from thread index"
```

---

### Task 5: Route list discovery through metadata sources only

**Files:**

- Modify: `svc/session/service.go`
- Modify: `svc/session/service_test.go`

**Interfaces:**

- Consumes: expanded `Agent.SessionIndex` and metadata-only `discoverClaude`, `discoverCodex`, and `discoverGrok`.
- Produces: source selection where `SessionIndex` replaces `SessionDirs`; unsupported providers are list-invisible.

- [ ] **Step 1: Write a failing source-selection test**

Add to `svc/session/service_test.go`:

```go
func TestScanAgentUsesSessionIndexInsteadOfSessionDirs(t *testing.T) {
	configured := agent.Agent{
		Type:         "codex",
		SessionDirs:  []string{"current", "archived"},
		SessionIndex: "state.sqlite",
	}
	var visited []string
	discover := func(agentName, source, cwd string) ([]model.AgentSession, error) {
		visited = append(visited, source)
		return nil, nil
	}

	_, err := scanAgent(configured, "/workspace", discover)
	require.NoError(t, err)
	assert.Equal(t, []string{"state.sqlite"}, visited)
}
```

Add a production dispatch test:

```go
func TestDiscoverProviderDoesNotScanUnsupportedStructuredProviders(t *testing.T) {
	root := t.TempDir()
	writeJSONL(t, filepath.Join(root, "session.jsonl"),
		`{"cwd":"/workspace","timestamp":"2026-07-18T08:00:00Z"}`,
	)

	for _, agentName := range []string{"antigravity", "antigravity-cli", "hermes-agent", "opencode", "pi"} {
		got, err := discoverProvider(agentName, root, "/workspace")
		require.NoError(t, err)
		assert.Empty(t, got, agentName)
	}
}
```

- [ ] **Step 2: Run the tests and verify RED**

```bash
go test ./svc/session -run 'TestScanAgentUsesSessionIndexInsteadOfSessionDirs|TestDiscoverProviderDoesNotScanUnsupportedStructuredProviders' -count=1
```

Expected: the first test fails because `scanAgent` visits both roots; the second fails because structured records are still parsed and listed.

- [ ] **Step 3: Select index or ordered roots in `scanAgent`**

Replace the source loop setup with:

```go
sources := configured.SessionDirs
if strings.TrimSpace(configured.SessionIndex) != "" {
	sources = []string{configured.SessionIndex}
}
for _, root := range sources {
	if strings.TrimSpace(root) == "" {
		continue
	}
	items, err := discover(agentName, root, cwd)
	if err != nil {
		return nil, fmt.Errorf("session: discover %s at %s: %w", agentName, root, err)
	}
	sessions = append(sessions, items...)
}
```

Change `discoverProvider` so unsupported providers return no list items:

```go
func discoverProvider(agentName, source, cwd string) ([]model.AgentSession, error) {
	switch agentName {
	case "claude-code":
		return discoverClaude(source, cwd)
	case "codex":
		return discoverCodex(source, cwd)
	case "grok":
		return discoverGrok(source, cwd)
	default:
		return nil, nil
	}
}
```

- [ ] **Step 4: Update the end-to-end `List` fixture**

In `TestListSortsByLastActivityThenAgentAndID`:

1. Create Claude files under `~/.claude/projects/<claudeProjectKey(cwd)>/` with arbitrary content.
2. Set Claude file mtimes to the expected sort timestamps with `os.Chtimes`.
3. Create `~/.codex/state_5.sqlite` through `createCodexIndexFixture`.
4. Insert `codex-new` and `codex-old` rows with matching cwd and expected millisecond timestamps.
5. Keep the expected order `codex-new`, `claude-a`, `claude-b`, `codex-old`.

Define the shared timestamp and use this insertion body:

```go
base := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
db := createCodexIndexFixture(t, filepath.Join(home, ".codex", "state_5.sqlite"))
_, err := db.Exec(`INSERT INTO threads
	(id, rollout_path, created_at, updated_at, cwd, created_at_ms, updated_at_ms)
	VALUES
	('codex-new', '/tmp/codex-new.jsonl', 0, 0, ?, ?, ?),
	('codex-old', '/tmp/codex-old.jsonl', 0, 0, ?, ?, ?)`,
	cwd, base.Add(time.Hour).UnixMilli(), base.Add(time.Hour).UnixMilli(),
	cwd, base.Add(-time.Hour).UnixMilli(), base.Add(-time.Hour).UnixMilli(),
)
require.NoError(t, err)
```

- [ ] **Step 5: Verify service GREEN**

```bash
gofmt -w svc/session/service.go svc/session/service_test.go
go test ./svc/session -count=1
go test -race ./svc/session -count=1
git diff --check
```

Expected: session package and race tests pass.

- [ ] **Step 6: Commit metadata-only routing**

```bash
git add svc/session/service.go svc/session/service_test.go
git commit -m "refactor: route session list through metadata sources"
```

---

### Task 6: Synchronize documentation and run final gates

**Files:**

- Modify: `README.md`
- Modify without staging pre-existing hunks: `docs/superpowers/specs/2026-07-18-session-tui-design.md`

**Interfaces:**

- Consumes: completed metadata-only list behavior.
- Produces: accurate user documentation and measured final verification.

- [ ] **Step 1: Update README session behavior**

Replace the paragraph after `skills session` with:

```markdown
命令會讀取 `svc/agent/providers/` 的 session metadata source，只列出 metadata
明確指向目前工作目錄的項目。列表階段不開啟或解析 transcript；Claude 由目前
project folder 的 session 檔名列出，Codex 讀取 `state_5.sqlite` thread index，
Grok 讀取目前 project folder 的 session directories。其他 provider 在具備
project-scoped path 或外部 metadata index 前不顯示。缺少 metadata source 時會
顯示空結果。
```

Keep the existing TUI navigation paragraph unchanged.

- [ ] **Step 2: Add an override note to the existing TUI spec**

After `## 範圍與非目標`, add:

```markdown
> Session list discovery 已由
> [`2026-07-18-session-metadata-only-list-design.md`](2026-07-18-session-metadata-only-list-design.md)
> 更新：list 僅讀 path、filesystem metadata 或外部 index；detail 才解析 transcript。
```

Do not stage the file because it contained user changes before this task. Preserve both the prior formatting changes and this cross-reference in the worktree.

- [ ] **Step 3: Run deterministic verification**

```bash
go test ./... -count=1
go test -race ./svc/session -count=1
go vet ./...
go build -o /tmp/skills-session ./cmd/skills
git diff --check
```

Expected: all commands exit `0` with no warnings.

- [ ] **Step 4: Measure the real list path without launching the TUI**

Add a temporary test file `svc/session/zz_session_latency_test.go` containing:

```go
package session

import (
	"os"
	"testing"
	"time"
)

func TestSessionListLatencyDiagnostic(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cwd = filepath.Dir(filepath.Dir(cwd))
	started := time.Now()
	items, err := List(cwd)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("sessions=%d duration=%s", len(items), time.Since(started))
}
```

Include `path/filepath` in imports, then run:

```bash
go test ./svc/session -run TestSessionListLatencyDiagnostic -count=1 -v
```

Expected: current repo sessions are listed in less than `1s`. Delete the temporary test with `apply_patch` immediately after recording the result; do not commit it.

- [ ] **Step 5: Commit only the clean documentation file**

```bash
git add README.md
git commit -m "docs: explain metadata-only session listing"
```

- [ ] **Step 6: Inspect final scope**

```bash
git status --short
git log --oneline -7
```

Expected: only `docs/superpowers/specs/2026-07-18-session-tui-design.md` remains modified, containing the user's original formatting changes plus the unstaged cross-reference. No temporary binaries, profiles, or diagnostic test files remain in the repository.
