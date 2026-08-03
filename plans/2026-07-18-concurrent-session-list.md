# Concurrent Agent Session List Implementation Plan

> For agentic workers: REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax.

Goal: Scan configured agents concurrently with one goroutine per agent while keeping each agent's sessionDirs sequential and preserving existing List behavior.

Architecture: List continues to normalize cwd, then delegates to an injected listAgents helper. listAgents launches one goroutine per agent into an indexed result slice, waits for all scans, selects errors in configured-agent order, and performs dedupe and sorting on the caller goroutine. scanAgent owns the sequential root loop and existing error format.

Tech Stack: Go 1.26.3, sync.WaitGroup, testify, Go race detector.

## Global Constraints

- Use exactly one goroutine per configured agent.
- Keep each agent's SessionDirs sequential and in configured order.
- Preserve the public List(cwd string) ([]model.AgentSession, error) signature.
- Preserve the error format: session: discover <agent> at <root>: <cause>.
- Select errors by configured agent order, not goroutine completion order.
- Dedupe by (Agent, ID), keeping the item with the latest LastActivity.
- Preserve sorting by LastActivity descending, Agent ascending, then ID ascending.
- Do not return partial sessions on error.
- Do not add context cancellation, a worker pool, a concurrency option, or a new dependency.
- Preserve the pre-existing uncommitted edit in docs/superpowers/specs/2026-07-18-session-tui-design.md.

---

### Task 1: Define concurrent scan behavior with failing tests

Files:

- Modify: svc/session/service_test.go

Interfaces:

- Produces the expected helper contract:

~~~go
type providerDiscoverer func(agentName, root, cwd string) ([]model.AgentSession, error)

func listAgents(configured []agent.Agent, cwd string, discover providerDiscoverer) ([]model.AgentSession, error)
~~~

- [ ] Step 1: Add test imports and a bounded wait helper

Add errors, sync, time, model, and agent imports. Add:

~~~go
func waitForSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}
~~~

- [ ] Step 2: Add the cross-agent concurrency test

~~~go
func TestListAgentsScansDifferentAgentsConcurrently(t *testing.T) {
	configured := []agent.Agent{
		{Type: "claude-code", SessionDirs: []string{"claude-root"}},
		{Type: "codex", SessionDirs: []string{"codex-root"}},
	}
	entered := make(chan string, len(configured))
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseScans := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(releaseScans)

	discover := func(agentName, root, cwd string) ([]model.AgentSession, error) {
		entered <- agentName
		<-release
		return nil, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := listAgents(configured, "/workspace", discover)
		done <- err
	}()

	started := make([]string, 0, len(configured))
	for range configured {
		select {
		case agentName := <-entered:
			started = append(started, agentName)
		case <-time.After(time.Second):
			releaseScans()
			t.Fatal("agents did not enter discovery concurrently")
		}
	}
	assert.ElementsMatch(t, []string{"claude-code", "codex"}, started)

	releaseScans()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("concurrent agent scan did not finish")
	}
}
~~~

- [ ] Step 3: Add the sequential roots test

~~~go
func TestListAgentsKeepsSessionDirsSequentialWithinAgent(t *testing.T) {
	configured := []agent.Agent{{
		Type:        "codex",
		SessionDirs: []string{"first-root", "second-root"},
	}}
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirstRoot := func() {
		releaseOnce.Do(func() { close(releaseFirst) })
	}
	t.Cleanup(releaseFirstRoot)

	discover := func(agentName, root, cwd string) ([]model.AgentSession, error) {
		switch root {
		case "first-root":
			close(firstStarted)
			<-releaseFirst
		case "second-root":
			close(secondStarted)
		}
		return nil, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := listAgents(configured, "/workspace", discover)
		done <- err
	}()

	waitForSignal(t, firstStarted, "first root did not start")
	select {
	case <-secondStarted:
		t.Fatal("second root started before first root completed")
	case <-time.After(50 * time.Millisecond):
	}

	releaseFirstRoot()
	waitForSignal(t, secondStarted, "second root did not start after first root completed")
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("sequential root scan did not finish")
	}
}
~~~

