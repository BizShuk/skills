package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bizshuk/skills/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverClaudeFiltersByCWDAndIncludesSubagents(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "project", "subagents"), 0o755))

	writeJSONL(t, filepath.Join(root, "project", "parent.jsonl"),
		`{"sessionId":"parent","cwd":"`+cwd+`","timestamp":"2026-07-18T08:00:00Z"}`,
		`not-json`,
		`{"sessionId":"parent","cwd":"`+cwd+`","timestamp":"2026-07-18T08:05:00Z"}`,
	)
	writeJSONL(t, filepath.Join(root, "project", "other.jsonl"),
		`{"sessionId":"other","cwd":"/other","timestamp":"2026-07-18T08:10:00Z"}`,
	)
	writeJSONL(t, filepath.Join(root, "project", "subagents", "child.jsonl"),
		`{"sessionId":"child","cwd":"`+cwd+`","timestamp":"2026-07-18T08:03:00Z"}`,
	)

	got, err := discoverClaude(root, cwd)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.ElementsMatch(t, []string{"child", "parent"}, []string{got[0].ID, got[1].ID})
	startedByID := map[string]time.Time{}
	lastByID := map[string]time.Time{}
	for _, item := range got {
		startedByID[item.ID] = item.StartedAt
		lastByID[item.ID] = item.LastActivity
	}
	assert.Equal(t, time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC), startedByID["parent"])
	assert.Equal(t, time.Date(2026, 7, 18, 8, 5, 0, 0, time.UTC), lastByID["parent"])
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
