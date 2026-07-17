package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bizshuk/skills/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadGrokDetailFiltersPromptHistory(t *testing.T) {
	project := t.TempDir()
	sessionPath := filepath.Join(project, "session-1")
	require.NoError(t, os.MkdirAll(sessionPath, 0o755))

	writeJSONL(t, filepath.Join(sessionPath, "summary.json"),
		`{"session_summary":"Implement session detail","created_at":"2026-07-18T07:59:00Z","info":{"cwd":"/workspace/project"}}`,
	)
	writeJSONL(t, filepath.Join(project, "prompt_history.jsonl"),
		`{"session_id":"session-1","timestamp":"2026-07-18T08:00:00Z","prompt":"keep this prompt"}`,
		`{"session_id":"other-session","timestamp":"2026-07-18T08:01:00Z","prompt":"do not include this prompt"}`,
		"not-json",
		`{"session_id":"session-1","timestamp":"2026-07-18T08:02:00Z","prompt":"keep this one too"}`,
	)

	item := model.AgentSession{
		Agent: "grok",
		ID:    "session-1",
		Path:  sessionPath,
	}
	detail, err := loadGrokDetail(item)

	require.NoError(t, err)
	assert.Equal(t, item, detail.Session)
	assert.Equal(t, "Implement session detail", detail.Title)
	assert.Equal(t, "/workspace/project", detail.CWD)
	require.Len(t, detail.Events, 3)
	assert.Equal(t, "Implement session detail", detail.Events[0].Summary)
	assert.Equal(t, "system", detail.Events[0].Role)
	assert.Equal(t, "event", detail.Events[0].Kind)
	assert.Equal(t, []string{"keep this prompt", "keep this one too"}, []string{
		detail.Events[1].Summary,
		detail.Events[2].Summary,
	})
	for _, event := range detail.Events[1:] {
		assert.Equal(t, "user", event.Role)
		assert.Equal(t, "message", event.Kind)
	}
}
