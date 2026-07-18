package session

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mitchellh/go-homedir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bizshuk/skills/model"
	"github.com/bizshuk/skills/svc/agent"
)

func waitForSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}

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

func TestListSortsByLastActivityThenAgentAndID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	homedir.DisableCache = true
	cwd := filepath.Join(t.TempDir(), "workspace")
	claudeProject := filepath.Join(home, ".claude", "projects", claudeProjectKey(cwd))
	require.NoError(t, os.MkdirAll(claudeProject, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex", "sessions"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex", "archived_sessions"), 0o755))

	claudeA := filepath.Join(claudeProject, "claude-a.jsonl")
	claudeB := filepath.Join(claudeProject, "claude-b.jsonl")
	writeJSONL(t, claudeA,
		`{"sessionId":"claude-a","cwd":"`+cwd+`","timestamp":"2026-07-18T08:00:00Z"}`,
	)
	writeJSONL(t, claudeB,
		`{"sessionId":"claude-b","cwd":"`+cwd+`","timestamp":"2026-07-18T08:00:00Z"}`,
	)
	wantClaudeTime := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(claudeA, wantClaudeTime, wantClaudeTime))
	require.NoError(t, os.Chtimes(claudeB, wantClaudeTime, wantClaudeTime))
	writeJSONL(t, filepath.Join(home, ".codex", "sessions", "codex-new.jsonl"),
		`{"type":"session_meta","payload":{"id":"codex-new","cwd":"`+cwd+`"}}`,
		`{"timestamp":"2026-07-18T09:00:00Z"}`,
	)
	writeJSONL(t, filepath.Join(home, ".codex", "archived_sessions", "codex-old.jsonl"),
		`{"type":"session_meta","payload":{"id":"codex-old","cwd":"`+cwd+`"}}`,
		`{"timestamp":"2026-07-18T07:00:00Z"}`,
	)

	got, err := List(cwd)
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, []string{"codex-new", "claude-a", "claude-b", "codex-old"}, []string{
		got[0].ID, got[1].ID, got[2].ID, got[3].ID,
	})
}

func TestListMissingRootsReturnEmptyResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	homedir.DisableCache = true

	got, err := List(filepath.Join(t.TempDir(), "workspace"))
	require.NoError(t, err)
	assert.Empty(t, got)
}
