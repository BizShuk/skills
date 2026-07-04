# `skills add` (Go 版) 使用文件

## 簡介 (Intro)

本文件說明 `golang` 分支上的 `skills add` 命令列工具，這是以 Go 重寫 `skills add [path]` 後的版本，位於 module `github.com/bizshuk/skills`，原始入口為 `cmd/skills/main.go`。重寫後的 CLI 在功能上對應原本 TypeScript 版的 `skills add`：解析來源、走訪遞迴 plugin、以 bubbletea TUI 讓使用者挑選要安裝的 skills 與目標 agents，最後把選定的 skill 與 subagent 目錄複製到對應的安裝目錄。整體架構採單一職責分層，相關原始碼位於 `svc/source/`、`svc/manifest/`、`svc/fetch/`、`svc/discover/`、`svc/install/`、`svc/tui/`。

## Build

```bash
# 安裝到自訂目錄
GOBIN=$HOME/.local/bin go install ./cmd/skills

# 或直接在 repo 內產出執行檔
go build -o bin/skills ./cmd/skills
```

裸跑 `go build ./...` 會失敗：因為本 repo 已經有一個 `skills/` 目錄（同名的 TypeScript plugin 子樹），而 `cmd/skills/` 若走預設 `go build` 規則會嘗試在當前目錄產出同名的 `skills` 執行檔，與既有目錄衝突；把輸出路徑明確指到 `bin/skills` 或以 `GOBIN` 安裝到 `~/.local/bin` 等外部位置即可規避。

## 用法 (Usage)

主命令形式：

```bash
skills add [path]
```

`path` 可以是 GitHub shorthand、完整 GitHub URL、子路徑、GitLab URL、任意 https 連結，或本機相對／絕對路徑。下列為各類來源各一行範例：

```bash
skills add owner/repo
skills add https://github.com/owner/repo
skills add https://github.com/owner/repo/tree/main/skills/foo
skills add https://gitlab.com/group/subgroup/repo
skills add https://example.com/team
skills add ./local/plugins
skills add owner/repo#v2
```

## Flags

| Flag | 說明 |
| --- | --- |
| `--global` | 安裝到 user 層目錄（預設寫到 project 層） |
| `--agent` | 覆寫自動偵測，指定一或多個目標 agent（可重複） |
| `--depth` | 遞迴最大深度（預設 `3`） |
| `--yes` | 跳過 TUI，安裝所有偵測到的 skills 到預設 agents |

## 支援的 Agents

`svc/install/agents.go` 內建 6 個支援目標，安裝位置與偵測方式如下（`~` 為 `$HOME`）：

| Agent | project skills | user skills | project agents | user agents | 偵測方式 |
| --- | --- | --- | --- | --- | --- |
| `claude-code` | `.claude/skills` | `~/.claude/skills` | `.claude/agents` | `~/.claude/agents` | `~/.claude` 目錄存在 |
| `antigravity` | `.agents/skills` | `~/.gemini/antigravity/skills` | `.agents/agents` | `~/.gemini/antigravity/agents` | `~/.gemini/antigravity` 目錄存在 |
| `antigravity-cli` | `.agents/skills` | `~/.gemini/antigravity-cli/skills` | `.agents/agents` | `~/.gemini/antigravity-cli/agents` | `~/.gemini/antigravity-cli` 目錄存在 |
| `codex` | `.agents/skills` | `~/.codex/skills` | `.agents/agents` | `~/.codex/agents` | `~/.codex` 目錄存在 |
| `opencode` | `.agents/skills` | `~/.config/opencode/skills` | `.agents/agents` | `~/.config/opencode/agents` | `~/.config/opencode` 目錄存在 |
| `hermes-agent` | `.hermes/skills` | `~/.hermes/skills` | `.hermes/agents` | `~/.hermes/agents` | `~/.hermes` 目錄存在 |

未帶 `--agent` 時，`install.Detect()` 以各 agent 的 home 目錄是否存在判定目前已安裝的 agent，並在 TUI 中預先勾選。

## 遞迴與並行 (Recursion)

走訪位於 `svc/discover/discover.go`，採逐層 (level-by-level) BFS：root 為 depth `0`，每跨進一個 remote plugin 就 `+1`。當某個 plugin 的下一層深度大於 `--depth`（預設 `3`）時即停止走訪，不再建立對應的 Category。同一個 `owner/repo` 在單趟走訪內只會被抓取一次：`visited` set 以小寫化的 `ownerRepo` 為鍵，兼顧防環與去重；同一層內部多個 remote plugin 的 fetch 以 `errgroup` 並行執行，並以 `nextMu` 收集下一層節點，等該層全部 goroutine 完成才進入下一輪 BFS。

## 無法取得 (`unable to fetch`)

當某個 remote plugin 因網路錯誤、超過 5 次重試後仍 4xx／5xx、或 tarball 解壓失敗等原因抓不下來時，該 plugin 仍會以 `FetchOK=false`、`FetchErr="unable to fetch"` 的形式出現在 TUI 的分類樹中，方便使用者看見它「存在但這次沒抓到」；主流程不會因此中斷，後續 plugin 與本地 plugin 的走訪、安裝都不受影響。

## Project-level vs User-level

預設模式為 project level：destination 是相對於 `cwd` 的路徑（例如 `.claude/skills`、`agents/skills`、`hermes/skills` 等）。加上 `--global` 後切換為 user level：`install.Apply` 會把 skill 複製到對應 agent 的 user skills 目錄（絕對路徑，置於 `$HOME` 下），若該目錄尚未存在則於複製時一併建立。`--agent` 可在任一模式下覆寫 TUI 預設偵測，僅安裝到列出的目標 agent。

## 設計文件 (Spec)

設計規格請見 `docs/superpowers/specs/2026-07-04-skills-add-golang-design.md`。
