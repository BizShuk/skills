package session

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/bizshuk/skills/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatRendersTable(t *testing.T) {
	started := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	lastActivity := time.Date(2026, 7, 18, 8, 5, 0, 0, time.UTC)
	sessions := []model.AgentSession{{
		Agent:        "claude-code",
		ID:           "session-1",
		StartedAt:    started,
		LastActivity: lastActivity,
		Path:         "/tmp/session-1.jsonl",
	}}

	var out bytes.Buffer
	require.NoError(t, Format(&out, "/workspace/project", sessions))
	assert.Contains(t, out.String(), "AGENT")
	assert.Contains(t, out.String(), "SESSION ID")
	assert.Contains(t, out.String(), "STARTED")
	assert.Contains(t, out.String(), "LAST ACTIVITY")
	assert.Contains(t, out.String(), "PATH")
	assert.Contains(t, out.String(), "claude-code")
	assert.Contains(t, out.String(), "session-1")
	assert.Contains(t, out.String(), started.Local().Format("2006-01-02 15:04:05"))
	assert.Contains(t, out.String(), lastActivity.Local().Format("2006-01-02 15:04:05"))
	assert.Contains(t, out.String(), "/tmp/session-1.jsonl")
	assert.NotContains(t, out.String(), "0001-01-01")
}

func TestFormatEmptyResult(t *testing.T) {
	var out bytes.Buffer
	err := Format(&out, "/workspace/project", nil)
	require.NoError(t, err)
	assert.Equal(t, "no agent sessions found for /workspace/project\n", out.String())
	assert.True(t, strings.HasSuffix(out.String(), "\n"))
}
