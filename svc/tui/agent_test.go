package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bizshuk/skills/svc/agent"
)

func TestAgentSelectionGroupsByGlobalRulePath(t *testing.T) {
	sharedPath := filepath.Join(t.TempDir(), "GEMINI.md")
	m := newAgentSelectionModel([]agent.Agent{
		{Type: "antigravity", DisplayName: "Antigravity", GlobalRulePath: sharedPath},
		{Type: "antigravity-cli", DisplayName: "Antigravity CLI", GlobalRulePath: sharedPath},
		{Type: "codex", DisplayName: "Codex", GlobalRulePath: filepath.Join(t.TempDir(), "AGENTS.md")},
	})

	require.Len(t, m.rows, 2)
	require.Len(t, m.rows[0].agents, 2)
	assert.Equal(t, agent.AgentType("antigravity"), m.rows[0].agents[0].Type)
	assert.Equal(t, agent.AgentType("antigravity-cli"), m.rows[0].agents[1].Type)
	assert.Equal(t, agent.AgentType("codex"), m.rows[1].agents[0].Type)
}

func TestAgentSelectionPrechecksDetectedAgents(t *testing.T) {
	root := t.TempDir()
	detectedDir := filepath.Join(root, "codex")
	require.NoError(t, os.MkdirAll(detectedDir, 0o755))

	m := newAgentSelectionModel([]agent.Agent{
		{
			Type:           "codex",
			DisplayName:    "Codex",
			GlobalRulePath: filepath.Join(root, "AGENTS.md"),
			DetectDir:      detectedDir,
		},
		{
			Type:           "pi",
			DisplayName:    "Pi",
			GlobalRulePath: filepath.Join(root, "pi", "AGENTS.md"),
			DetectDir:      filepath.Join(root, "missing"),
		},
	})

	require.Len(t, m.rows, 2)
	assert.True(t, m.rows[0].detected)
	assert.True(t, m.rows[0].checked)
	assert.False(t, m.rows[1].detected)
	assert.False(t, m.rows[1].checked)
	assert.Equal(t, []agent.AgentType{"codex"}, m.Selection())
}

func TestAgentSelectionDefaultsToOnlyDetectedMembersOfSharedPathGroup(t *testing.T) {
	root := t.TempDir()
	detectedDir := filepath.Join(root, "antigravity")
	require.NoError(t, os.MkdirAll(detectedDir, 0o755))
	sharedPath := filepath.Join(root, "GEMINI.md")

	m := newAgentSelectionModel([]agent.Agent{
		{
			Type:           "antigravity",
			DisplayName:    "Antigravity",
			GlobalRulePath: sharedPath,
			DetectDir:      detectedDir,
		},
		{
			Type:           "antigravity-cli",
			DisplayName:    "Antigravity CLI",
			GlobalRulePath: sharedPath,
			DetectDir:      filepath.Join(root, "missing"),
		},
	})

	require.Len(t, m.rows, 2)
	assert.True(t, m.rows[0].checked)
	assert.False(t, m.rows[1].checked)
	assert.Equal(t, []agent.AgentType{"antigravity"}, m.Selection())
}

func TestAgentSelectionSpaceTogglesWholeSharedPathGroup(t *testing.T) {
	sharedPath := filepath.Join(t.TempDir(), "GEMINI.md")
	m := newAgentSelectionModel([]agent.Agent{
		{Type: "antigravity", DisplayName: "Antigravity", GlobalRulePath: sharedPath},
		{Type: "antigravity-cli", DisplayName: "Antigravity CLI", GlobalRulePath: sharedPath},
	})
	require.False(t, m.rows[0].checked)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mustAgentSelectionModel(t, updated)
	assert.ElementsMatch(t,
		[]agent.AgentType{"antigravity", "antigravity-cli"},
		m.Selection(),
	)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mustAgentSelectionModel(t, updated)
	assert.True(t, m.done)
}

func TestAgentSelectionEscCancels(t *testing.T) {
	m := newAgentSelectionModel([]agent.Agent{{
		Type:           "codex",
		DisplayName:    "Codex",
		GlobalRulePath: filepath.Join(t.TempDir(), "AGENTS.md"),
	}})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mustAgentSelectionModel(t, updated)
	assert.True(t, m.cancel)
	assert.Empty(t, m.Selection())
}

func TestAgentSelectionRunWithNoAgentsReturnsEmpty(t *testing.T) {
	selected, err := RunAgentSelection(nil)
	require.NoError(t, err)
	assert.Empty(t, selected)
}

func mustAgentSelectionModel(t *testing.T, model tea.Model) agentSelectionModel {
	t.Helper()
	got, ok := model.(agentSelectionModel)
	require.True(t, ok, "unexpected model type %T", model)
	return got
}
