# `skills install` 全域規則安裝設計 (Design Spec)

日期：2026-07-28
Module：`github.com/bizshuk/skills`

## 目標 (Goal)

新增 `skills install [url]`，下載一份 Markdown 全域規則，讓使用者以與
`skills add` 相同的 agent 選擇體驗，將內容安裝到所選 agent 的全域規則檔。

未提供 `url` 時使用：

```text
https://raw.githubusercontent.com/BizShuk/cc-plugin/refs/heads/master/config/CLAUDE.global.md
```

## 命令介面 (CLI)

```text
skills install
skills install <url>
skills install --agent codex
skills install --yes
```

- 位置參數允許零或一個 URL。
- 互動模式顯示所有 provider、預選本機已偵測的 agent。
- 共用同一 global rule path 且偵測狀態相同的 provider 合併成一列；若只有
  部分 provider 被偵測到則分列，確保預設只選取已偵測的 agent。
- `--agent` 限制可選 agent；`--yes` 跳過 TUI，使用明確指定或已偵測的 agent。
- 此命令永遠安裝到 user/global scope，因此不提供 `--global`。

## Provider Metadata

每個 `svc/agent/providers/*.json` 新增 `globalRulePath`，由 embedded provider
metadata 作為唯一真實來源 (single source of truth)：

| Provider | Global rule path |
| --- | --- |
| `claude-code` | `~/.claude/CLAUDE.md` |
| `antigravity` | `~/.gemini/GEMINI.md` |
| `antigravity-cli` | `~/.gemini/GEMINI.md` |
| `codex` | `~/.codex/AGENTS.md` |
| `opencode` | `~/.config/opencode/AGENTS.md` |
| `hermes-agent` | `~/.hermes/AGENTS.md` |
| `grok` | `~/.grok/rules/CLAUDE.global.md` |
| `pi` | `~/.pi/agent/AGENTS.md` |

Hermes 保留 `cc-plugin` 現行的 `~/.hermes/AGENTS.md` 相容語意，不覆寫
`SOUL.md` persona。

## 架構 (Architecture)

```text
cmd/install.go
    │
    ├── agent provider metadata + detection
    ├── tui.RunAgentSelection
    └── rule.Fetcher.Fetch → rule.Apply
```

- `svc/agent`：保存與展開 provider 的 `GlobalRulePath`。
- `svc/tui`：只負責顯示與回傳 agent 選擇，不做網路或檔案寫入。
- `svc/rule/fetch.go`：以 context-aware HTTP request 下載內容，最多嘗試五次。
- `svc/rule/install.go`：依唯一 target path 原子覆寫檔案並聚合錯誤。
- `cmd/install.go`：組合 flags、agent 選擇、下載、target grouping、安裝與輸出。

## 資料流 (Data Flow)

1. 解析零或一個 URL 與 `--agent`、`--yes`。
2. 依 `add` 語意取得候選 agent；未知 `--agent` 立即回傳錯誤。
3. 互動模式開啟 agent picker；`--yes` 直接使用明確指定或偵測結果。
4. 沒有 target 時回傳明確錯誤，不發出 HTTP request。
5. 下載一次，保留 response body 的原始 bytes。
6. 依 `GlobalRulePath` 合併 targets，同一路徑只寫一次。
7. 建立 parent directory，於同目錄寫入 temporary file，再 rename 覆蓋 target。
8. 印出每個成功 path 與最終 agent/path 數；任何 target 失敗則回傳非零。

## 覆寫與錯誤語意 (Overwrite and Errors)

- 不建立 backup。
- 既有 regular file 由 atomic rename 取代。
- 既有 symlink 由 regular file 取代，不追蹤 symlink 去修改其 source。
- 最終檔案 mode 為 `0644`。
- 非 2xx HTTP response 視為錯誤；transient transport、`429`、`5xx` 最多嘗試五次。
- 下載失敗時不寫任何 target。
- 單一 target 寫入失敗不阻止其他 target，最後以 `errors.Join` 回傳聚合錯誤。
- 不寫入 `installs.json`；重新執行 `skills install` 即為 refresh。

## 測試策略 (Testing)

- provider JSON：每個 provider 具備非空 `globalRulePath`，且 `~/` 正確展開。
- fetch：default/custom URL、exact bytes、redirect/2xx、重試、4xx、5xx、context cancellation。
- install：建立 parent、`0644`、共享 path 去重、regular overwrite、symlink replacement、
  partial failure aggregation。
- TUI：相同 path grouping、混合偵測狀態分列、detected-only defaults、
  toggle、confirm、cancel。
- command：root registration、argument/flag contract、unknown agent、`--yes` selection、
  default URL 與 output。
- 完整驗證：`go test ./...`、`go build -o bin/skills .`。

## 非目標 (Non-Goals)

- 不安裝 project-scope rules。
- 不修改 provider 自身設定檔以引用 remote URL。
- 不保存 backup 或 rollback history。
- 不把 global rule 納入 `skills update`／`installs.json`。