- [ ] Step 4: Add deterministic error and dedupe/sort tests

~~~go
func TestListAgentsReturnsFirstConfiguredAgentError(t *testing.T) {
	firstErr := errors.New("first agent failed")
	secondErr := errors.New("second agent failed")
	firstStarted := make(chan struct{})
	secondFinished := make(chan struct{})
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirstAgent := func() {
		releaseOnce.Do(func() { close(releaseFirst) })
	}
	t.Cleanup(releaseFirstAgent)

	configured := []agent.Agent{
		{Type: "claude-code", SessionDirs: []string{"claude-root"}},
		{Type: "codex", SessionDirs: []string{"codex-root"}},
	}
	discover := func(agentName, root, cwd string) ([]model.AgentSession, error) {
		if agentName == "claude-code" {
			close(firstStarted)
			<-releaseFirst
			return nil, firstErr
		}
		close(secondFinished)
		return nil, secondErr
	}
	type listResult struct {
		items []model.AgentSession
		err   error
	}
	done := make(chan listResult, 1)
	go func() {
		items, err := listAgents(configured, "/workspace", discover)
		done <- listResult{items: items, err: err}
	}()

	waitForSignal(t, firstStarted, "first configured agent did not start")
	waitForSignal(t, secondFinished, "second configured agent did not finish first")
	releaseFirstAgent()
	var result listResult
	select {
	case result = <-done:
	case <-time.After(time.Second):
		t.Fatal("deterministic error scan did not finish")
	}

	assert.Nil(t, result.items)
	require.ErrorIs(t, result.err, firstErr)
	assert.Contains(t, result.err.Error(), "session: discover claude-code at claude-root")
}

func TestListAgentsDeduplicatesAndSortsAfterConcurrentScans(t *testing.T) {
	base := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
	configured := []agent.Agent{
		{Type: "claude-code", SessionDirs: []string{"claude-old", "claude-new"}},
		{Type: "codex", SessionDirs: []string{"codex-root"}},
	}
	discover := func(agentName, root, cwd string) ([]model.AgentSession, error) {
		switch root {
		case "claude-old":
			return []model.AgentSession{{Agent: agentName, ID: "shared", LastActivity: base, Path: "old"}}, nil
		case "claude-new":
			return []model.AgentSession{
				{Agent: agentName, ID: "shared", LastActivity: base.Add(2 * time.Hour), Path: "new"},
				{Agent: agentName, ID: "claude-only", LastActivity: base.Add(time.Hour)},
			}, nil
		case "codex-root":
			return []model.AgentSession{{Agent: agentName, ID: "shared", LastActivity: base.Add(3 * time.Hour)}}, nil
		default:
			return nil, nil
		}
	}

	got, err := listAgents(configured, "/workspace", discover)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"codex/shared", "claude-code/shared", "claude-code/claude-only"}, []string{
		got[0].Agent + "/" + got[0].ID,
		got[1].Agent + "/" + got[1].ID,
		got[2].Agent + "/" + got[2].ID,
	})
	assert.Equal(t, "new", got[1].Path)
}
~~~

- [ ] Step 5: Run the focused tests and verify the expected failure

Run:

~~~
gofmt -w svc/session/service_test.go
go test ./svc/session -run 'TestListAgents' -count=1
~~~

Expected: compilation fails because listAgents and providerDiscoverer are not defined.

- [ ] Step 6: Commit the red tests

~~~
git add svc/session/service_test.go
git commit -m "test: define concurrent agent session scans"
~~~

### Task 2: Implement one goroutine per agent

Files:

- Modify: svc/session/service.go
- Test: svc/session/service_test.go

Interfaces:

