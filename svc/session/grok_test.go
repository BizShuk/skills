package session

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverGrokFiltersEscapedProjectRoot(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "workspace")
	project := filepath.Join(root, url.PathEscape(cwd))
	other := filepath.Join(root, url.PathEscape(filepath.Join(t.TempDir(), "other")))
	require.NoError(t, os.MkdirAll(filepath.Join(project, "session-a"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(project, "session-b"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(other, "session-other"), 0o755))

	writeJSONL(t, filepath.Join(project, "session-a", "summary.json"),
		`{"created_at":"2026-07-18T08:00:00Z","updated_at":"2026-07-18T08:10:00Z"}`,
	)
	writeJSONL(t, filepath.Join(project, "session-b", "summary.json"),
		`{"created_at":"2026-07-18T08:05:00Z","updated_at":"2026-07-18T08:20:00Z"}`,
	)
	writeJSONL(t, filepath.Join(project, "session-b", "prompt_context.json"),
		`{"working_directory":"`+cwd+`"}`,
	)
	writeJSONL(t, filepath.Join(other, "session-other", "summary.json"),
		`{"created_at":"2026-07-18T09:00:00Z","updated_at":"2026-07-18T09:30:00Z"}`,
	)

	got, err := discoverGrok(root, cwd)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.ElementsMatch(t, []string{"session-a", "session-b"}, []string{got[0].ID, got[1].ID})
	for _, item := range got {
		assert.Equal(t, "grok", item.Agent)
	}
}
