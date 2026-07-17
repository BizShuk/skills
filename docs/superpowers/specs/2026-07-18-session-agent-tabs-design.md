# Session Agent Tabs Design

## 目標

擴充 `skills session` 的 session list TUI，讓使用者能以 agent tab 分組瀏覽目前工作目錄的 sessions：

- 在 list view 以 `←`/`→` 循環切換 agent。
- 每個 agent 記住自己的 session cursor 與 list offset。
- 以 `Enter` 或滑鼠左鍵開啟目前 tab 的 session detail。
- session row 以日期/時間先於 session ID 顯示。
- tab 與必要的狀態資訊使用一致、可辨識的色彩 render。

Detail parser、lazy loading、normalized timeline 與既有 empty-result plain output 維持不變。

## 使用者介面

有 sessions 時，list view 的 layout 為：

```text
Session list

[ codex 24 ] [ claude-code 5 ] [ grok 2 ]

←/→ switch agent    ↑/↓ move session    enter/click open    esc quit

> 2026-07-18 02:58:27  019f7107-e3fd-7080-892e-89fab8f4bfd1
  2026-07-17 02:47:59  019f6b71-3c50-7f20-bded-6688ebee0262
```

### Tab 行為

- 只顯示至少有一筆 session 的 agent。
- tab 順序沿用既有 session list 的 first-seen order；同一組輸入下順序穩定。
- tab label 顯示 agent name 與該 agent 的 session count。
- active tab 使用 agent accent color、粗體及反白背景。
- inactive tab 使用同一 agent accent color 的一般前景色與 dim 效果。
- `←` 從第一個 tab 移到最後一個；`→` 從最後一個 tab 移到第一個。
- 切換 tab 後，顯示該 agent 上次保存的 cursor 與 offset。

### Session row

- row 不重複顯示 agent name，因為 agent 已由 active tab 表示。
- row 格式固定為 `YYYY-MM-DD HH:MM:SS  SESSION_ID`。
- 日期來源為 `AgentSession.LastActivity`；缺少 timestamp 時顯示 `-`。
- session ID 維持完整值，terminal 寬度不足時以既有 rune-aware truncation 截斷。
- `↑`/`↓` 只移動目前 agent tab 的 sessions。
- `Enter` 與可見 row 的滑鼠左鍵開啟 detail loading。

## 色彩規格

使用既有 Lip Gloss dependency，不新增第三方套件。agent 使用固定 palette，避免同一 agent 在不同畫面變色：

| Agent | Accent |
| --- | --- |
| `claude-code` | purple |
| `codex` | cyan |
| `grok` | orange |
| `antigravity` | green |
| `antigravity-cli` | teal |
| `hermes-agent` | blue |
| `opencode` | pink |
| `pi` | violet |
| unknown agent | deterministic fallback blue |

Color 只套用在 tab、cursor marker 與必要的 metadata label；session ID、日期與 transcript summary 保持高對比一般文字，確保內容不依賴色彩才能讀取。ANSI escape sequence 不進入 model data。

## State model

`SessionModel` 在建立時將輸入 sessions 依 `Agent` 分組，並保留目前 list 的排序結果。新增狀態：

- `agentTabs []string`：可顯示的 agent 順序。
- `sessionsByAgent map[string][]model.AgentSession`：每個 tab 的 sessions。
- `activeAgent int`：目前 tab index。
- `cursorByAgent map[string]int`：每個 agent 的 cursor。
- `offsetByAgent map[string]int`：每個 agent 的 list offset。

目前 active agent 的 sessions、cursor 與 offset 可由 helper 取得；切換 tab 前先保存目前值，切換後載入目標 agent 值並 clamp 至有效範圍。若資料只有單一 agent，左右鍵為 no-op，不影響 Enter/click detail 行為。

Detail loading 使用目前 active agent 的 selected item；detail phase 不建立 tab navigation，`←`/`Esc` 仍返回 list 並保留原 tab/cursor。

## 相容性與錯誤處理

- `session.List`、provider parser、`session.LoadDetail` 不變更介面。
- 空 session list 仍由 command 使用 plain text output，不啟動 TUI。
- unknown agent 仍可建立 tab，使用 fallback color。
- tab 切換不觸發 filesystem I/O 或 detail loading。
- terminal width 不足時，tab label 與 row 使用 rune-aware truncation；不可產生跨行 layout。

## 測試與驗收

新增或調整 `svc/tui/session_test.go`，至少覆蓋：

- 多 agent sessions 會建立正確 tab 與 count。
- tab 依 first-seen order 顯示。
- `→`、`←` 可循環切換 agent。
- 切換後恢復每個 agent 獨立保存的 cursor 與 offset。
- 單一 agent 時左右鍵不會離開 list 或開啟 detail。
- list row 的 date/time 出現在 session ID 前。
- active/inactive tab render 包含正確 label，並套用預期 color style。
- Enter/滑鼠 click 仍只開啟目前 active tab 的 session。
- detail 的 Esc/左鍵返回後保留 active tab 與 cursor。

完整驗收維持：

```bash
go test ./...
go vet ./...
go build -o /tmp/skills-session .
```

## 非目標

- 不修改 provider session discovery 或 detail parser。
- 不新增 tab 搜尋、session filtering 或排序選項。
- 不以 color 作為唯一資訊傳達方式。
- 不改變 detail view 的返回按鍵或 lazy loading contract。
