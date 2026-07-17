# 列出 Agent Session 實作計畫

> 給代理工作者：必要子技能：使用 `subagent-driven-development`（建議）或 `executing-plans`，逐項實作本計畫。步驟使用核取方塊 (`- [ ]`) 追蹤進度。

目標：新增 `skills session`，列出記錄的工作目錄符合目前資料夾的 agent session。

架構：Provider JSON 檔案透過 `sessionDirs` 管理所有 session 根目錄。`svc/session` 套件透過小型、來源專用的 parser 探索並正規化 session；`cmd/session.go` 只負責解析目前目錄並串接 Cobra 輸出。Model 維持純資料結構，formatter 透過注入的 `io.Writer` 寫出內容。

技術堆疊：Go 1.26.3、Cobra 1.10.2、標準函式庫 JSON/JSONL parsing、`filepath`、`text/tabwriter`，以及既有的 `svc/agent` provider embedding。

## 全域限制

- 所有 agent 專用的 session 路徑都放在 `svc/agent/providers/*.json`；不可在 `svc/session` 加入 `~/.claude`、`~/.codex` 或其他 agent home literal。
- 使用 `RunE`、`cmd.OutOrStdout()` 與 wrapped errors；library 或 command package 內不得直接結束 process。
- 比較正規化後的絕對路徑；只有具備明確工作目錄證據且符合目前資料夾的 session 才能列出。
- 缺少設定的根目錄，以及不支援或無效的 session record，應跳過而不讓其他 provider 中止。
- 新增的 exported Go type 與 function 必須提供以其名稱開頭的 doc comment。
- 遵循 red-green-refactor：每個 production behavior 都必須先有會失敗的 focused test。
- 保留既有 provider 順序與安裝行為。
- 面向使用者的 repository 文件使用繁體中文；適當處搭配英文技術術語（English technical terms），並使用 backtick 而非粗體強調。

---

### 任務 1：新增由 provider 設定的 session 根目錄

檔案：

- 修改：`svc/agent/agent.go`
- 修改：`svc/agent/agents.go`
- 修改：`svc/agent/agent_test.go`
- 修改：`svc/agent/providers/antigravity-cli.json`
- 修改：`svc/agent/providers/antigravity.json`
- 修改：`svc/agent/providers/claude-code.json`
- 修改：`svc/agent/providers/codex.json`
- 修改：`svc/agent/providers/grok.json`
- 修改：`svc/agent/providers/hermes-agent.json`
- 修改：`svc/agent/providers/opencode.json`
- 修改：`svc/agent/providers/pi.json`

介面：

- `agent.Provider.SessionDirs []string` 依設定內容原樣解碼 JSON `sessionDirs` 值。
- `agent.Agent.SessionDirs []string` 包含展開 `~/` 後的相同路徑。
- `agent.Agents()` 回傳新的 `SessionDirs` slice，讓呼叫端無法修改 embedded provider state。

- [ ] 步驟 1：撰寫會失敗的 provider schema test。

在 `TestProviderFieldsRoundTripViaJSON` 加入 loaded provider 具有非 nil session directory slice 的明確 assertion，並新增 home expansion 的 focused test：

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

在 JSON validity test 加入：

```go
assert.Contains(t, raw, "sessionDirs")
```

- [ ] 步驟 2：執行 focused test，確認它因為 `Provider` 與 `Agent` 尚未公開 `SessionDirs` 而失敗。

執行：`go test ./svc/agent -run 'TestProviderSessionDirsExpandHome|TestProviderJSONFilesAreValid' -count=1`

預期：因為 `SessionDirs` field 未定義或缺少 `sessionDirs` JSON key 而得到 `FAIL`。

- [ ] 步驟 3：新增 provider 與 translated-agent fields。

在 `Provider` 新增欄位：

```go
SessionDirs []string `json:"sessionDirs"`
```

在 `Agent` 新增欄位：

```go
SessionDirs []string // absolute session roots after `~/` expansion
```

