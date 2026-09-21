package cmd

import (
	"testing"

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
