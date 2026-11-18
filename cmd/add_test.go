package cmd

import (
	"path/filepath"
	"testing"

	"github.com/bizshuk/skills/svc/agent"
	"github.com/bizshuk/skills/svc/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddCmdFlags(t *testing.T) {
	cmd := addCmd()

	globalFlag := cmd.Flags().Lookup("global")
	require.NotNil(t, globalFlag)
	assert.Equal(t, "true", globalFlag.DefValue)

	projectFlag := cmd.Flags().Lookup("project")
	require.NotNil(t, projectFlag)
	assert.Equal(t, "false", projectFlag.DefValue)

	// Verify mutually exclusive flags error when both provided.
	cmd.SetArgs([]string{"some-path", "--global", "--project"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRecordInstall_NormalizesLocalRelativeSource(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	recordInstall(".", agent.Selection{
		SkillPaths: []string{"/tmp/skills/inf-spec"},
		AgentTypes: []agent.AgentType{"claude-code"},
		Global:     true,
	}, 3)

	f, err := update.Load()
	require.NoError(t, err)
	require.Len(t, f.Entries, 1)
	assert.True(t, filepath.IsAbs(f.Entries[0].Source), "local relative source should be stored as absolute path, got %q", f.Entries[0].Source)
	assert.NotEqual(t, ".", f.Entries[0].Source)
}

func TestRecordInstall_KeepsRemoteSourceVerbatim(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	recordInstall("bizshuk/inf", agent.Selection{
		SkillPaths: []string{"/tmp/skills/inf-spec"},
		AgentTypes: []agent.AgentType{"claude-code"},
		Global:     true,
	}, 3)

	f, err := update.Load()
	require.NoError(t, err)
	require.Len(t, f.Entries, 1)
	assert.Equal(t, "bizshuk/inf", f.Entries[0].Source)
}
