package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootRegistersSessionCommand(t *testing.T) {
	root := newRootCmd()
	command, _, err := root.Find([]string{"session"})
	require.NoError(t, err)
	require.NotNil(t, command)
	require.Equal(t, "session", command.Name())
	require.NoError(t, command.Args(command, nil))
	require.Error(t, command.Args(command, []string{"unexpected"}))
}
