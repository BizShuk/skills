# Session Metadata-only List 設計規格

## 目標

將 `skills session` 的列表階段改為 metadata-only discovery：列表只讀取
project path、session 檔名／目錄名、filesystem metadata 或 agent 提供的外部
metadata index。列表不得開啟或解析 session transcript；只有使用者開啟 detail
時才讀取 session 內容。

完成後，session 數量與歷史 transcript 大小不再主導列表延遲。以目前工作目錄
約 24 個 session 的實際資料為驗收案例，列表應由數秒降至次秒內完成。

## 已確認的限制

- `List(cwd)` 仍只列出可由 metadata 明確證明屬於目前工作目錄的 session。
- 列表階段不得呼叫 session transcript 的 `os.Open`、`os.ReadFile` 或 JSON parser。
- session ID 由檔名、目錄名或外部 index 取得。
- 列表日期由 filesystem `mtime` 或外部 index timestamp 取得。
- `LoadDetail` 保持 lazy loading，只有進入 detail 才解析完整內容。
- 不允許「metadata 不足時退回掃描 transcript」。
- 沒有 project-scoped path 或外部 metadata index 的 provider 暫時不顯示。
- 不在 Go source 寫死 agent home path；所有額外 index path 放在 provider JSON。

## Provider 資料來源

| Agent | 列表資料源 | cwd 證據 | Session ID | 列表時間 |
| --- | --- | --- | --- | --- |
| Claude | 目前 project folder 的頂層 `*.jsonl` | Claude project folder 名稱 | 檔名去除 `.jsonl` | 檔案 `mtime` |
| Codex | `state_5.sqlite` 的 `threads` table | `threads.cwd` | `threads.id` | `created_at_ms` / `updated_at_ms` |
| Grok | URL-encoded project folder 的直接 session 子目錄 | project folder 名稱 | session 目錄名 | 目錄 `mtime` |
| Antigravity/Hermes/OpenCode/Pi | 暫不列出 | 無可用 path/index 契約 | 不適用 | 不適用 |

Antigravity、Hermes、OpenCode、Pi 的 detail parser 保留；未來若確認外部 index 或
project-scoped path，可新增 metadata-only list adapter，不必修改 detail model。

## Provider 設定

`agent.Provider` 與 `agent.Agent` 新增 optional `SessionIndex string`。Codex provider
加入：

```json
"sessionIndex": "~/.codex/state_5.sqlite"
```

`agent.Agents()` 使用既有 home expansion 規則展開此欄位。`sessionDirs` 仍是 detail
source root 與 path-backed provider 的 discovery root；Codex 列表不再走訪其中檔案。

## Discovery 架構

不同 agent 的 list adapter 必須各自遵守相同輸出 contract：

```go
type listDiscoverer func(configured agent.Agent, cwd string) ([]model.AgentSession, error)
```

`listAgents` 維持每個 agent 一個 goroutine。單一 agent 內需要處理多個
`sessionDirs` 時仍依設定順序執行；Codex 只有一次 index query，不逐一掃描
`sessionDirs`。所有 adapter 回傳後沿用既有 `(agent, id)` 去重與排序。

### Claude

1. 將 normalized cwd 轉為 Claude project key：先轉 slash，再將 `/` 替換成 `-`。
2. 將 project key join 到 provider `sessionDirs` root。
3. 使用 `os.ReadDir` 只讀取該 project folder 的直接 children。
4. 只接受頂層 regular `*.jsonl`；不遞迴進入 `<session-id>/subagents`、
   `tool-results` 或其他 session 內部目錄。
5. 以 filename stem 作為 ID，並以 `DirEntry.Info().ModTime()` 同時填入
   `StartedAt` 與 `LastActivity`。

project folder 不存在時回傳空列表。任何其他 Claude project folder 都不得被 stat
或走訪。

### Codex

使用 `database/sql` 搭配 pure-Go `modernc.org/sqlite`，以 read-only mode 開啟
`SessionIndex`。查詢 `threads` table 中 `cwd` 等於 normalized cwd 的 rows，取得：

