package model

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAgentSessionStoresListingMetadata(t *testing.T) {
	started := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	last := started.Add(15 * time.Minute)
	s := AgentSession{
		Agent:        "codex",
		ID:           "session-1",
		StartedAt:    started,
		LastActivity: last,
		Path:         filepath.Join("/tmp", "session.jsonl"),
	}

	assert.Equal(t, "codex", s.Agent)
	assert.Equal(t, "session-1", s.ID)
	assert.Equal(t, started, s.StartedAt)
	assert.Equal(t, last, s.LastActivity)
	assert.Equal(t, filepath.Join("/tmp", "session.jsonl"), s.Path)
}

func TestAgentSessionDetailStoresNormalizedEvents(t *testing.T) {
	timestamp := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	detail := AgentSessionDetail{
		Session: AgentSession{Agent: "codex", ID: "session-1"},
		Title:   "Implement session TUI",
		CWD:     "/workspace/project",
		Events: []SessionEvent{{
			Timestamp: timestamp,
			Role:      "assistant",
			Kind:      "message",
			Summary:   "Implemented the list view",
			Raw:       "",
		}},
	}

	assert.Equal(t, "session-1", detail.Session.ID)
	assert.Equal(t, timestamp, detail.Events[0].Timestamp)
	assert.Equal(t, "assistant", detail.Events[0].Role)
	assert.Equal(t, "message", detail.Events[0].Kind)
	assert.Equal(t, "Implemented the list view", detail.Events[0].Summary)
}
