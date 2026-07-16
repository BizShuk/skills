# 使用 gosdk/tui 取代本地表格工具設計

## 目標

移除 `utils/table.go` 的重複表格實作，改由 `github.com/bizshuk/gosdk/tui` 提供表格渲染功能，並維持現有統計報告輸出行為。

## 變更範圍

- 刪除 `utils/table.go`。
- 刪除只驗證本地實作的 `utils/table_test.go`。
- 修改 `svc/stat/format.go`，直接使用 `tui.Table` 與 `tui.Cell`。
- 不新增相容轉接層，不保留 `utils.Table` API。

## 行為與資料流

`svc/stat/format.go` 建立表格資料 → `tui.Table` → 寫入既有的 `io.Writer`。

現有的欄位、列資料、對齊方式、分隔線、Total 列與最後欄位高亮設定全部維持不變。

## 測試與驗證

- 以既有 `svc/stat` 測試驗證統計報告仍可正常產生。
- 執行 `go test ./...` 驗證完整專案。
- 執行 `go build -o bin/skills .` 驗證 CLI 可編譯；實際 entrypoint 位於 repo 根目錄的 `main.go`。

## 風險

目前解析到的 `github.com/bizshuk/gosdk/tui` API 與本地 `utils.Table` 完全一致，因此預期不需調整呼叫端行為。若 SDK 版本解析結果改變導致 API 不相容，應先更新或固定正確的 gosdk 版本，再繼續替換。