在 `Agents()` 中，將每個 provider root 展開到新的 slice：

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

加入以下 JSON values：

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

- [ ] 步驟 4：執行 focused provider tests，確認它們通過。

執行：`go test ./svc/agent -run 'TestProviderSessionDirsExpandHome|TestProviderJSONFilesAreValid|TestProviderFieldsRoundTripViaJSON' -count=1`

預期：`PASS`。

- [ ] 步驟 5：對變更的 Go files 執行 `gofmt`，並檢查 diff。

執行：`gofmt -w svc/agent/agent.go svc/agent/agents.go svc/agent/agent_test.go && git diff --check`

預期：沒有 whitespace errors。

### 任務 2：新增 session value model

檔案：

- 建立：`model/session.go`
- 建立：`model/session_test.go`

介面：

- `model.AgentSession` 儲存 `Agent`、`ID`、`StartedAt`、`LastActivity` 與 `Path`。

- [ ] 步驟 1：撰寫會失敗的 model test。

建立 `model/session_test.go`：

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

- [ ] 步驟 2：執行 model test，確認它因為 `AgentSession` 未定義而失敗。

執行：`go test ./model -run TestAgentSessionStoresListingMetadata -count=1`

預期：得到 `undefined: AgentSession` 的 `FAIL`。

- [ ] 步驟 3：新增純資料 model。

建立 `model/session.go`：

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

- [ ] 步驟 4：執行 model test，確認它通過。

執行：`go test ./model -run TestAgentSessionStoresListingMetadata -count=1`

預期：`PASS`。

### 任務 3：實作共用的路徑、時間戳記與 JSON metadata helpers

檔案：

- 建立：`svc/session/helpers.go`
- 建立：`svc/session/helpers_test.go`

介面：

- `normalizePath(path string) (string, error)` 回傳清理後的絕對路徑，並在可能時解析 symlink。
- `samePath(left, right string) bool` 比較正規化後的路徑。
- `parseTimestamp(value any) (time.Time, bool)` 接受 RFC3339/RFC3339Nano 字串，以及 Unix seconds 或 milliseconds。
- `workingDirectories(value any) []string` 遞迴擷取 key 為 `cwd`、`Cwd`、`working_directory`、`workdir` 或 `workingDirectory` 的 values。
- `sessionMetadata` 在掃描檔案時累積 ID、符合 cwd 的證據、最早 timestamp 與最新 timestamp。

- [ ] 步驟 1：撰寫會失敗的 helper tests，涵蓋 path normalization、timestamp formats、nested cwd fields 與 invalid input。

建立 `svc/session/helpers_test.go`，包含以下案例：

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

- [ ] 步驟 2：執行 helper tests，確認符合預期的失敗。

執行：`go test ./svc/session -run 'TestSamePath|TestParseTimestamp|TestWorkingDirectories' -count=1`

預期：因為 package 與 helper functions 尚不存在而得到 `FAIL`。

- [ ] 步驟 3：只使用 standard-library types 實作 helpers。

使用 `filepath.Abs`、`filepath.Clean` 與 `filepath.EvalSymlinks`，並提供 absolute-path fallback。將大於 `1e12` 的 numeric timestamps 視為 milliseconds，較小值視為 seconds。收集支援的 working-directory keys 時，只遞迴走訪 JSON objects 與 arrays；不要檢查任意 string values 或 prompt content。

metadata accumulator 必須向 source parsers 提供以下 operations：

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

- [ ] 步驟 4：執行 helper tests，確認它們通過。

執行：`go test ./svc/session -run 'TestSamePath|TestParseTimestamp|TestWorkingDirectories' -count=1`

預期：`PASS`。

### 任務 4：新增 Claude 與 Codex session discoverers

檔案：

- 建立：`svc/session/claude.go`
- 建立：`svc/session/codex.go`
- 建立：`svc/session/claude_test.go`
- 建立：`svc/session/codex_test.go`

介面：

