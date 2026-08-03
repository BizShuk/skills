# `skills token` 子命令實作計畫

## Context

`skills` CLI 目前缺少 token 計算工具；使用者要把 prompt 餵給 Claude/GPT/Gemini 前需要粗估或精確計算 token 數。本次新增 `skills token [prompt] --provider <type>`，無 `--provider` 時跑本地啟發式估算（不需 API key、不需網路），加上 `--provider` 後依 provider 分派：
- `claude-code` → Anthropic `POST /v1/messages/count_tokens`
- `antigravity` / `antigravity-cli` → Gemini `countTokens`
- `codex` / `grok` / `opencode` / `hermes-agent` / `pi` → 本地 tiktoken（OpenAI/xAI 相容 tokenizer，無 API 端點可用）

prompt 接受位置參數，無參數時自動讀 stdin，方便 `$(cat file)` 與管線用法。`Long` 描述欄位直接列出 bash 範例。輸出只有整數到 stdout、錯誤到 stderr，可直接接到 shell 或預算判斷。

## File Changes

### New files (6)

| Path | Responsibility |
| --- | --- |
| `cmd/token.go` | cobra 組裝：`tokenCmd()` + `resolvePrompt(cmd, args)` 處理 arg/stdin 來源；`Long` 帶 bash 範例 |
| `svc/token/token.go` | 公開 `Count(ctx, provider, prompt) (int, error)`；`httpClient` 共用 30s timeout；`withRetry` 工具（429/5xx/network 重試 5 次，200/400/800/1600/3200ms backoff，capped 5s）；`dispatch` 表（init 註冊）；`supportedProviders()` 從 `agent.LoadAll()` 產出錯誤訊息 |
| `svc/token/local.go` | `localCount(prompt)` = `ceil(runeCount/4)`；無 I/O、純函式 |
| `svc/token/anthropic.go` | `anthropicCount(ctx, prompt)` → Anthropic count_tokens；env: `ANTHROPIC_API_KEY`、選用 `ANTHROPIC_BASE_URL`；model `claude-sonnet-4-5` |
| `svc/token/gemini.go` | `geminiCount(ctx, prompt)` → Gemini countTokens；env: `GEMINI_API_KEY` 或 `GOOGLE_API_KEY`；model `gemini-2.0-flash` |
| `svc/token/openai_compat.go` | `tiktokenO200k(ctx, prompt)` → 本地 o200k_base（必要時 fallback cl100k_base），不發 HTTP |

### Modified files (3)

| Path | Change |
| --- | --- |
| `cmd/root.go` | 在 `newRootCmd()` 內 `root.AddCommand(tokenCmd())` |
| `go.mod` | 新增 `github.com/pkoukk/tiktoken-go`（latest stable） |
| `README.md` | 在 `## \`skills remove\`` 段落之後新增 `## \`skills token\`` |

### Tests (2 new files)

| Path | Coverage |
| --- | --- |
| `svc/token/token_test.go` | local 估算（含 UTF-8）、dispatch、未知 provider、tiktoken 5 種 provider、tiktoken fallback、anthropic 成功/缺 key/401/403/5xx 重試/放棄/context 取消、gemini 成功/Google_API_KEY fallback/缺 key/401/429 重試/antigravity-cli alias、`supportedProviders()`、`TestDispatchCoversAllProviders`（防新 provider 漏註冊） |
| `cmd/token_test.go` | 指令註冊、`resolvePrompt` arg/stdin/去尾換行/空拒絕/多參數拒絕 |

## Public API

```go
// svc/token/token.go
func Count(ctx context.Context, provider string, prompt string) (int, error)
```

- `provider == ""` → `localCount`
- `provider == "claude-code"` → `anthropicCount`
- `provider in {"antigravity","antigravity-cli"}` → `geminiCount`
- `provider in {"codex","grok","opencode","hermes-agent","pi"}` → `tiktokenO200k`
- 其他 → error，message 列舉所有 `agent.LoadAll()` 已知類型

`cmd/token.go` 內私有 helper：

```go
func resolvePrompt(cmd *cobra.Command, args []string) (string, error)
```

- `len(args)==1` → 回傳 `args[0]`
- `len(args)==0` → `io.ReadAll(cmd.InOrStdin())`，自動支援 `cat file | skills token`
- `len(args)>1` → error
- 結果 `strings.TrimRight(prompt, "\n")` 後若為空則 error

## Flag & Usage

- `Use: "token [prompt]"`、`Short: "Count tokens for a prompt (local estimate or provider API)"`
- Flag：`--provider, -p`（string，預設空），description 列舉全部 8 個 provider 值
- `Long` 完整字串必含 bash 範例區塊：

