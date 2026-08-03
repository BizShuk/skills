package stat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestSessionRootComesFromAgentProviders(t *testing.T) {
	home := os.Getenv("HOME")

	assert.Equal(t, filepath.Join(home, ".claude/projects"),
		sessionRoot("claude-code", 0, "sources.claude.projects_dir"))
	assert.Equal(t, filepath.Join(home, ".codex/sessions"),
		sessionRoot("codex", 0, "sources.codex.sessions_dir"))
	assert.Equal(t, filepath.Join(home, ".codex/archived_sessions"),
		sessionRoot("codex", 1, "sources.codex.archived_dir"))
	assert.Equal(t, filepath.Join(home, ".gemini/antigravity-ide/brain"),
		sessionRoot("antigravity", 0, "sources.antigravity.brain_dir"))
	assert.Equal(t, filepath.Join(home, ".gemini/antigravity-cli/brain"),
		sessionRoot("antigravity-cli", 0, "sources.antigravity_cli.brain_dir"))
}

func TestSessionRootHonorsViperOverride(t *testing.T) {
	viper.Set("sources.claude.projects_dir", "/tmp/custom-claude")
	t.Cleanup(func() { viper.Set("sources.claude.projects_dir", "") })

	assert.Equal(t, "/tmp/custom-claude",
		sessionRoot("claude-code", 0, "sources.claude.projects_dir"))
}

func TestSessionRootReturnsEmptyForUnknownAgentOrIndex(t *testing.T) {
	assert.Equal(t, "", sessionRoot("no-such-agent", 0, "sources.none"))
	assert.Equal(t, "", sessionRoot("claude-code", 9, "sources.none"))
}