- `discoverClaude(root, cwd string) ([]model.AgentSession, error)` 遞迴掃描所有 `.jsonl` files，包含 `subagents/` files。
- `discoverCodex(root, cwd string) ([]model.AgentSession, error)` 遞迴掃描所有 `.jsonl` files，並使用 `session_meta.payload` metadata。
- 兩個 functions 遇到 missing root 時都靜默回傳 empty slice，並跳過 malformed lines 或沒有 matching cwd evidence 的 files。

- [ ] Step 1: Write the failing Claude fixture test.

fixture 必須包含一個 matching session、一個不同 cwd、一行 malformed line，以及一個 nested subagent file。Assert 只回傳 matching parent 與 subagent sessions，並包含 earliest/latest timestamps。

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

- [ ] 步驟 2：執行 Claude test，確認它因為 `discoverClaude` 未定義而失敗。

執行：`go test ./svc/session -run TestDiscoverClaudeFiltersByCWDAndIncludesSubagents -count=1`

預期：得到 `undefined: discoverClaude` 的 `FAIL`。

- [ ] Step 3: Implement Claude discovery.

使用 `filepath.WalkDir` 走訪 `root`。對每個 regular `.jsonl` file，以至少 10 MiB 的 scanner buffer 掃描每一行，unmarshal 成 `map[string]any`，將 `sessionId`、`cwd` 與 `timestamp` 加入 metadata；只有 metadata 具有非空 ID 且 `MatchesCWD` 為 true 時，才 emit 一個 `model.AgentSession`。`Path` 使用絕對檔案路徑，`Agent` 使用 `claude-code`。

- [ ] Step 4: Run the Claude test and verify it passes.

執行：`go test ./svc/session -run TestDiscoverClaudeFiltersByCWDAndIncludesSubagents -count=1`

預期：`PASS`。

- [ ] 步驟 5：撰寫會失敗的 Codex fixture test。

建立一般 session file 與 archived session file，將 `session_meta.payload.cwd` 設為 target。加入不同 cwd 的 session，assert 兩個 matching roots 都會回傳，並忽略 malformed lines。

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

- [ ] 步驟 6：執行 Codex test，確認它失敗。

執行：`go test ./svc/session -run TestDiscoverCodexScansArchivedRootAndUsesSessionMeta -count=1`

預期：因為 `discoverCodex` 未定義而得到 `FAIL`。

- [ ] 步驟 7：實作 Codex discovery，確認它通過。

使用與 Claude 相同的 JSONL walker，但只從 `type == "session_meta"` 的 records 讀取 `payload.id` 與 `payload.cwd`。Timestamps 可以來自檔案中的任何 valid record。使用去除 `.jsonl` 的 filename 作為 fallback ID，並使用 `codex` 作為 agent value。

執行：`go test ./svc/session -run TestDiscoverCodexScansArchivedRootAndUsesSessionMeta -count=1`

預期：`PASS`。

### 任務 5：新增 Grok 與 structured-metadata discoverers

檔案：

- 建立：`svc/session/grok.go`
- 建立：`svc/session/structured.go`
- 建立：`svc/session/grok_test.go`
- 建立：`svc/session/structured_test.go`

介面：

- `discoverGrok(root, cwd string) ([]model.AgentSession, error)` 處理 URL-escaped project directory names 與 child session directories。
- `discoverStructured(root, cwd, agentName string) ([]model.AgentSession, error)` 在存在 structured cwd metadata 時，處理 Antigravity、Hermes、OpenCode 與 Pi 的 transcript/session files。

- [ ] Step 1: Write the failing Grok fixture test.

建立一個以 `url.PathEscape(cwd)` 命名的 project directory，以及兩個 child session directories。在 `prompt_context.json` 與 `summary.json` 放入 `working_directory`、`created_at` 與 `updated_at` values。加入第二個 project root，並 assert 只有 target project 會依 descending last-activity order 回傳：

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

- [ ] 步驟 2：執行 Grok test，確認它因為 `discoverGrok` 未定義而失敗。

