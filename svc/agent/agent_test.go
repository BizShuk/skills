package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAllReturnsEightProviders(t *testing.T) {
	all := LoadAll()
	assert.Len(t, all, 8)
	want := []string{
		"claude-code", "antigravity", "antigravity-cli",
		"codex", "opencode", "hermes-agent", "grok", "pi",
	}
	got := make([]string, 0, len(all))
	for _, p := range all {
		got = append(got, string(p.Type))
	}
	assert.ElementsMatch(t, want, got)
}

func TestProviderFieldsRoundTripViaJSON(t *testing.T) {
	all := LoadAll()
	for _, p := range all {
		t.Run(string(p.Type), func(t *testing.T) {
			// each provider must have non-empty project + user dirs
			assert.NotEmpty(t, p.Type)
			assert.NotEmpty(t, p.ProjectSkillsDir, "ProjectSkillsDir empty for %s", p.Type)
			assert.NotEmpty(t, p.UserSkillsDir, "UserSkillsDir empty for %s", p.Type)
			assert.NotEmpty(t, p.ProjectAgentsDir, "ProjectAgentsDir empty for %s", p.Type)
			assert.NotEmpty(t, p.UserAgentsDir, "UserAgentsDir empty for %s", p.Type)
			assert.NotEmpty(t, p.DetectDir, "DetectDir empty for %s", p.Type)
			// user-side paths must start with "~/" (the agent package expands at use time)
			assert.True(t, strings.HasPrefix(p.UserSkillsDir, "~/"), "UserSkillsDir not ~/: %s", p.UserSkillsDir)
			assert.True(t, strings.HasPrefix(p.UserAgentsDir, "~/"), "UserAgentsDir not ~/: %s", p.UserAgentsDir)
			assert.True(t, strings.HasPrefix(p.DetectDir, "~/"), "DetectDir not ~/: %s", p.DetectDir)
		})
	}
}

func TestParseSingleProvider(t *testing.T) {
	raw := `{
      "type": "claude-code",
      "displayName": "Claude Code",
      "projectSkillsDir": ".claude/skills",
      "userSkillsDir": "~/.claude/skills",
      "projectAgentsDir": ".claude/agents",
      "userAgentsDir": "~/.claude/agents",
      "detectDir": "~/.claude"
    }`
	p, err := parse(strings.NewReader(raw))
	require.NoError(t, err)
	assert.Equal(t, "claude-code", string(p.Type))
	assert.Equal(t, "Claude Code", p.DisplayName)
}

func TestParseRejectsInvalidJSON(t *testing.T) {
	_, err := parse(strings.NewReader(`{"type":`))
	assert.Error(t, err)
}

func TestParseRejectsMissingType(t *testing.T) {
	_, err := parse(strings.NewReader(`{"displayName":"x"}`))
	assert.Error(t, err)
}

// TestProviderJSONFilesAreValid ensures every file in providers/ parses cleanly.
// New providers added to the embed set will fail-fast here.
func TestProviderJSONFilesAreValid(t *testing.T) {
	files, err := providers.ReadDir("providers")
	require.NoError(t, err)
	require.NotEmpty(t, files, "no providers embedded")

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		t.Run(f.Name(), func(t *testing.T) {
			data, err := providers.ReadFile("providers/" + f.Name())
			require.NoError(t, err)
			var raw map[string]any
			require.NoError(t, json.Unmarshal(data, &raw))
			assert.Contains(t, raw, "type")
			assert.Contains(t, raw, "projectSkillsDir")
			assert.Contains(t, raw, "userSkillsDir")
			assert.Contains(t, raw, "projectAgentsDir")
			assert.Contains(t, raw, "userAgentsDir")
			assert.Contains(t, raw, "detectDir")
		})
	}
	_ = os.Getenv // keep os import available for future tests
}