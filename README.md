# `skills` (Go 版) 使用文件

## 簡介 (Intro)

本文件說明 `golang` 分支上的 `skills` 命令列工具，這是以 Go 重寫 `skills add [path]` 後的版本，位於 module `github.com/bizshuk/skills`，原始入口為 repo 根目錄的 `main.go`。CLI 提供 skill、subagent 與全域規則安裝、移除，以及 agent session 查詢。

## Build

```bash
# 安裝到自訂目錄
GOBIN=$HOME/.local/bin go install .

# 或直接在 repo 內產出執行檔
go build -o bin/skills .
```

命令樹位於 `cmd/` package，repo 根目錄的 `main.go` 是薄入口；以 `-o`
指定輸出可避免在原始碼目錄產生未預期的執行檔。

## 用法 (Usage)

安裝 skill 與 subagent 的主命令形式：

```bash
skills add [path]
```

`path` 是`目標 (target)`，命令會依目標種類選擇取得方式：本機路徑直接就地讀取，
GitHub 與 GitLab 下載 repo archive，其他 git URL 以 `git clone --depth 1` 取得，
單一 https 文件則直接下載。下列為各類目標各一行範例：

```bash
skills add owner/repo
skills add https://github.com/owner/repo
skills add https://github.com/owner/repo/tree/main/skills/foo
skills add owner/repo/skills/foo
skills add https://gitlab.com/group/subgroup/repo
skills add https://git.example.com/team/repo.git
skills add https://raw.githubusercontent.com/owner/repo/main/skills/foo/SKILL.md
skills add https://example.com/pack.tar.gz
skills add ./local/plugins
skills add owner/repo#v2
```

指定`子路徑 (subpath)`時只會安裝該路徑底下的 skill，而非整個 repo；子路徑不存在
會直接報錯，不會退回整個 repo。單一 https 文件目標僅接受 `.tar.gz`／`.tgz`
archive 與 `.md` skill 文件，其餘 URL 會明確拒絕而非猜測內容。

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

## `skills install`

將全域規則 (global rule) 安裝到選定 agent 的 user-level 規則檔：

```bash
skills install
skills install https://example.com/team/AGENTS.md
skills install --agent codex
skills install --agent antigravity,antigravity-cli --yes
skills install --yes
```

URL 為可選參數；未指定時使用：

```text
https://raw.githubusercontent.com/BizShuk/cc-plugin/refs/heads/master/config/CLAUDE.global.md
```

互動模式會顯示所有已知 agent，並預先勾選本機偵測到的 agent。`--agent`
可重複使用或以逗號分隔，會把選擇清單限縮到指定 agent；搭配 `--yes` 時直接
安裝到指定 agent。只使用 `--yes` 時，則直接安裝到所有偵測到的 agent。

命令只下載來源一次，保持回應 bytes 不變，再依 provider 的
`globalRulePath` 寫入。多個 agent 共用相同路徑時只寫入一次，例如
`antigravity` 與 `antigravity-cli` 都使用 `~/.gemini/GEMINI.md`。既有檔案或
symlink 會由同目錄的原子替換 (atomic replace) 直接覆寫，不建立備份；需要
更新內容時重新執行同一命令即可。此命令不會寫入 skill 安裝紀錄
`installs.json`。

`skills install` 支援的 flag：

| Flag | 說明 |
| --- | --- |
| `--agent` | 限縮到指定 agent（可重複或以逗號分隔） |
| `--yes` | 跳過 TUI，安裝到指定或已偵測的 agent |

## 支援的 Agents

`svc/agent/providers/` 內建 8 個支援目標，安裝位置、全域規則路徑與偵測方式
如下（`~` 為 `$HOME`）：

| Agent | project skills | user skills | project agents | user agents | global rule | 偵測方式 |
| --- | --- | --- | --- | --- | --- | --- |
| `claude-code` | `.claude/skills` | `~/.claude/skills` | `.claude/agents` | `~/.claude/agents` | `~/.claude/CLAUDE.md` | `~/.claude` 目錄存在 |
| `antigravity` | `.agents/skills` | `~/.gemini/antigravity/skills` | `.agents/agents` | `~/.gemini/antigravity/agents` | `~/.gemini/GEMINI.md` | `~/.gemini/antigravity` 目錄存在 |
| `antigravity-cli` | `.agents/skills` | `~/.gemini/antigravity-cli/skills` | `.agents/agents` | `~/.gemini/antigravity-cli/agents` | `~/.gemini/GEMINI.md` | `~/.gemini/antigravity-cli` 目錄存在 |
| `codex` | `.agents/skills` | `~/.agents/skills` | `.agents/agents` | `~/.agents/agents` | `~/.codex/AGENTS.md` | `~/.codex` 目錄存在 |
| `opencode` | `.agents/skills` | `~/.config/opencode/skills` | `.agents/agents` | `~/.config/opencode/agents` | `~/.config/opencode/AGENTS.md` | `~/.config/opencode` 目錄存在 |
| `hermes-agent` | `.hermes/skills` | `~/.hermes/skills` | `.hermes/agents` | `~/.hermes/agents` | `~/.hermes/AGENTS.md` | `~/.hermes` 目錄存在 |
| `grok` | `.grok/skills` | `~/.grok/skills` | `.grok/agents` | `~/.grok/agents` | `~/.grok/rules/CLAUDE.global.md` | `~/.grok` 目錄存在 |
| `pi` | `.pi/skills` | `~/.pi/skills` | `.pi/agents` | `~/.pi/agents` | `~/.pi/agent/AGENTS.md` | `~/.pi` 目錄存在 |

未帶 `--agent` 時，`agent.Detect()` 以各 agent 的 home 目錄是否存在判定目前
已安裝的 agent，並在 TUI 中預先勾選。

## Manifest 的 `skills` 欄位

`marketplace.json` 以 `source` 指向各個 plugin 目錄，該目錄的 `plugin.json` 再以
`skills` 陣列宣告自己的 skill 從哪裡來。每個條目可以是`路徑`或`repo`：

| 寫法                                  | 判定       |
| ------------------------------------- | ---------- |
| `./skills/foo`、`../x`、絕對路徑      | 路徑       |
| `github:owner/repo`、URL、`git@...`   | repo       |
| `library/outline`（本機存在）         | 路徑       |
| `bizshuk/autop`（本機不存在）         | repo       |

`owner/repo` 與相對路徑的寫法完全相同，因此這類條目以`本機是否真的存在`裁決；
明確標記的寫法不受此影響。路徑條目逃出 plugin 目錄（`../` 或指向他處的絕對路徑）
一律丟棄。

repo 條目支援兩種 repo 形狀：repo 根目錄放 `SKILL.md`（整個 repo 就是一個
skill，名稱取自 manifest 條目），或 repo 內以慣例的 `skills/<name>/SKILL.md`
收納多個 skill。兩種形狀取得的 skill 都會併入宣告它的 plugin 之下，而非另外
掛成子分類 —— 因為 manifest 宣告的是「我的 skill」。

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
