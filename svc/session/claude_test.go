package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
