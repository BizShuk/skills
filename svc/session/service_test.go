package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mitchellh/go-homedir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListSortsByLastActivityThenAgentAndID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	homedir.DisableCache = true
	cwd := filepath.Join(t.TempDir(), "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex", "sessions"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex", "archived_sessions"), 0o755))

	writeJSONL(t, filepath.Join(home, ".claude", "projects", "claude-a.jsonl"),
		`{"sessionId":"claude-a","cwd":"`+cwd+`","timestamp":"2026-07-18T08:00:00Z"}`,
	)
	writeJSONL(t, filepath.Join(home, ".claude", "projects", "claude-b.jsonl"),
		`{"sessionId":"claude-b","cwd":"`+cwd+`","timestamp":"2026-07-18T08:00:00Z"}`,
	)
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
