package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/skills/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverClaudeReadsOnlyCurrentProjectSessionEntries(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	projectKey := strings.ReplaceAll(filepath.ToSlash(cwd), "/", "-")
	project := filepath.Join(root, projectKey)
	otherProject := filepath.Join(root, "-other-project")
	require.NoError(t, os.MkdirAll(filepath.Join(project, "session-a", "subagents"), 0o755))
	require.NoError(t, os.MkdirAll(otherProject, 0o755))

	sessionPath := filepath.Join(project, "session-a.jsonl")
	writeJSONL(t, sessionPath, `not-session-json`)
	writeJSONL(t, filepath.Join(project, "session-a", "subagents", "agent-child.jsonl"),
		`{"sessionId":"child","cwd":"`+cwd+`"}`,
	)
	writeJSONL(t, filepath.Join(otherProject, "other.jsonl"),
		`{"sessionId":"other","cwd":"`+cwd+`"}`,
	)

	wantTime := time.Date(2026, 7, 18, 8, 5, 0, 0, time.Local)
	require.NoError(t, os.Chtimes(sessionPath, wantTime, wantTime))

	got, err := discoverClaude(root, cwd)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "session-a", got[0].ID)
	assert.Equal(t, "claude-code", got[0].Agent)
	assert.Equal(t, sessionPath, got[0].Path)
	assert.Equal(t, wantTime, got[0].StartedAt)
	assert.Equal(t, wantTime, got[0].LastActivity)
}

func TestLoadClaudeDetailNormalizesTimeline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeJSONL(t, path,
		`{"type":"user","sessionId":"s1","cwd":"/workspace/project","timestamp":"2026-07-18T08:00:00Z","message":{"role":"user","content":[{"type":"text","text":"Inspect the parser"}]}}`,
		`{"type":"assistant","sessionId":"s1","timestamp":"2026-07-18T08:01:00Z","message":{"role":"assistant","content":[{"type":"text","text":"I will inspect it"},{"type":"tool_use","name":"rg","input":{"pattern":"session"}}]}}`,
		"not-json",
		`{"type":"user","sessionId":"s1","timestamp":"2026-07-18T08:01:30Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":[{"type":"text","text":"found session"}]}]}}`,
		`{"type":"system","sessionId":"s1","timestamp":"2026-07-18T08:02:00Z","subtype":"info","summary":"Finished"}`,
		`{"type":"system","sessionId":"s1","timestamp":"2026-07-18T08:03:00Z","subtype":"progress"}`,
		`{"type":"future_event","payload":{"value":1}}`,
	)

	started := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	item := model.AgentSession{
		Agent:        "claude-code",
		ID:           "s1",
		StartedAt:    started,
		LastActivity: started.Add(3 * time.Minute),
		Path:         path,
	}

	detail, err := loadClaudeDetail(item)
	require.NoError(t, err)
	assert.Equal(t, item, detail.Session)
	assert.Equal(t, "/workspace/project", detail.CWD)
	assert.Equal(t, "Inspect the parser", detail.Title)

	require.Len(t, detail.Events, 7)
	assert.Equal(t, model.SessionEvent{
		Timestamp: started,
		Role:      "user",
		Kind:      "message",
		Summary:   "Inspect the parser",
	}, detail.Events[0])
	assert.Equal(t, "assistant", detail.Events[1].Role)
	assert.Equal(t, "message", detail.Events[1].Kind)
	assert.Equal(t, "I will inspect it", detail.Events[1].Summary)
	assert.Equal(t, "tool", detail.Events[2].Role)
	assert.Equal(t, "tool_call", detail.Events[2].Kind)
	assert.Equal(t, "rg", detail.Events[2].Summary)
	assert.Equal(t, "tool", detail.Events[3].Role)
	assert.Equal(t, "tool_call", detail.Events[3].Kind)
	assert.Equal(t, "found session", detail.Events[3].Summary)
	assert.Equal(t, "system", detail.Events[4].Role)
	assert.Equal(t, "event", detail.Events[4].Kind)
	assert.Equal(t, "Finished", detail.Events[4].Summary)
	assert.Equal(t, "system", detail.Events[5].Role)
	assert.Equal(t, "event", detail.Events[5].Kind)
	assert.Equal(t, "progress", detail.Events[5].Summary)
	assert.Equal(t, "raw", detail.Events[6].Kind)
	assert.JSONEq(t, `{"type":"future_event","payload":{"value":1}}`, detail.Events[6].Raw)
}