執行：`go test ./svc/session -run TestDiscoverGrokFiltersEscapedProjectRoot -count=1`

預期：`FAIL`。

- [ ] 步驟 3：實作 Grok discovery，確認它通過。

使用 `url.PathUnescape` decode 每個 first-level project directory，與 normalized cwd 比較，並掃描每個 child directory 的 JSON metadata。使用 child directory name 作為 session ID。將 `Path` 設為 session directory。只有 `prompt_context.working_directory` 符合 target cwd 時才接受。

執行：`go test ./svc/session -run TestDiscoverGrokFiltersEscapedProjectRoot -count=1`

預期：`PASS`。

- [ ] 步驟 4：撰寫會失敗的 structured-metadata test。

建立包含 nested `tool_calls[].args.Cwd`、timestamps，以及從 enclosing directory 推導 ID 的 transcript fixture。再加入一個 cwd 只出現在任意 prompt text 的 fixture，assert 後者會被忽略。

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

- [ ] 步驟 5：執行 structured test，確認它失敗。

執行：`go test ./svc/session -run TestDiscoverStructuredUsesExplicitCwdKeysOnly -count=1`

預期：因為 `discoverStructured` 未定義而得到 `FAIL`。

- [ ] 步驟 6：實作 structured discovery，確認它通過。

走訪設定的 roots。依最近的 session directory 分組 records：對於 `.system_generated/logs` 下的 transcript，使用 brain directory；否則使用 file path 作為 session path。依 helper 支援的 key names 遞迴 parse JSON objects，收集 timestamps，只 emit 具有明確 matching cwd evidence 與 stable ID 的 groups。忽略任意 string content 與 malformed lines。

執行：`go test ./svc/session -run TestDiscoverStructuredUsesExplicitCwdKeysOnly -count=1`

預期：`PASS`。

### 任務 6：新增 service aggregation、sorting 與 output formatting

檔案：

- 建立：`svc/session/service.go`
- 建立：`svc/session/format.go`
- 建立：`svc/session/service_test.go`
- 建立：`svc/session/format_test.go`

介面：

- `List(cwd string) ([]model.AgentSession, error)` 載入展開後的 provider session roots，並 dispatch 到 source discoverers。
- `Format(w io.Writer, cwd string, sessions []model.AgentSession) error` render stable table 或 empty-result message。

- [ ] Step 1: Write failing tests for aggregation and stable sorting.

使用包含 Claude fixture 與 Codex fixture 的 temporary `$HOME` roots，以目前 test directory 呼叫 `List`，並 assert results 依 last activity descending、接著 agent 與 ID 排序。加入 missing roots 不回傳 error 的 test。

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

- [ ] 步驟 2：執行 service test，確認它因為 `List` 未定義而失敗。

執行：`go test ./svc/session -run TestListSortsByLastActivityThenAgentAndID -count=1`

預期：`FAIL`。

- [ ] 步驟 3：實作 `List`。

使用 `agent.Agents()` 與每個 `Agent.SessionDirs`。將已知 types dispatch 到 `discoverClaude`、`discoverCodex`、`discoverGrok` 或 `discoverStructured`。跳過 empty roots 與沒有 discoverer 的 providers。依 `(agent, ID)` 去重；出現 duplicate records 時，保留 `LastActivity` 較新的 entry。使用以下方式排序：

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

執行：`go test ./svc/session -run TestListSortsByLastActivityThenAgentAndID -count=1`

預期：`PASS`。

- [ ] 步驟 5：撰寫會失敗的 formatter tests。

Assert header、兩個 session rows、local timestamp layout、source paths 與 empty-result message。使用 `bytes.Buffer` 作為 writer。

```go
func TestFormatEmptyResult(t *testing.T) {
	var out bytes.Buffer
	err := Format(&out, "/workspace/project", nil)
	require.NoError(t, err)
	assert.Equal(t, "no agent sessions found for /workspace/project\n", out.String())
}
```

