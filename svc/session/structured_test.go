package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverStructuredUsesExplicitCwdKeysOnly(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "workspace")
	brain := filepath.Join(root, "brain-1", ".system_generated", "logs")
	require.NoError(t, os.MkdirAll(brain, 0o755))
	writeJSONL(t, filepath.Join(brain, "transcript.jsonl"),
		`{"created_at":"2026-07-18T08:00:00Z","tool_calls":[{"args":{"Cwd":"`+cwd+`"}}]}`,
		`{"created_at":"2026-07-18T08:05:00Z","content":"`+cwd+` was mentioned"}`,
	)

	got, err := discoverStructured(root, cwd, "antigravity")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "brain-1", got[0].ID)
}
