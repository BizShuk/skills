package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bizshuk/skills/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadStructuredDetailPreservesRawFallback(t *testing.T) {
	root := t.TempDir()
	transcriptDir := filepath.Join(root, "session-1", "nested")
	require.NoError(t, os.MkdirAll(transcriptDir, 0o755))
	writeJSONL(t, filepath.Join(transcriptDir, "transcript.jsonl"),
		`{"timestamp":"2026-07-18T08:00:00Z","type":"message","message":{"role":"user","content":"hello"},"metadata":{"Cwd":"/workspace/project"}}`,
		`{"timestamp":"2026-07-18T08:01:00Z","type":"note","prompt":"mentions /workspace/project but is not cwd metadata"}`,
		`{"timestamp":"2026-07-18T08:02:00Z","type":"future_event","payload":{"value":1}}`,
	)
	require.NoError(t, os.WriteFile(filepath.Join(transcriptDir, "ignored.txt"), []byte(`{"type":"message","content":"ignore me"}`), 0o644))

	item := model.AgentSession{
		Agent: "antigravity",
		ID:    "session-1",
		Path:  filepath.Join(root, "session-1"),
	}
	detail, err := loadStructuredDetail(item)

	require.NoError(t, err)
	assert.Equal(t, item, detail.Session)
	assert.Equal(t, "/workspace/project", detail.CWD)
	require.Len(t, detail.Events, 3)
	assert.Equal(t, "user", detail.Events[0].Role)
	assert.Equal(t, "message", detail.Events[0].Kind)
	assert.Equal(t, "hello", detail.Events[0].Summary)
	assert.Equal(t, "mentions /workspace/project but is not cwd metadata", detail.Events[1].Summary)
	assert.Equal(t, "message", detail.Events[1].Kind)
	assert.Equal(t, "raw", detail.Events[2].Kind)
	assert.JSONEq(t, `{"type":"future_event","payload":{"value":1},"timestamp":"2026-07-18T08:02:00Z"}`, detail.Events[2].Raw)
}