- Consumes the Task 1 providerDiscoverer and listAgents test contract.
- Produces:

~~~go
type providerDiscoverer func(agentName, root, cwd string) ([]model.AgentSession, error)

type agentScanResult struct {
	items []model.AgentSession
	err   error
}

func listAgents(configured []agent.Agent, cwd string, discover providerDiscoverer) ([]model.AgentSession, error)
func scanAgent(configured agent.Agent, cwd string, discover providerDiscoverer) ([]model.AgentSession, error)
~~~

- [ ] Step 1: Delegate List after cwd normalization

Replace the sequential body after normalizePath with:

~~~go
return listAgents(agent.Agents(), normalizedCWD, discoverProvider)
~~~

- [ ] Step 2: Add agent-level goroutine orchestration

Add sync to imports and implement:

~~~go
func listAgents(configured []agent.Agent, cwd string, discover providerDiscoverer) ([]model.AgentSession, error) {
	results := make([]agentScanResult, len(configured))
	var wait sync.WaitGroup
	wait.Add(len(configured))
	for index, configuredAgent := range configured {
		go func(index int, configuredAgent agent.Agent) {
			defer wait.Done()
			results[index].items, results[index].err = scanAgent(configuredAgent, cwd, discover)
		}(index, configuredAgent)
	}
	wait.Wait()

	byKey := make(map[string]model.AgentSession)
	for _, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		for _, item := range result.items {
			key := item.Agent + "\x00" + item.ID
			previous, exists := byKey[key]
			if !exists || item.LastActivity.After(previous.LastActivity) {
				byKey[key] = item
			}
		}
	}

	sessions := make([]model.AgentSession, 0, len(byKey))
	for _, item := range byKey {
		sessions = append(sessions, item)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if !sessions[i].LastActivity.Equal(sessions[j].LastActivity) {
			return sessions[i].LastActivity.After(sessions[j].LastActivity)
		}
		if sessions[i].Agent != sessions[j].Agent {
			return sessions[i].Agent < sessions[j].Agent
		}
		return sessions[i].ID < sessions[j].ID
	})
	return sessions, nil
}
~~~

- [ ] Step 3: Add the sequential per-agent scanner

~~~go
func scanAgent(configured agent.Agent, cwd string, discover providerDiscoverer) ([]model.AgentSession, error) {
	agentName := string(configured.Type)
	var sessions []model.AgentSession
	for _, root := range configured.SessionDirs {
		if strings.TrimSpace(root) == "" {
			continue
		}
		items, err := discover(agentName, root, cwd)
		if err != nil {
			return nil, fmt.Errorf("session: discover %s at %s: %w", agentName, root, err)
		}
		sessions = append(sessions, items...)
	}
	return sessions, nil
}
~~~

- [ ] Step 4: Run focused, package, and race tests

~~~
gofmt -w svc/session/service.go svc/session/service_test.go
go test ./svc/session -run 'TestListAgents' -count=1
go test ./svc/session -count=1
go test -race ./svc/session -count=1
~~~

Expected: all commands pass with no race reports.

- [ ] Step 5: Commit the implementation

~~~
git add svc/session/service.go svc/session/service_test.go
git commit -m "feat: scan agent sessions concurrently"
~~~

### Task 3: Complete repository verification

Files:

- No additional source changes.

Interfaces:

- Consumes the Task 2 implementation.
- Produces a verified concurrent List implementation with unchanged public behavior.

- [ ] Step 1: Run the full test suite

~~~
go test ./... -count=1
~~~

Expected: all packages pass.

- [ ] Step 2: Run static analysis and build

~~~
go vet ./...
go build -o /tmp/skills-session .
~~~

Expected: both commands exit zero.

- [ ] Step 3: Verify scope and history

~~~
git diff --check
git status --short
git log --oneline -5
~~~

Expected: no whitespace errors; only the pre-existing session TUI spec edit remains uncommitted.