- [ ] 步驟 6：執行 formatter tests，確認它們因為 `Format` 未定義而失敗。

執行：`go test ./svc/session -run TestFormat -count=1`

預期：`FAIL`。

- [ ] 步驟 7：實作並驗證 `Format`。

使用 `text/tabwriter`，欄位為 `AGENT`、`SESSION ID`、`STARTED`、`LAST ACTIVITY` 與 `PATH`。將 zero timestamps render 為 `-`；否則使用 local timezone 與 layout `2006-01-02 15:04:05`。檢查 `fmt.Fprintln` 與 `tabwriter.Flush` 的 errors。

執行：`go test ./svc/session -run TestFormat -count=1`

預期：`PASS`。

### 任務 7：串接 Cobra command 並同步文件

檔案：

- 建立：`cmd/session.go`
- 修改：`cmd/root.go`
- 建立：`cmd/root_test.go`
- 修改：`README.md`
- 修改：`CLAUDE.md`

介面：

- `sessionCmd() *cobra.Command` 註冊 `Use: "session"`、`cobra.NoArgs` 與 `RunE` handler。
- `newRootCmd() *cobra.Command` 回傳 command tree；`Execute()` 使用它，讓 registration 可以在不結束 process 的情況下測試。

- [ ] 步驟 1：撰寫會失敗的 command registration test。

建立 `cmd/root_test.go`：

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

- [ ] 步驟 2：執行 command test，確認它因為 `newRootCmd` 與 `session` command 不存在而失敗。

執行：`go test ./cmd -run TestRootRegistersSessionCommand -count=1`

預期：`FAIL`。

- [ ] 步驟 3：實作 command wiring。

建立 `cmd/session.go`：

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

重構 `cmd/root.go`，讓 command construction 可測試：

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

- [ ] 步驟 4：執行 command test，確認它通過。

執行：`go test ./cmd -run TestRootRegistersSessionCommand -count=1`

預期：`PASS`。

- [ ] 步驟 5：在 `README.md` 與 `CLAUDE.md` 新增簡潔文件。

文件內容：

```markdown
## `skills session`

列出目前資料夾中各 agent 的 session：

```bash
skills session
```

命令會讀取 `svc/agent/providers/` 的 `sessionDirs`，只列出 session
metadata 明確指向目前工作目錄的項目；缺少 session 目錄時會顯示空結果。
```

在 command overview 加入 `session`，並在 `CLAUDE.md` 加入其 focused test command；不要改變既有 build conventions。

- [ ] 步驟 6：執行 Markdown 與 diff checks。

執行：`git diff --check`

預期：沒有 whitespace errors。

### 任務 8：完整驗證與 live command smoke test

檔案：

- 驗證：任務 1–7 的所有變更檔案

- [ ] 步驟 1：執行 focused package tests。

執行：`go test ./svc/agent ./model ./svc/session ./cmd -count=1`

預期：所有列出的 packages 都通過。

- [ ] 步驟 2：執行完整 test suite。

執行：`go test ./... -count=1`

預期：exit code 為 `0`，且沒有 failing packages。

- [ ] 步驟 3：執行 static analysis。

執行：`go vet ./...`

預期：exit code 為 `0` 且沒有 diagnostics。

- [ ] 步驟 4：將 CLI build 到 external output path。

執行：`go build -o /tmp/skills-session .`

預期：exit code 為 `0`；repository `bin/` 不變更。

- [ ] 步驟 5：從目前 repository 執行 live command。

執行：`/tmp/skills-session session`

預期：輸出 table 或 exact empty-result message，不得 panic，也不得出現 hard-coded path error。命令可能合理地列出目前資料夾之 Claude、Codex 或 Grok roots 中的 sessions。

- [ ] 步驟 6：驗證 final diff 與 repository state。

執行：`git diff --check && git status --short && git diff --stat`

預期：沒有 whitespace errors，只有預期的 implementation/docs files，且 Git 沒有追蹤 generated binaries 或 temporary fixtures。
