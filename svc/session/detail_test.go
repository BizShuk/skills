package session

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/bizshuk/skills/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDetailDispatchesByAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeJSONL(t, path, `{"type":"user","message":{"role":"user","content":"hello"}}`)

	detail, err := LoadDetail(model.AgentSession{Agent: "claude-code", ID: "s1", Path: path})
	require.NoError(t, err)
	require.Len(t, detail.Events, 1)
	assert.Equal(t, "hello", detail.Events[0].Summary)
}

func TestLoadDetailDispatchesAllSupportedAgents(t *testing.T) {
	for _, agent := range []string{
		"claude-code",
		"codex",
		"grok",
		"antigravity",
		"antigravity-cli",
		"hermes-agent",
		"opencode",
		"pi",
	} {
		t.Run(agent, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missing.jsonl")
			_, err := LoadDetail(model.AgentSession{Agent: agent, ID: "missing", Path: path})
			require.Error(t, err)
			assert.False(t, errors.Is(err, errUnsupportedAgent), "supported agent must not return unsupported-agent")
			assert.True(t, errors.Is(err, fs.ErrNotExist), "source error must be wrapped")
			assert.Contains(t, err.Error(), agent)
			assert.Contains(t, err.Error(), path)
		})
	}
}

func TestLoadDetailRejectsEmptyPath(t *testing.T) {
	_, err := LoadDetail(model.AgentSession{Agent: "claude-code", ID: "s1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty session path")
	assert.Contains(t, err.Error(), "claude-code")
}

func TestLoadDetailRejectsUnsupportedAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")

	_, err := LoadDetail(model.AgentSession{Agent: "unknown-agent", ID: "s1", Path: path})
	require.Error(t, err)
	assert.ErrorIs(t, err, errUnsupportedAgent)
	assert.Contains(t, err.Error(), "unknown-agent")
	assert.Contains(t, err.Error(), path)
}

func TestLoadDetailReturnsWrappedMissingPathError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.jsonl")

	_, err := LoadDetail(model.AgentSession{Agent: "codex", ID: "missing", Path: path})
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
	assert.Contains(t, err.Error(), "codex")
	assert.Contains(t, err.Error(), path)
}

func TestScanDetailFileSkipsMalformedLinesAndKeepsRawJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeJSONL(t, path,
		`{"type":"message","role":"user","content":"hello"}`,
		"not-json",
		`{"type":"future_event","payload":{"value":1}}`,
	)

	var records []string
	err := scanDetailFile(path, func(record map[string]any, raw string) error {
		records = append(records, raw)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Contains(t, records[0], `"content":"hello"`)
	assert.Contains(t, records[1], `"future_event"`)
}

func TestNormalizeGenericRecordExtractsMessageAndRawFallback(t *testing.T) {
	message, ok := normalizeGenericRecord(map[string]any{
		"timestamp": "2026-07-18T08:00:00Z",
		"type":      "message",
		"role":      "user",
		"content":   "hello",
	}, `{"type":"message"}`)
	require.True(t, ok)
	assert.Equal(t, "user", message.Role)
	assert.Equal(t, "message", message.Kind)
	assert.Equal(t, "hello", message.Summary)

	raw, ok := normalizeGenericRecord(map[string]any{
		"type":    "future_event",
		"payload": map[string]any{"value": float64(1)},
	}, `{"type":"future_event","payload":{"value":1}}`)
	require.True(t, ok)
	assert.Equal(t, "raw", raw.Kind)
	assert.Equal(t, `{"type":"future_event","payload":{"value":1}}`, raw.Raw)
}
