# CLAUDE.md

## 專案概要 (Project Summary)

本專案是以 Go 語言重寫的 `skills add/remove` 工具，專為不同代理器 (agent) 挑選並安裝技能 (skill) 與子代理器 (subagent)。

## 常用開發指令 (Development Commands)

- 編譯執行檔：`go build -o bin/skills ./cmd/skills`
- 執行全部單元測試：`go test ./...`
- 執行特定套件測試：`go test ./svc/agent/... -v`
- 安裝執行檔到本機：`GOBIN=$HOME/.local/bin go install ./cmd/skills`

## 程式碼風格與規範 (Code Style & Guidelines)

- 本專案模組名稱為 `github.com/bizshuk/skills`。
- 單一職責分層架構，業務程式碼放置於 `svc/` 目錄。
- 專案依賴管理使用 `go.mod` 與 `go.sum`。
- 代理器配置採用 JSON 檔案，統一存放於 `svc/agent/providers/` 目錄中，並以 `go:embed` 內嵌。
- 遵循繁體中文為主、術語併記英文圓括號的風格。
- 標示強調時一律使用 `backtick`，不使用粗體。
