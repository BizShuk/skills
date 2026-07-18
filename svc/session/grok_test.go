package session

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bizshuk/skills/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverGrokReadsOnlyCurrentProjectSessionDirectories(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	project := filepath.Join(root, url.PathEscape(cwd))
	other := filepath.Join(root, url.PathEscape(filepath.Join(t.TempDir(), "other")))
	sessionPath := filepath.Join(project, "session-a")
	require.NoError(t, os.MkdirAll(filepath.Join(sessionPath, "nested"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(other, "session-other"), 0o755))
	writeJSONL(t, filepath.Join(sessionPath, "summary.json"), `not-json`)
	writeJSONL(t, filepath.Join(project, "prompt_history.jsonl"), `not-json`)

	wantTime := time.Date(2026, 7, 18, 8, 20, 0, 0, time.Local)
	require.NoError(t, os.Chtimes(sessionPath, wantTime, wantTime))

	got, err := discoverGrok(root, cwd)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "session-a", got[0].ID)
	assert.Equal(t, "grok", got[0].Agent)
	assert.Equal(t, sessionPath, got[0].Path)
	assert.Equal(t, wantTime, got[0].StartedAt)
	assert.Equal(t, wantTime, got[0].LastActivity)
}

func TestDiscoverGrokMissingCurrentProjectReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")

	got, err := discoverGrok(root, cwd)

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestLoadGrokDetailFiltersPromptHistory(t *testing.T) {
	project := t.TempDir()
	sessionPath := filepath.Join(project, "session-1")
	require.NoError(t, os.MkdirAll(sessionPath, 0o755))

	writeJSONL(t, filepath.Join(sessionPath, "summary.json"),
		`{"session_summary":"Implement session detail","info":{"cwd":"/workspace/project"}}`,
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
