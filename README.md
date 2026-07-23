# `skills` (Go 版) 使用文件

## 簡介 (Intro)

本文件說明 `golang` 分支上的 `skills` 命令列工具，這是以 Go 重寫 `skills add [path]` 後的版本，位於 module `github.com/bizshuk/skills`，原始入口為 `cmd/skills/main.go`。CLI 提供 skill 安裝、移除與 agent session 查詢：解析來源、走訪遞迴 plugin、以 bubbletea TUI 讓使用者挑選要安裝的 skills 與目標 agents，最後把選定的 skill 與 subagent 目錄複製到對應的安裝目錄。整體架構採單一職責分層，相關原始碼位於 `svc/source/`、`svc/manifest/`、`svc/fetch/`、`svc/discover/`、`svc/install/`、`svc/tui/`。

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

## `skills session`

列出目前資料夾中各 agent 的 session：

```bash
skills session
```

命令會讀取 `svc/agent/providers/` 的 session metadata source，只列出 metadata
明確指向目前工作目錄的項目。列表階段不開啟或解析 transcript；Claude 由目前
project folder 的 session 檔名列出，Codex 讀取 `state_5.sqlite` thread index，
Grok 讀取目前 project folder 的 session directories。其他 provider 在具備
project-scoped path 或外部 metadata index 前不顯示。缺少 metadata source 時會
顯示空結果。

有 session 時會進入互動式 TUI。列表上方會以 tab 分組顯示有 session 的 agents：使用 `←`/`→` 切換 agent，`↑`/`↓` 移動該 agent 的 session，按 `Enter` 或滑鼠左鍵開啟 detail；detail 畫面按 `←` 或 `Esc` 返回列表，並可用 `↑`/`↓`、`PageUp`/`PageDown` 捲動 timeline。列表 row 會先顯示 `YYYY-MM-DD HH:MM:SS`，再顯示 session ID；每個 agent 會保留自己的選取位置。完整 transcript 採 lazy loading，只在開啟選取的 session 時讀取。

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
| `codex` | `.agents/skills` | `~/.agents/skills` | `.agents/agents` | `~/.agents/agents` | `~/.codex` 目錄存在 |
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

## `skills remove`

`skills remove` 是 `add` 的對稱操作：列出所有已安裝的 skill 與 subagent
（每個 agent 都納入），讓使用者多選後從磁碟刪除，並同步清理
`installs.json`，避免下次 `skills update` 默默還原。

```bash
skills remove                 # 列出全部，進入 TUI 多選
skills remove --yes           # 跳過 TUI 與 y/N 確認，直接刪除全部
skills remove --agent claude-code    # 只處理指定 agent
skills remove --project       # 只列專案層（.claude/skills 等相對 cwd 的路徑）
skills remove --global        # 只列全域層（~/.claude/skills 等絕對路徑）
```

TUI 為單一階段的扁平清單：每列一個 `(skill|subagent)` 名稱，後接目前
裝了它的 agent 與 scope（例：`writer  [skill] — claude-code (project),
antigravity (project)`）。空白鍵切換選取、Enter 確認、Esc 取消。

確認階段會印出將被刪除的內容到 stderr，並從 stdin 讀取 `y/N`（`--yes`
跳過）。即使部分檔案刪除失敗，命令仍會回傳非零並繼續處理其餘項目；
`installs.json` 中對應的 skill/subagent 名稱也會被清掉，整個 entry
若清空則一併刪除。

## Flags

`skills remove` 支援的 flag：

| Flag        | 說明                                                  |
| ----------- | ----------------------------------------------------- |
| `--agent`   | 限縮到指定 agent（可重複），預設為全部                |
| `--global`  | 只顯示全域層安裝（與 `--project` 互斥）                |
| `--project` | 只顯示專案層安裝（與 `--global` 互斥）                  |
| `--yes`     | 自動勾選所有符合條件的項目並跳過 y/N 確認              |

## `skills token`

`skills token` 回報 prompt 的 token 數。預設用本地啟發式（每 4 個 rune 算一個 token，向下取上限），
加上 `--provider` 後改打該 provider 的 API 或本地 tokenizer 取精確值。輸出只有整數到 stdout，
錯誤到 stderr，方便直接接到 shell 或預算檢查。

```bash
skills token "hello world"
skills token "$(cat README.md)"
skills token "$(< SKILL.md)"
cat prompt.txt | skills token
cat prompt.txt | skills token --provider claude-code
echo "summarize this" | skills token --provider codex
```

支援的 `--provider` 值跟 `svc/agent/providers/` 內所有 provider 對應：

| Provider | 計數方式 | 需要的環境變數 |
| --- | --- | --- |
| `claude-code` | Anthropic `POST /v1/messages/count_tokens` | `ANTHROPIC_API_KEY`（`ANTHROPIC_BASE_URL` 可選） |
| `antigravity`, `antigravity-cli` | Gemini `countTokens` | `GEMINI_API_KEY` 或 `GOOGLE_API_KEY` |
| `codex`, `grok`, `opencode`, `hermes-agent`, `pi` | 本地 tiktoken `o200k_base` | （無） |

完整的 bash 用法範例請見 `skills token --help`。
