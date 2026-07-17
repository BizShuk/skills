package session

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
