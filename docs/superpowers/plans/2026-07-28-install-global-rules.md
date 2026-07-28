# `skills install` Global Rules Implementation Plan

> `For agentic workers:` required workflow is inline TDD execution. Subagent delegation is disabled for this task.

`Goal:` Build `skills install [url]` with an `add`-style agent picker and provider-defined global rule targets.

`Architecture:` Provider JSON owns each target path. The command selects agents, fetches one byte payload through `svc/rule`, groups agents by expanded target path, and atomically installs once per unique path.

`Tech Stack:` Go 1.26.3, Cobra, Bubble Tea, Testify, standard `net/http` and filesystem packages.

## Global Constraints

- Default URL is `https://raw.githubusercontent.com/BizShuk/cc-plugin/refs/heads/master/config/CLAUDE.global.md`.
- Existing files and symlinks are replaced directly without backup.
- Interactive selection matches `skills add`: all known agents are visible and detected agents are preselected.
- `--yes` skips TUI; `--agent` limits targets.
- HTTP transient failures receive at most five total attempts.
- The downloaded bytes are not normalized.
- `installs.json` is unchanged.

---

### Task 1: Provider global rule metadata

`Files:`

- Modify: `svc/agent/agent.go`
- Modify: `svc/agent/agents.go`
- Modify: `svc/agent/agent_test.go`
- Modify: `svc/agent/providers/*.json`

`Interfaces:`

- Produces: `Provider.GlobalRulePath string`
- Produces: `Agent.GlobalRulePath string`, expanded from `~/`

- [x] `Step 1:` Add failing assertions that every embedded provider has a non-empty
  `globalRulePath` beginning with `~/`, and that `Agents()` expands it under a test HOME.
- [x] `Step 2:` Run `go test ./svc/agent -run 'TestProviderFieldsRoundTripViaJSON|TestProviderGlobalRulePathExpandsHome' -v`;
  expect compile/assertion failure because the field does not exist.
- [x] `Step 3:` Add `GlobalRulePath` to both structs, translate it in `Agents()`, and add the
  eight approved JSON values.
- [x] `Step 4:` Re-run the targeted tests; expect PASS.

### Task 2: Rule download and atomic installation

`Files:`

- Create: `svc/rule/fetch.go`
- Create: `svc/rule/fetch_test.go`
- Create: `svc/rule/install.go`
- Create: `svc/rule/install_test.go`

`Interfaces:`

```go
const DefaultURL = "https://raw.githubusercontent.com/BizShuk/cc-plugin/refs/heads/master/config/CLAUDE.global.md"

type Fetcher struct {
    Client     *http.Client
    RetryDelay time.Duration
}

func (f Fetcher) Fetch(ctx context.Context, sourceURL string) ([]byte, error)

type Target struct {
    Path   string
    Agents []string
}

type Installed struct {
    Path   string
    Agents []string
}

func Apply(content []byte, targets []Target) ([]Installed, error)
```

- [x] `Step 1:` Add `httptest.Server` tests for exact bytes, immediate 4xx failure, transient
  5xx retry success, five-attempt exhaustion, and canceled context.
- [x] `Step 2:` Run `go test ./svc/rule -run TestFetcher -v`; expect build failure because
  `Fetcher` is missing.
- [x] `Step 3:` Implement context-aware requests, retry only transport errors, `429`, and
  `5xx`, close every body, and return contextual errors.
- [x] `Step 4:` Re-run fetch tests; expect PASS.
- [x] `Step 5:` Add failing filesystem tests for parent creation, mode `0644`, exact bytes,
  duplicate path collapse, regular-file overwrite, symlink replacement, and joined partial errors.
- [x] `Step 6:` Run `go test ./svc/rule -run TestApply -v`; expect build failure because
  `Apply` is missing.
- [x] `Step 7:` Implement same-directory temporary writes followed by rename; deduplicate
  target paths before writing and continue after individual failures.
- [x] `Step 8:` Run `go test ./svc/rule -v`; expect PASS.

### Task 3: Agent-only TUI selection

`Files:`

- Create: `svc/tui/agent.go`
- Create: `svc/tui/agent_test.go`

`Interfaces:`

```go
func RunAgentSelection(agents []agent.Agent) ([]agent.AgentType, error)
```

- [x] `Step 1:` Add model tests proving shared `GlobalRulePath` values with the same
  detection state form one row, mixed detection states default to detected agents only,
  Space toggles a row, Enter returns all selected group members, and Esc cancels.
- [x] `Step 2:` Run `go test ./svc/tui -run TestAgentSelection -v`; expect build failure
  because the agent-only model is missing.
- [x] `Step 3:` Implement a focused Bubble Tea model using the existing checkbox, cursor,
  key, and cancellation conventions; do not add filesystem or network logic.
- [x] `Step 4:` Re-run the targeted TUI tests; expect PASS.

### Task 4: Cobra command orchestration

`Files:`

- Create: `cmd/install.go`
- Create: `cmd/install_test.go`
- Modify: `cmd/root.go`
- Modify: `cmd/root_test.go`

`Interfaces:`

```go
func installCmd() *cobra.Command
```

- [x] `Step 1:` Add failing tests for root registration, zero-or-one URL, default URL,
  unknown agent rejection, detected `--yes`, explicit `--agent --yes`, shared-path grouping,
  and output counts using injected fetch/picker/apply functions.
- [x] `Step 2:` Run `go test ./cmd -run 'TestInstall|TestRootRegistersInstallCommand' -v`;
  expect failure because `installCmd` is missing and root does not register it.
- [x] `Step 3:` Implement `installCmd`, dependency-injected internal constructor, agent
  validation/selection, target grouping, one fetch, apply, per-path output, and summary.
- [x] `Step 4:` Register the command in `newRootCmd()` and rerun targeted tests; expect PASS.

### Task 5: Documentation and full verification

`Files:`

- Modify: `README.md`
- Modify: `docs/terminology.md`

- [x] `Step 1:` Document `skills install [url]`, its default URL, flags, overwrite behavior,
  provider paths, shared Antigravity target, and refresh semantics.
- [x] `Step 2:` Add terminology for `global rule` and `globalRulePath`.
- [x] `Step 3:` Run `gofmt` on changed Go files.
- [x] `Step 4:` Run `go test ./...`; expect zero failures.
- [x] `Step 5:` Run `go vet ./...`; expect zero findings.
- [x] `Step 6:` Run `go build -o bin/skills .`; expect exit code 0.
- [x] `Step 7:` Inspect `git diff --check`, `git status --short`, and the complete diff;
  confirm only scoped files changed and no generated artifact is unintentionally tracked.
