package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverCodexScansArchivedRootAndUsesSessionMeta(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested"), 0o755))

	writeJSONL(t, filepath.Join(root, "nested", "rollout.jsonl"),
		`{"type":"session_meta","payload":{"id":"active","cwd":"`+cwd+`"}}`,
		`{"type":"event_msg","timestamp":"2026-07-18T08:20:00Z"}`,
	)
	writeJSONL(t, filepath.Join(root, "archived.jsonl"),
		`{"type":"session_meta","payload":{"id":"archived","cwd":"`+cwd+`"}}`,
		`{"type":"event_msg","timestamp":"2026-07-18T07:20:00Z"}`,
	)

	got, err := discoverCodex(root, cwd)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.ElementsMatch(t, []string{"active", "archived"}, []string{got[0].ID, got[1].ID})
	for _, item := range got {
		assert.Equal(t, "codex", item.Agent)
	}
}
