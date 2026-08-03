# Session Event Semantic Colors Design

## Context

`skills session` 的 detail view 目前以相同樣式顯示所有 timeline event。事件已由 session parser 正規化為 `message`、`tool_call`、`event` 與 `raw`，但使用者仍需閱讀完整 label 才能分辨事件用途。

## Goal

在 detail view 中依 event 的語意類型為整個 row 著色，讓 tool invocation、system event、一般對話與未解析 raw record 可以快速區分；不改變 parser、model 或 session 檔案格式。

## Non-goals

- 不新增 message type 或修改 provider parser。
- 不將 ANSI escape sequence 寫入 `model.SessionEvent` 或其他 service data。
- 不新增可由使用者設定的色彩選項；沿用固定 TUI palette。
- 不改變 detail scroll、lazy loading 或 row truncation 行為。

## Rendering design

`formatSessionEvent` 先完成 timestamp、label、summary 的 sanitization 與寬度截斷，再以 Lip Gloss 對完整 plain row 套用 foreground color。如此 ANSI escape sequence 不會參與寬度計算，也不會污染 normalized event data。

色彩依下列優先順序判斷：

1. `Kind=tool_call`：tool invocation 或 tool result，使用橘色 `208`。
2. `Kind=raw`：未知或未解析 record，使用灰色 `244`。
3. `Kind=event`、`Kind=system_event` 或 `Role=system`：system event，使用黃色 `220`。
4. `Kind=message` 搭配 `Role=user`：user message，使用藍色 `39`。
5. `Kind=message` 搭配 `Role=assistant`：assistant message，使用綠色 `42`。
6. 其他或未知組合：使用高對比 fallback 色 `81`。

`Role=system` 優先於一般 message 判斷，確保 provider 將 system record 正規化成 `Kind=message` 時仍能顯示為 system color。顏色只套用於 detail event row；session list row 與 agent tab 維持既有配色。

## Code boundaries

修改範圍限於 `svc/tui/session.go` 與其測試：

- 新增 `sessionEventAccent(model.SessionEvent) lipgloss.Color`，集中保存語意色彩判斷。
- 保留 `formatSessionEvent` 的文字格式與 truncation，只在最後套用 style。
- 以 table-driven tests 覆蓋 `tool_call`、`raw`、system event、user、assistant 與 fallback。

## Verification

- focused TUI tests 驗證每種 event type 取得預期 accent，且 row 仍保留 timestamp、label、summary。
- `go test ./svc/tui -count=1`
- `go test ./... -count=1`
- `go vet ./...`
- `go build -o /tmp/skills-session .`
- `git diff --check`
