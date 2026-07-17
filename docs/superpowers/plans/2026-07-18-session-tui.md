# Session TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 將 skills session 擴充為可從 session list 開啟 normalized timeline detail 的 Bubble Tea TUI，支援滑鼠點擊、右方向鍵進入，以及 Escape/左方向鍵返回。

**Architecture:** 保留現有 session.List(cwd) 的 lazy metadata discovery，新增 session.LoadDetail(item) 在選取時才解析完整 source record。model 提供 provider-neutral 的 SessionEvent 與 AgentSessionDetail；svc/tui 使用獨立的兩層 state machine，不修改既有 add/remove model。未知合法 JSON record 使用 compact raw fallback，malformed record 只跳過。

**Tech Stack:** Go 1.26.3、Bubble Tea 1.3.10、Lip Gloss 1.1.0、既有 svc/session parser、testify。

## Global Constraints

- 所有 agent-specific session paths 仍只存在 svc/agent/providers/*.json；不可在新 parser 加入 home-directory literal。
- model 只保存資料，不做 filesystem I/O；svc/session 擁有 detail parsing；svc/tui 擁有 terminal state 與 rendering。
- Detail 使用 lazy loading；列表建立時不可解析所有完整 transcript。
- normalized event 的 role 只使用 user、assistant、tool、system 或空字串；kind 只使用 message、tool_call、event、raw。
- malformed JSON record 必須跳過並繼續後續 record；未知但合法 JSON 必須保留 Raw。
- skills session 沒有 session 時維持既有 plain-text empty-result output，不啟動空 TUI。
- 所有 exported Go type/function 必須有以名稱開頭的 doc comment；I/O errors 必須以 %w 加上來源 context。
- 每個 production behavior 先寫 focused failing test，再寫最小 production code，並在每個 task 完成時執行 focused test。
- 不新增第三方 dependency；沿用既有 Bubble Tea/Lip Gloss。

---

## File Map

### Model

- Modify: model/session.go — 新增 SessionEvent 與 AgentSessionDetail，維持純資料。
- Modify: model/session_test.go — 驗證 detail metadata 與 event 欄位保存。

### Detail service

- Create: svc/session/detail.go — exported LoadDetail dispatch 與 source path error context。
- Create: svc/session/detail_scan.go — JSON/JSONL record scanning，保留 raw line 與 malformed skip。
- Create: svc/session/detail_normalize.go — generic nested text、timestamp、role、kind normalization helpers。
- Create: svc/session/detail_claude.go — Claude message/tool/system normalization。
- Create: svc/session/detail_codex.go — Codex response_item/event_msg normalization。
- Create: svc/session/detail_grok.go — Grok prompt_history.jsonl/summary.json normalization。
- Create: svc/session/detail_structured.go — structured provider best-effort normalization。
- Create: svc/session/detail_test.go — shared fixture helpers and service dispatch/error tests。
- Create: svc/session/detail_claude_test.go — Claude fixtures。
- Create: svc/session/detail_codex_test.go — Codex fixtures。
- Create: svc/session/detail_grok_test.go — Grok fixtures。
- Create: svc/session/detail_structured_test.go — structured/raw fallback fixtures。

### TUI and command

- Create: svc/tui/session.go — list/loading/detail model、mouse support、scrolling、RunSession。
- Create: svc/tui/session_test.go — keyboard/mouse/detail state transition tests。
- Modify: cmd/session.go — non-empty result delegates to tui.RunSession；empty result keeps session.Format。
- Modify: cmd/root_test.go — retain command registration and no-args coverage。
- Create: cmd/session_test.go — test injected empty-result command wiring without launching Bubble Tea。
- Modify: README.md — document interactive controls and detail behavior。
- Modify: CLAUDE.md — add focused TUI test command。

---

### Task 1: Extend the model with normalized detail types

**Files:**

- Modify: model/session.go
- Modify: model/session_test.go

**Interfaces:**

- Produces model.SessionEvent with Timestamp, Role, Kind, Summary, Raw.
- Produces model.AgentSessionDetail with Session, Title, CWD, Events.

- [ ] **Step 1: Write the failing model test**

Append to model/session_test.go:

~~~go
func TestAgentSessionDetailStoresNormalizedEvents(t *testing.T) {
	timestamp := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	detail := AgentSessionDetail{
		Session: AgentSession{Agent: "codex", ID: "session-1"},
		Title:   "Implement session TUI",
		CWD:     "/workspace/project",
		Events: []SessionEvent{{
			Timestamp: timestamp,
			Role:      "assistant",
			Kind:      "message",
			Summary:   "Implemented the list view",
			Raw:       "",
		}},
	}
	require.Equal(t, "session-1", detail.Session.ID)
	require.Equal(t, timestamp, detail.Events[0].Timestamp)
	require.Equal(t, "assistant", detail.Events[0].Role)
	require.Equal(t, "message", detail.Events[0].Kind)
	require.Equal(t, "Implemented the list view", detail.Events[0].Summary)
}
~~~

Add time to imports if it is not already present.

- [ ] **Step 2: Run the model test and verify it fails**

Run: go test ./model -run TestAgentSessionDetailStoresNormalizedEvents -count=1

Expected: FAIL with undefined AgentSessionDetail or SessionEvent.

- [ ] **Step 3: Add the data-only structs**

Add to model/session.go:

~~~go
// SessionEvent is one provider-neutral event in an agent session timeline.
type SessionEvent struct {
	Timestamp time.Time
	Role      string
	Kind      string
	Summary   string
	Raw       string
}

// AgentSessionDetail contains session metadata and its normalized timeline.
type AgentSessionDetail struct {
	Session AgentSession
	Title   string
	CWD     string
	Events  []SessionEvent
}
~~~

- [ ] **Step 4: Run the focused model test**

Run: gofmt -w model/session.go model/session_test.go && go test ./model -run TestAgentSessionDetailStoresNormalizedEvents -count=1

Expected: PASS.

- [ ] **Step 5: Commit the model unit**

~~~bash
git add model/session.go model/session_test.go
git commit -m "feat: add normalized session detail model"
~~~

### Task 2: Add detail record scanning and generic normalization helpers

**Files:**

- Create: svc/session/detail_scan.go
- Create: svc/session/detail_normalize.go
- Create: svc/session/detail_test.go

**Interfaces:**

- scanDetailFile(path string, visit func(record map[string]any, raw string) error) error scans JSONL lines or one JSON document, skips malformed JSON records, and passes compact raw JSON.
- normalizeGenericRecord(record map[string]any, raw string) (model.SessionEvent, bool) returns a provider-neutral event or false when there is no displayable event.
- eventTimestamp(record map[string]any) time.Time finds top-level or nested recognized timestamps using parseTimestamp.

- [ ] **Step 1: Write failing scanner and normalization tests**

Create svc/session/detail_test.go:

~~~go
func TestScanDetailFileSkipsMalformedLinesAndKeepsRawJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeJSONL(t, path,
		"{\"type\":\"message\",\"role\":\"user\",\"content\":\"hello\"}",
		"not-json",
		"{\"type\":\"future_event\",\"payload\":{\"value\":1}}",
	)

	var records []string
	err := scanDetailFile(path, func(record map[string]any, raw string) error {
		records = append(records, raw)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Contains(t, records[0], "\"content\":\"hello\"")
	assert.Contains(t, records[1], "\"future_event\"")
}

func TestNormalizeGenericRecordExtractsMessageAndRawFallback(t *testing.T) {
	message, ok := normalizeGenericRecord(map[string]any{
		"timestamp": "2026-07-18T08:00:00Z",
		"type":      "message",
		"role":      "user",
		"content":   "hello",
	}, "{\"type\":\"message\"}")
	require.True(t, ok)
	assert.Equal(t, "user", message.Role)
	assert.Equal(t, "message", message.Kind)
	assert.Equal(t, "hello", message.Summary)

	raw, ok := normalizeGenericRecord(map[string]any{
		"type":    "future_event",
		"payload": map[string]any{"value": float64(1)},
	}, "{\"type\":\"future_event\",\"payload\":{\"value\":1}}")
	require.True(t, ok)
	assert.Equal(t, "raw", raw.Kind)
	assert.Equal(t, "{\"type\":\"future_event\",\"payload\":{\"value\":1}}", raw.Raw)
}
~~~

- [ ] **Step 2: Run the tests and verify the expected failure**

Run: go test ./svc/session -run 'TestScanDetailFile|TestNormalizeGenericRecord' -count=1

Expected: FAIL because the detail scanner and normalizer do not exist.

- [ ] **Step 3: Implement scanner and generic helpers**

Use a 16 MiB bufio.Scanner buffer for JSONL. For each valid record use json.Marshal(record) as raw fallback; for JSON files accept one object or an array of objects. scanDetailFile must return filesystem errors, but ignore malformed JSON records and continue.

Generic normalization recognizes type, role, content, message, payload, timestamp, created_at, createdAt, updated_at, and updatedAt. Text extraction recurses through string content arrays and maps with text, content, or message fields; arbitrary nested objects are not dumped as summary text. A valid record with no recognized message/event fields becomes a raw event.

- [ ] **Step 4: Run focused tests and inspect formatting**

Run: gofmt -w svc/session/detail_scan.go svc/session/detail_normalize.go svc/session/detail_test.go && go test ./svc/session -run 'TestScanDetailFile|TestNormalizeGenericRecord' -count=1

Expected: PASS.

- [ ] **Step 5: Commit scanner and helpers**

~~~bash
git add svc/session/detail_scan.go svc/session/detail_normalize.go svc/session/detail_test.go
git commit -m "feat: add session detail scanning helpers"
~~~

### Task 3: Normalize Claude and Codex details

**Files:**

- Create: svc/session/detail_claude.go
- Create: svc/session/detail_codex.go
- Create: svc/session/detail_claude_test.go
- Create: svc/session/detail_codex_test.go

**Interfaces:**

- loadClaudeDetail(item model.AgentSession) (model.AgentSessionDetail, error).
- loadCodexDetail(item model.AgentSession) (model.AgentSessionDetail, error).
- Both return the original item in detail.Session and preserve source path.

- [ ] **Step 1: Write the failing Claude normalization test**

Create a temporary JSONL fixture containing:

~~~json
{"type":"user","sessionId":"s1","cwd":"/workspace/project","timestamp":"2026-07-18T08:00:00Z","message":{"role":"user","content":[{"type":"text","text":"Inspect the parser"}]}}
{"type":"assistant","sessionId":"s1","timestamp":"2026-07-18T08:01:00Z","message":{"role":"assistant","content":[{"type":"text","text":"I will inspect it"},{"type":"tool_use","name":"rg","input":{"pattern":"session"}}]}}
not-json
{"type":"system","sessionId":"s1","timestamp":"2026-07-18T08:02:00Z","subtype":"info","summary":"Finished"}
~~~

Assert the output contains user message, assistant message, tool call, system event, and no error from the malformed line. Assert the tool event has Role `tool` and Kind `tool_call`.

- [ ] **Step 2: Run the Claude test and verify it fails**

Run: go test ./svc/session -run TestLoadClaudeDetailNormalizesTimeline -count=1

Expected: FAIL because loadClaudeDetail is undefined.

- [ ] **Step 3: Implement Claude detail loading**

Scan item.Path; for each valid record use message.role when present, extract message.content text blocks, convert tool_use/tool_result to tool events, and use subtype/summary for system events. Unknown valid records delegate to generic raw fallback. Set Title from the first user summary when no stronger title exists, and set CWD from the first explicit cwd value.

- [ ] **Step 4: Run the Claude test**

Run: gofmt -w svc/session/detail_claude.go svc/session/detail_claude_test.go && go test ./svc/session -run TestLoadClaudeDetailNormalizesTimeline -count=1

Expected: PASS.

- [ ] **Step 5: Write the failing Codex normalization test**

Create a fixture with session_meta, event_msg user/agent records, response_item message, function_call, and an unknown valid response_item. Assert session_meta does not create a timeline event, messages use payload roles, function calls become tool events, and unknown JSON is raw.

Run: go test ./svc/session -run TestLoadCodexDetailNormalizesTimeline -count=1

Expected: FAIL because loadCodexDetail is undefined.

- [ ] **Step 6: Implement Codex detail loading and verify**

Read payload.type only for Codex-specific branches. Handle message, function_call, custom_tool_call, function_call_output, custom_tool_call_output, user_message, and agent_message; use generic raw fallback for unknown valid payloads. Read the session title from the first user message when available.

Run: gofmt -w svc/session/detail_codex.go svc/session/detail_codex_test.go && go test ./svc/session -run TestLoadCodexDetailNormalizesTimeline -count=1

Expected: PASS.

- [ ] **Step 7: Commit Claude/Codex detail parsers**

~~~bash
git add svc/session/detail_claude.go svc/session/detail_claude_test.go svc/session/detail_codex.go svc/session/detail_codex_test.go
git commit -m "feat: normalize claude and codex session details"
~~~

### Task 4: Normalize Grok and structured provider details

**Files:**

- Create: svc/session/detail_grok.go
- Create: svc/session/detail_structured.go
- Create: svc/session/detail_grok_test.go
- Create: svc/session/detail_structured_test.go

**Interfaces:**

- loadGrokDetail(item model.AgentSession) (model.AgentSessionDetail, error).
- loadStructuredDetail(item model.AgentSession) (model.AgentSessionDetail, error).

- [ ] **Step 1: Write the failing Grok detail test**

Build a temporary Grok layout matching the existing discoverer: item.Path is the child session directory, its parent project directory contains prompt_history.jsonl, and the session directory contains summary.json. Include prompt records for two session IDs and assert only the selected ID is returned. Assert Title comes from summary.json.session_summary, while prompts become user events.

Run: go test ./svc/session -run TestLoadGrokDetailFiltersPromptHistory -count=1

Expected: FAIL because loadGrokDetail is undefined.

- [ ] **Step 2: Implement Grok detail loading and verify**

Read summary.json from item.Path, read the parent prompt_history.jsonl, filter exact session_id == item.ID, and retain prompt timestamps. Skip malformed prompt lines. Use the summary title as a system/event summary only if non-empty.

Run: gofmt -w svc/session/detail_grok.go svc/session/detail_grok_test.go && go test ./svc/session -run TestLoadGrokDetailFiltersPromptHistory -count=1

Expected: PASS.

- [ ] **Step 3: Write the failing structured/raw fallback test**

Create a nested JSONL fixture containing an explicit role/content message and a valid future record. Assert one normalized message plus one raw event; put the target cwd only in an explicit Cwd field and verify arbitrary prompt text is not used as metadata.

Run: go test ./svc/session -run TestLoadStructuredDetailPreservesRawFallback -count=1

Expected: FAIL because loadStructuredDetail is undefined.

- [ ] **Step 4: Implement structured loading and verify**

Walk item.Path when it is a directory, otherwise scan the single file. Parse .json and .jsonl only; pass records through generic normalization and use explicit nested cwd fields to fill CWD. Do not attribute arbitrary content to the working directory.

Run: gofmt -w svc/session/detail_structured.go svc/session/detail_structured_test.go && go test ./svc/session -run TestLoadStructuredDetailPreservesRawFallback -count=1

Expected: PASS.

- [ ] **Step 5: Commit Grok/structured detail parsers**

~~~bash
git add svc/session/detail_grok.go svc/session/detail_grok_test.go svc/session/detail_structured.go svc/session/detail_structured_test.go
git commit -m "feat: normalize grok and structured session details"
~~~

### Task 5: Add public detail service dispatch

**Files:**

- Create: svc/session/detail.go
- Modify: svc/session/detail_test.go

**Interfaces:**

~~~go
// LoadDetail reads and normalizes the selected agent session transcript.
func LoadDetail(item model.AgentSession) (model.AgentSessionDetail, error)
~~~

- [ ] **Step 1: Write failing dispatch/error tests**

Add tests:

~~~go
func TestLoadDetailDispatchesByAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeJSONL(t, path, "{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"hello\"}}")
	detail, err := LoadDetail(model.AgentSession{Agent: "claude-code", ID: "s1", Path: path})
	require.NoError(t, err)
	require.Len(t, detail.Events, 1)
	assert.Equal(t, "hello", detail.Events[0].Summary)
}

func TestLoadDetailReturnsWrappedMissingPathError(t *testing.T) {
	_, err := LoadDetail(model.AgentSession{Agent: "codex", ID: "missing", Path: filepath.Join(t.TempDir(), "missing.jsonl")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "codex")
	assert.Contains(t, err.Error(), "missing.jsonl")
}
~~~

Run: go test ./svc/session -run 'TestLoadDetailDispatchesByAgent|TestLoadDetailReturnsWrappedMissingPathError' -count=1

Expected: FAIL because LoadDetail is undefined.

- [ ] **Step 2: Implement dispatch**

Dispatch claude-code, codex, grok and the structured provider names to their loaders. Reject an empty source path and return an unsupported-agent error for unknown agent names. Wrap errors as session: load <agent> <path>: %w.

- [ ] **Step 3: Run service detail tests**

Run: gofmt -w svc/session/detail.go svc/session/detail_test.go && go test ./svc/session -run 'TestLoadDetail|TestLoadClaudeDetail|TestLoadCodexDetail|TestLoadGrokDetail|TestLoadStructuredDetail' -count=1

Expected: PASS.

- [ ] **Step 4: Commit public detail service**

~~~bash
git add svc/session/detail.go svc/session/detail_test.go
git commit -m "feat: expose lazy session detail loading"
~~~

### Task 6: Build the two-screen session TUI

**Files:**

- Create: svc/tui/session.go
- Create: svc/tui/session_test.go

**Interfaces:**

~~~go
type SessionDetailLoader func(model.AgentSession) (model.AgentSessionDetail, error)

func NewSessionModel(items []model.AgentSession, loader SessionDetailLoader) SessionModel
func RunSession(items []model.AgentSession) error
~~~

SessionModel implements tea.Model. It owns list cursor/offset, phase, selected item, detail/error state, detail scroll offset, terminal dimensions, and the injected loader.

- [ ] **Step 1: Write failing list rendering and navigation tests**

Create svc/tui/session_test.go with a two-item fixture and loader:

~~~go
func TestSessionModelStartsOnListAndRendersRows(t *testing.T) {
	m := NewSessionModel(sampleSessions(), nil)
	assert.Contains(t, m.View(), "Session list")
	assert.Contains(t, m.View(), "claude-code")
	assert.Contains(t, m.View(), "session-1")
	assert.Equal(t, 0, m.cursor)
}

func TestSessionModelRightArrowLoadsDetail(t *testing.T) {
	want := model.AgentSessionDetail{
		Session: sampleSessions()[0],
		Title:   "Inspect parser",
		Events:  []model.SessionEvent{{Role: "user", Kind: "message", Summary: "hello"}},
	}
	loader := func(item model.AgentSession) (model.AgentSessionDetail, error) {
		assert.Equal(t, "session-1", item.ID)
		return want, nil
	}
	m := NewSessionModel(sampleSessions(), loader)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = mustSessionModel(t, updated)
	require.NotNil(t, cmd)
	assert.Contains(t, m.View(), "Loading")

	loaded, ok := cmd().(detailLoadedMsg)
	require.True(t, ok)
	updated, _ = m.Update(loaded)
	m = mustSessionModel(t, updated)
	assert.Contains(t, m.View(), "Inspect parser")
	assert.Contains(t, m.View(), "hello")
}
~~~

- [ ] **Step 2: Run TUI tests and verify they fail**

Run: go test ./svc/tui -run 'TestSessionModelStartsOnListAndRendersRows|TestSessionModelRightArrowLoadsDetail' -count=1

Expected: FAIL because the new session model does not exist.

- [ ] **Step 3: Implement list/loading/detail state machine**

Use phases sessionListPhase, sessionLoadingPhase, and sessionDetailPhase. The right arrow and Enter set loading state and return a command that invokes the injected loader. The command returns detailLoadedMsg{detail, err}. On success enter detail; on error render the error and keep the selected session.

List controls:

~~~go
tea.KeyUp, tea.KeyDown
tea.KeyRight, tea.KeyEnter
tea.KeyEsc
~~~

Detail controls:

~~~go
tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown
tea.KeyLeft, tea.KeyEsc
~~~

View must show metadata, source path, event timestamp/role/kind/summary, and control hints. Keep each event to one rendered line using existing truncateRune; no ANSI escape sequences belong in model data.

- [ ] **Step 4: Add mouse click behavior**

Handle tea.MouseMsg only in list phase. For a left-button press, map the visible row using a named sessionListFirstRow constant plus offset; if Y is outside the visible rows ignore it. A valid click updates cursor and uses the same detail-loading command as right arrow.

Add test:

~~~go
func TestSessionModelMouseClickLoadsClickedRow(t *testing.T) {
	var selected string
	loader := func(item model.AgentSession) (model.AgentSessionDetail, error) {
		selected = item.ID
		return model.AgentSessionDetail{Session: item}, nil
	}
	m := NewSessionModel(sampleSessions(), loader)
	updated, cmd := m.Update(tea.MouseMsg{
		X: 2, Y: sessionListFirstRow + 1,
		Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
	})
	m = mustSessionModel(t, updated)
	require.NotNil(t, cmd)
	_, _ = cmd().(detailLoadedMsg)
	assert.Equal(t, "session-2", selected)
}
~~~

- [ ] **Step 5: Add back-navigation and scroll tests**

Write tests that inject a successful detailLoadedMsg, send tea.KeyEsc and tea.KeyLeft, and assert phase returns to list, cursor remains on the selected row, and detail state is cleared. Add more events than the viewport and assert Up/Down changes detailOffset without leaving bounds.

- [ ] **Step 6: Implement RunSession and verify TUI tests**

RunSession returns immediately for an empty slice. For non-empty input, construct NewSessionModel(items, session.LoadDetail) and run:

~~~go
p := tea.NewProgram(m, tea.WithMouseCellMotion())
_, err := p.Run()
return err
~~~

Run: gofmt -w svc/tui/session.go svc/tui/session_test.go && go test ./svc/tui -run 'TestSessionModel|TestRunSession' -count=1

Expected: PASS.

- [ ] **Step 7: Commit the TUI**

~~~bash
git add svc/tui/session.go svc/tui/session_test.go
git commit -m "feat: add interactive session detail tui"
~~~

### Task 7: Wire the command and synchronize documentation

**Files:**

- Modify: cmd/session.go
- Modify: cmd/root_test.go
- Create: cmd/session_test.go
- Modify: README.md
- Modify: CLAUDE.md

**Interfaces:**

- sessionCmd remains Use: session, cobra.NoArgs, and RunE.
- RunE calls session.List(cwd), uses session.Format for empty results, and calls tui.RunSession(items) for non-empty results.

- [ ] **Step 1: Write the failing command behavior test**

Create cmd/session_test.go and test an injected command runner. The runner
signature is:

~~~go
func runSessionCommand(
	out io.Writer,
	cwd string,
	list func(string) ([]model.AgentSession, error),
	runTUI func([]model.AgentSession) error,
) error
~~~

The test supplies a list function returning nil and a TUI function that fails
the test if called, then asserts the exact existing empty-result line. This
proves the empty path does not launch an interactive program without coupling
the test to the process current directory.

Run: go test ./cmd -run 'TestRootRegistersSessionCommand|TestSessionCommand' -count=1

Expected: FAIL because runSessionCommand is undefined.

- [ ] **Step 2: Wire non-empty session results to TUI**

Import github.com/bizshuk/skills/svc/tui in cmd/session.go. Keep errors wrapped as session: list: %w and session: tui: %w; write no table after a successful interactive run.

- [ ] **Step 3: Update docs and verify command tests**

Document ↑↓, →/Enter/click, ←/Esc, and lazy detail loading in the skills session section of README.md. Add go test ./svc/tui/... -v to CLAUDE.md. Run:

gofmt -w cmd/session.go cmd/root_test.go && go test ./cmd -run 'TestRootRegistersSessionCommand|TestSessionCommand' -count=1

Expected: PASS.

- [ ] **Step 4: Commit command/docs wiring**

~~~bash
git add cmd/session.go cmd/root_test.go README.md CLAUDE.md
git commit -m "feat: wire session command to interactive tui"
~~~

### Task 8: Full verification and PTY smoke test

**Files:**

- Verify all files changed in Tasks 1–7.

- [ ] **Step 1: Run focused package tests**

Run: go test ./model ./svc/session ./svc/tui ./cmd -count=1

Expected: all packages exit with code 0.

- [ ] **Step 2: Run the complete test suite**

Run: go test ./... -count=1

Expected: exit code 0 and no failing package.

- [ ] **Step 3: Run static analysis and build**

Run:

~~~bash
go vet ./...
go build -o /tmp/skills-session .
~~~

Expected: both exit with code 0.

- [ ] **Step 4: Run non-interactive empty-result smoke test**

Use a temporary HOME with no configured session roots and run /tmp/skills-session session from a temporary directory. Assert output is:

~~~text
no agent sessions found for /absolute/current/working/directory
~~~

- [ ] **Step 5: Run PTY interactive smoke test**

Launch /tmp/skills-session session from the current repository under a PTY with mouse enabled. Verify the first screen contains Session list, send Down then Right, verify detail contains Session detail or Loading, send Escape, verify the list returns, then send Ctrl-C. Record any terminal-only limitation without changing the model tests.

- [ ] **Step 6: Inspect final diff and status**

Run:

~~~bash
git diff --check
git status --short
git diff --stat HEAD
~~~

Expected: no whitespace errors, no generated binary under the repository, and only the intended session detail/TUI implementation, tests, docs, and approved prior session-list changes.
