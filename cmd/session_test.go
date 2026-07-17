package cmd

import (
	"bytes"
	"testing"

	"github.com/bizshuk/skills/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionCommandKeepsPlainOutputWhenListIsEmpty(t *testing.T) {
	var output bytes.Buffer
	err := runSessionCommand(
		&output,
		"/workspace/project",
		func(string) ([]model.AgentSession, error) {
			return nil, nil
		},
		func([]model.AgentSession) error {
			t.Fatal("empty session list must not launch TUI")
			return nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t, "no agent sessions found for /workspace/project\n", output.String())
}

func TestSessionCommandRunsTUIForNonEmptyList(t *testing.T) {
	items := []model.AgentSession{{Agent: "codex", ID: "session-1"}}
	var received []model.AgentSession

	err := runSessionCommand(
		&bytes.Buffer{},
		"/workspace/project",
		func(string) ([]model.AgentSession, error) {
			return items, nil
		},
		func(got []model.AgentSession) error {
			received = got
			return nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t, items, received)
}
