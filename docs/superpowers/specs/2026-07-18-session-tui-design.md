# Agent Session TUI 設計規格

## 目標

將 `skills session` 從純文字列表擴充為可互動的 terminal UI (TUI)：

- 啟動時讀取目前工作目錄的 session list。
- 使用者可用上下方向鍵移動列表游標。
- 使用滑鼠左鍵、右方向鍵或 Enter 開啟選取的 session detail。
- 使用 Escape 或左方向鍵從 detail 返回列表。
- Detail 採 `normalized timeline`；無法辨識的 record 保留 raw JSON fallback。

## 範圍與非目標

包含 Claude、Codex、Grok，以及目前 provider 設定中其他 structured session
格式的 detail 讀取。沿用既有 `sessionDirs` 與 `List(cwd)` 的 session 篩選結果。

不包含 session resume、刪除、修改、搜尋、token 統計或新增 provider path。
本次不改變 `skills session` 對無結果的 plain-text empty message。

## 格式分析

各 agent 的儲存格式不同，不能用一個 raw struct 直接反序列化所有來源：

| Agent | 儲存形式 | 關鍵欄位 | Detail 來源 |
| --- | --- | --- | --- |
| Claude | 逐行 JSONL | `type`、`timestamp`、`sessionId`、`message.role`、`message.content` | session `.jsonl` |
| Codex | 逐行 JSONL | `type`、`payload.type`、`payload.role`、`payload.content` | session `.jsonl` |
| Grok | session directory + JSON/JSONL | `summary.json`、`prompt_history.jsonl`、`session_id` | session directory 的 parent/root metadata |
| Antigravity/Hermes/OpenCode/Pi | provider-specific JSON/JSONL | nested `cwd` / `Cwd` / `workdir` 與 message-like fields | session path 下的 structured files |

Claude 與 Codex 的單行 record 也可能是 hook、progress、tool result、image 或
其他系統事件，因此 parser 必須採 best-effort normalization；未知 record 不得
讓整個 detail 讀取失敗。

## Model

在 `model/session.go` 新增純資料結構：

```go
type SessionEvent struct {
	Timestamp time.Time
	Role      string
	Kind      string
	Summary   string
	Raw       string
}

type AgentSessionDetail struct {
	Session AgentSession
	Title   string
	CWD     string
	Events  []SessionEvent
}
```

欄位語意：

- `Role` 使用 `user`、`assistant`、`tool`、`system`；無法判斷時為空字串。
- `Kind` 使用 `message`、`tool_call`、`event`、`raw`。
- `Summary` 是 TUI 可直接顯示的單行摘要，不改寫原始內容的語意。
- `Raw` 只在 normalization 不足以表達 record 時保存原始 JSON。
- `AgentSessionDetail.Session` 重用列表中的 metadata 與 source path。

## Service API 與資料流

在 `svc/session` 新增：

```go
func LoadDetail(item model.AgentSession) (model.AgentSessionDetail, error)
```

資料流：

```text
session.List(cwd)
    -> []model.AgentSession
    -> tui session list
    -> selected AgentSession
    -> session.LoadDetail(selected)
    -> model.AgentSessionDetail
    -> normalized timeline
```

`LoadDetail` 依 `item.Agent` dispatch 到 source-specific parser。列表建立時不
預先讀取完整內容；TUI 開啟 detail 時才執行 I/O。Detail 讀取命令以 Bubble Tea
`tea.Cmd` 執行，完成後以 message 回傳，讓大型 JSONL 不阻塞鍵盤處理。

所有 parser 遵循以下規則：

- source path 不存在或不可讀時回傳 wrapped error，由 TUI 顯示可返回的錯誤畫面。
- malformed record 跳過，後續 record 繼續解析。
- 已知 message/tool/event 轉成 `SessionEvent`。
- 無法辨識但仍是合法 JSON 的 record 轉成 `Kind: "raw"`，`Raw` 保存 compact JSON。
- event timestamp 優先使用 record timestamp，缺少時接受 nested timestamp 欄位。
- 所有事件依 timestamp 穩定排序；沒有 timestamp 的事件保留原始出現順序。

### Source-specific normalization