- `id`
- `rollout_path`
- `created_at_ms`，值為 `NULL` 時使用 `created_at * 1000`
- `updated_at_ms`，值為 `NULL` 時使用 `updated_at * 1000`

`rollout_path` 直接成為 detail source path，因此 current 與 archived session 不必再
分別掃描。session index 不存在時回傳空列表；index 存在但無法開啟或 query 時回傳
wrapped error。schema 不含必要欄位時視為不支援的 index，回傳空列表，不得退回
JSONL discovery。

SQLite connection 必須在 query 後關閉；list path 不更新 database，也不建立 table。
`mode=ro` 表示不得有 logical SQL/database/table writes；一般 live-WAL locking/read-mark
coordination 可接受，且對持續變動的 index 不得使用 `immutable=1`。

### Grok

1. 只讀取 Grok session root 的直接 project children。
2. URL decode project folder name，僅保留與 normalized cwd 相同者。
3. 只讀取該 project folder 的直接 session directories；忽略
   `prompt_history.jsonl` 與其他檔案。
4. 以 directory name 作為 ID，以 directory `mtime` 填入列表時間。

不得走進 session directory 讀取 `summary.json`、`chat_history.jsonl` 或其他內容。

## Detail 資料流

```text
metadata-only List(cwd)
    -> []model.AgentSession
    -> TUI list
    -> user opens selected session
    -> LoadDetail(selected)
    -> source-specific transcript parser
```

現有 Claude、Codex、Grok 與 structured detail normalization 不改變。列表取得的
filesystem/index 時間只用於排序與 row 顯示；detail 不需要回頭更新列表 metadata。

## 錯誤處理

- 缺少 Claude/Grok project folder 或 Codex index：該 agent 回傳空列表。
- 單一 filesystem entry 在 `Info()` 前消失：跳過該 entry，避免 session rotation
  中斷整個列表。
- Claude/Grok project root 無權讀取：回傳 wrapped error。
- Codex index lock：使用 SQLite read-only query；若仍無法讀取，回傳 wrapped error。
- 不支援的 provider：回傳空列表，不嘗試 generic structured transcript scan。
- Detail source 在列表後消失：沿用既有 detail error view，可返回列表。

## 測試

### Claude

- root 內建立目前與其他 project folder，確認只讀目前 project。
- 頂層 JSONL 放入無效或不可解析內容，仍以檔名與 `mtime` 列出。
- nested subagent JSONL 不出現在列表。
- 其他 project 即使含 matching cwd transcript 也不得列出。

### Codex

- 建立 temporary SQLite `threads` fixture，確認只列 matching cwd。
- 同時涵蓋 current、archived rollout path，確認不依賴 `sessionDirs` traversal。
- rollout file 使用無效內容，列表仍成功，證明 list 不解析 transcript。
- missing index 與 incompatible schema 回傳空列表。
- query/open failure 回傳 wrapped error。

### Grok

- URL-encoded current project 下的直接 session directory 會列出。
- 其他 project、project-level files 與 nested session files 不會列出。
- session 內容為 malformed JSON 時不影響列表。

### Service 與驗收

- 不支援 provider 不呼叫 transcript discovery，回傳空列表。
- 保留跨 agent goroutine、agent 內 `sessionDirs` 順序、去重與排序測試。
- `go test ./... -count=1`
- `go test -race ./svc/session -count=1`
- `go vet ./...`
- `go build -o /tmp/skills-session .`
- 在本 repo 實測 `skills session` list latency，目標小於 `1s`。

## 文件同步

實作完成後更新 `README.md` 與既有 session TUI spec，明確說明：

- list 是 metadata-only；
- detail 才解析 transcript；
- 目前可列出的 provider 為 Claude、Codex、Grok；
- 其他 provider 在具備 project path 或 metadata index 前不出現在列表。

## 非目標

- 不建立自有 session cache 或 background indexer。
- 不從 transcript 第一行讀取 metadata。
- 不修改 session resume、刪除或 detail rendering。
- 不為 unsupported provider 推測 cwd。