```
Examples:

  skills token "hello world"

  skills token "$(cat README.md)"
  skills token "$(< SKILL.md)"

  cat prompt.txt | skills token
  cat prompt.txt | skills token --provider claude-code
  echo "summarize this" | skills token --provider codex
```

## Dispatch Table

```go
// svc/token/token.go, init()
var dispatch = map[agent.Type]counter{}

func init() {
    dispatch["claude-code"]     = anthropicCount
    dispatch["antigravity"]     = geminiCount
    dispatch["antigravity-cli"] = geminiCount
    dispatch["codex"]           = tiktokenO200k
    dispatch["grok"]            = tiktokenO200k
    dispatch["opencode"]        = tiktokenO200k
    dispatch["hermes-agent"]    = tiktokenO200k
    dispatch["pi"]              = tiktokenO200k
}
```

## HTTP Client & Retry

- `httpClient = &http.Client{Timeout: 30 * time.Second}`（與 `svc/plugin/fetch.go` 60s client 區隔；count_tokens 應該較快）
- 測試透過 `setHTTPClient(httpDoer)` 注入 `httptest.NewServer.Client()`
- `withRetry`：429/5xx/network error → 重試；其他 4xx 立即失敗；attempts 上限 5（沿用 `svc/plugin/fetch.go` 的 `maxAttempts` 慣例）；backoff `200ms × 2^(n-1)`，cap 5s；遵守 `ctx.Done()`

## Tests Plan

`testing` + `testify/{require,assert}`，table-driven，`t.Setenv` / `t.TempDir` / `httptest.NewServer`。`svc/token/token_test.go` 至少涵蓋：

- `TestLocalCount*`（3 個）：rune/4、UTF-8、最少回 1
- `TestCountDispatchesEmptyProviderToLocal`
- `TestCountRejectsEmptyPrompt`
- `TestCountErrorsOnUnknownProvider`
- `TestCountSucceedsViaTiktokenO200k`（table-driven 跑 5 個 provider）
- `TestTiktokenFallbackToCl100k`
- `TestAnthropicCount*`（6 個）：success、missing key、401、403、retries 5xx、gives up after max、context cancel
- `TestGeminiCount*`（6 個）：success、GOOGLE_API_KEY fallback、missing key、401、429 retry、antigravity-cli alias
- `TestSupportedProvidersListMatchesLoadAll`
- `TestDispatchCoversAllProviders`（防止新 provider JSON 加了但 dispatch 漏註冊）

`cmd/token_test.go` 至少涵蓋：

- `TestTokenCommandRegistered`
- `TestResolvePromptFromArg` / `TestResolvePromptFromStdin` / `TestResolvePromptStripsTrailingNewline` / `TestResolvePromptRejectsEmpty` / `TestResolvePromptRejectsMultipleArgs`

## Verification

```bash
cd /Users/shuk/projects/ai/skills

go build ./...
go vet ./...

go test ./svc/token/... -v
go test ./cmd/... -v -run Token
make test
make lint

go build -o bin/skills ./cmd/skills

./bin/skills token "hello world"
./bin/skills token "$(< README.md)"
cat README.md | ./bin/skills token
./bin/skills token "x" --provider bogus          # exit 1
./bin/skills token "hello" --provider codex      # tiktoken path, no API key
ANTHROPIC_API_KEY=sk-test ./bin/skills token "hi" --provider claude-code
```

成功路徑 stdout 應只有一個整數（無裝飾），方便殼層 pipe。

## Risks

1. **`tiktoken-go` cold-start** — BPE 表第一次載入 ~1s；以 `sync.Once` 緩存，後續呼叫忽略成本
2. **`ANTHROPIC_BASE_URL` 路徑拼接** — 我們附加 `/v1/messages/count_tokens`，與官方 SDK 一致；`Long` 描述內明載此行為
3. **新 provider 漏註冊** — 任何在 `svc/agent/providers/*.json` 新增的 type 都會被 `TestDispatchCoversAllProviders` 抓出
4. **CI 不打真 API** — 測試一律 `httptest.NewServer`；上線行為靠實際 API 驗證

## Critical Files

- `cmd/token.go`
- `svc/token/token.go`
- `svc/token/local.go`
- `svc/token/anthropic.go`
- `svc/token/gemini.go`
- `svc/token/openai_compat.go`
- `svc/token/token_test.go`
- `cmd/token_test.go`
- `cmd/root.go`（修改一行）
- `README.md`（新增一節）
- `go.mod` / `go.sum`（`go mod tidy` 自動更新）
