# CLAUDE.md

## 專案概要 (Project Summary)

本專案是以 Go 語言重寫的 `skills add/remove/install` 工具，專為不同代理器 (agent) 挑選並安裝技能 (skill)、子代理器 (subagent) 與全域規則 (global rule)。

## 常用開發指令 (Development Commands)

- 編譯執行檔：`go build -o bin/skills .`
- 執行全部單元測試：`go test ./...`
- 執行特定套件測試：`go test ./svc/agent/... -v`
- 執行 session 子命令測試：`go test ./svc/session/... -v`
- 執行 session TUI 測試：`go test ./svc/tui/... -v`
- 安裝執行檔到本機：`GOBIN=$HOME/.local/bin go install .`

## 程式碼風格與規範 (Code Style & Guidelines)

- 本專案模組名稱為 `github.com/bizshuk/skills`。
- 單一職責分層架構，業務程式碼放置於 `svc/` 目錄。

### 分層與擁有權 (Layering & Ownership)

依賴方向固定為 `cmd/` → `svc/*` → `utils/`／`model/`，不得反向。

| 目錄          | 職責                                              | 禁止事項                                    |
| ------------- | ------------------------------------------------- | ------------------------------------------- |
| `cmd/`        | cobra 命令組裝與旗標解析                          | 不放業務邏輯                                |
| `svc/fetch`   | 目標取得 (target fetching)：`Parse` + `Materialize` | —                                           |
| `svc/plugin`  | manifest 解讀與 `Catalog` 型別                    | 不得自行實作下載邏輯                        |
| `svc/discover`| plugin 樹狀走訪 (BFS) 並組出 `plugin.Catalog`     | —                                           |
| `model/`      | 純資料型別 (data types)                           | 不做 I/O、不做解析                          |
| `utils/`      | 無領域相依的通用工具 (檔案複製、描述解析、資料目錄) | 不得 import 任何 `svc/*` 或 `model`（leaf） |

- `svc/fetch` 以 target 種類分檔 (`github.go`／`gitlab.go`／`git.go`／`http.go`)。
- 專案依賴管理使用 `go.mod` 與 `go.sum`。

### 單一事實來源 (Single Source of Truth)

- `agent 安裝位置與 session 路徑`：`svc/agent/providers/*.json`，以 `go:embed` 內嵌。
  session roots 由 `sessionDirs` 設定，全域規則路徑由 `globalRulePath` 設定。
  `svc/session` 與 `svc/stat` 一律經 `agent.Agents()` 取得，不得自行硬寫路徑；
  `svc/stat` 的 `sources.*` viper key 僅作`覆寫`，不是來源。
- `重試語意 (retry)`：`github.com/bizshuk/gosdk/http`（慣例別名 `gohttp`）的
  `Retry` / `Retryable` / `IsRetryable` / `IsRetryableStatus` / `RetryPolicy`。
  `svc/fetch`、`svc/rule`、`svc/token` 共用同一份 5 次指數退避政策與同一套
  429／5xx 判定，各 package 不得自行實作重試迴圈或自行列舉可重試狀態碼。
- `應用資料目錄`：`utils.AppDataDir("skills")`（`~/.config/skills/data`）。
  `svc/update` 的 `installs.json` 與 `svc/stat` 的統計快取都落在此。
  不得直接呼叫 `config.GetAppConfigDir()` —— `config.Default` 只在 `main.go`
  呼叫，`go test` 下它回傳空字串，`filepath.Join` 會靜默產出相對路徑而寫進
  原始碼樹；`AppDataDir` 補上 XDG fallback，測試也才能用 `XDG_CONFIG_HOME`
  指向 `t.TempDir()`。
- 遵循繁體中文為主、術語併記英文圓括號的風格。
- 標示強調時一律使用 `backtick`，不使用粗體。
