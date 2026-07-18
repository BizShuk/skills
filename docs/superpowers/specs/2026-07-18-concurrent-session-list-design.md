# Concurrent Agent Session List Design

## Context

`svc/session.List` 目前依 `agent.Agents()` 順序逐一掃描 agent，並在每個 agent 內依序掃描其 `sessionDirs`。不同 agent 的 session roots 彼此獨立，因此 agent-level scan 可以安全平行化；同一 agent 的 roots 仍需保持原本的循序行為。

## Goal

讓 `List` 為每個 configured agent 啟動一個 goroutine，以縮短多 agent session discovery 的總等待時間，同時維持既有 dedupe、排序、錯誤內容與 provider-specific discovery 行為。

## Non-goals

- 不平行掃描同一 agent 的 `sessionDirs`。
- 不修改 provider JSON、`agent.Agent`、`model.AgentSession` 或 detail parser。
- 不引入 worker pool、可設定 concurrency limit 或新 dependency。
- 不改變 `List` 的 public signature 或輸出排序。
- 不在錯誤時回傳 partial session result。

## Architecture

新增 package-private discover function type 與兩個 helper：

```go
type providerDiscoverer func(agentName, root, cwd string) ([]model.AgentSession, error)

func listAgents(configured []agent.Agent, cwd string, discover providerDiscoverer) ([]model.AgentSession, error)
func scanAgent(configured agent.Agent, cwd string, discover providerDiscoverer) ([]model.AgentSession, error)
```

`List` 保留 cwd normalization，之後呼叫 `listAgents(agent.Agents(), normalizedCWD, discoverProvider)`。`listAgents` 建立與 configured agents 等長的 indexed result slice，並以 `sync.WaitGroup` 為每個 agent 啟動一個 goroutine。每個 goroutine 只寫入自己的 result slot，因此不需要 mutex；`Wait` 提供 aggregation 前的同步點。

`scanAgent` 依原順序走訪該 agent 的 `SessionDirs`：忽略空 root、呼叫 injected discoverer、收集 items；遇到第一個 root error 時停止該 agent 後續 roots，並維持既有 `session: discover <agent> at <root>: <cause>` error format。

所有 goroutine 完成後，主 goroutine 才依 configured agent 順序處理 results，將 items 合併到既有 `(Agent, ID)` dedupe map，再套用原本的 `LastActivity → Agent → ID` sorting。共享 map 不會被 goroutine 直接寫入。

## Error semantics

goroutine completion order 不影響回傳 error。`listAgents` 在 `Wait` 後依 `agent.Agents()` 原始順序尋找第一個 error；同一 agent 內則由循序 root scan 保留第一個 root error。這與原本 sequential traversal 的 observable error priority 一致。

即使某個 agent 已失敗，其他已啟動的 scans 仍會完成，因為現有 provider discoverers 沒有 `context.Context` cancellation contract。`List` 等待全部 goroutine 結束後回傳 deterministic error，不回傳 partial sessions，也不留下 blocked sender 或 goroutine leak。

## Testing

測試直接呼叫 `listAgents` 並注入 fake `providerDiscoverer`：

- 使用 barrier channels 證明兩個不同 agent 的第一個 root 可以同時進入 discoverer。
- 阻擋同一 agent 的第一個 root，確認第二個 root 不會提前開始。
- 讓不同 agent 回傳不同 errors，確認回傳值依 configured agent 順序決定，而非完成速度。
- 驗證同 agent 跨 roots 的 duplicate merge、不同 agents 同 ID 仍獨立，以及最終 sorting 維持既有結果。
- 保留現有 filesystem-backed `List` tests，並執行 `go test -race ./svc/session -count=1` 檢查 data race。

## Verification

- `go test ./svc/session -count=1`
- `go test -race ./svc/session -count=1`
- `go test ./... -count=1`
- `go vet ./...`
- `go build -o /tmp/skills-session .`
- `git diff --check`