- Claude：從 top-level `type` 與 `message` 取得 role/content；`tool_use` 轉成
  `tool_call`，`tool_result` 轉成 tool event，其他合法 record 使用 event/raw。
- Codex：從 `response_item.payload` 讀 message/tool；從
  `event_msg.payload` 讀 user message、agent message 與 task event；
  `session_meta` 只補 metadata，不產生 timeline event。
- Grok：從 session parent 的 `prompt_history.jsonl` 取同一 `session_id` 的
  user prompt；`summary.json.session_summary` 作為 detail title/summary event。
- Structured providers：遞迴讀取 JSON/JSONL，使用明確 role/content/type 欄位；
  不把任意 prompt string 中出現的路徑當成 metadata。

## TUI 狀態機

在既有 `svc/tui` 新增獨立的 session model，不修改 add/remove 的 model：

```text
list
  | Right / Enter / left mouse click
  v
loading detail --error--> detail error
  |
  v
detail
  | Left / Esc
  v
list
```

狀態欄位：

- `sessions []model.AgentSession`
- `cursor` 與 `offset`：列表游標及可視範圍。
- `phase`：list、loading、detail。
- `selected`：目前 detail 對應的 session。
- `detail *model.AgentSessionDetail`、`detailOffset`：timeline 與 scroll offset。
- `loader func(model.AgentSession) (model.AgentSessionDetail, error)`：測試注入，
  production wrapper 使用 `session.LoadDetail`。

列表畫面顯示 agent、session ID、last activity、source path；detail 畫面顯示
session metadata 及每個 normalized event 的 timestamp、role、kind、summary。
terminal 寬度不足時摘要截斷並保留單行，避免 detail scroll 與 ANSI layout 互相
干擾。畫面底部顯示可用操作提示。

Mouse support 使用 Bubble Tea `tea.MouseMsg` 與 `tea.WithMouseCellMotion()`。
滑鼠點擊只在可見 session row 範圍內生效，並同步更新 cursor 後開啟 detail。

## Command wiring

`cmd/session.go` 保留 `os.Getwd` 與 `session.List` 的 command boundary：

- 有 session：呼叫 `tui.RunSession(items)`。
- 沒有 session：使用既有 `session.Format` 輸出 empty-result message，不啟動空 TUI。
- TUI error 以 `session: tui: %w` 包裝回傳。

`RunSession` 使用 `tea.NewProgram(model, tea.WithMouseCellMotion())`，不在
service 或 model package 直接寫 terminal。

## 錯誤處理

- Detail 的單筆 malformed JSON 跳過，不影響同一檔案的後續 event。
- Detail source path 讀取失敗顯示錯誤文字，Escape/左鍵仍可回到 list。
- 空 session list 保持 non-interactive plain output。
- TUI 取消或 Ctrl-C 正常結束，不回傳錯誤。

## 測試與驗收

### Model/service

- 測試 `SessionEvent` 與 `AgentSessionDetail` 欄位可完整保存 detail metadata。
- Claude fixture 驗證 user/assistant/tool normalization、malformed line skip 與 raw fallback。
- Codex fixture 驗證 `response_item`、`event_msg` 與 `session_meta` dispatch。
- Grok fixture 驗證 session ID filter、prompt history 與 summary。
- Structured fixture 驗證 nested records 與 unknown raw fallback。
- 驗證 timestamp sort 及 missing source error。

### TUI

- 初始 view 顯示 list 且 cursor 在第一筆。
- Up/Down 移動 cursor 並維持 offset。
- Right、Enter、mouse click 進入 detail loading，成功 message 後顯示 detail。
- Esc、Left 從 detail 回到 list，回到原本 cursor。
- loader error 顯示 error detail，Esc/Left 可返回。
- detail timeline 支援上下滾動。

### CLI/verification

- `skills session` 的 command registration 與 empty output regression test。
- `go test ./... -count=1`
- `go vet ./...`
- `go build -o /tmp/skills-session .`
- PTY smoke test 驗證 list、右箭頭、Esc 互動流程。

## 非目標

- 不新增 agent-specific session path 到 Go source；path 仍只在 provider JSON。
- 不改動既有 session list filtering / sorting contract。
- 不實作 session resume 或 transcript editing。
