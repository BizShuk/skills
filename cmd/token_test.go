package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTokenCommandRegistered verifies that newRootCmd() exposes the
// token subcommand so users can discover it via `skills --help`.
func TestTokenCommandRegistered(t *testing.T) {
	root := newRootCmd()
	var found *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "token" {
			found = c
			break
		}
	}
	require.NotNil(t, found, "token subcommand must be registered in newRootCmd()")
	assert.Equal(t, "token [prompt]", found.Use)
}

func TestResolvePromptFromArg(t *testing.T) {
	cmd := &cobra.Command{Use: "token"}
	got, err := resolvePrompt(cmd, []string{"hello"})
	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

func TestResolvePromptFromStdin(t *testing.T) {
	cmd := &cobra.Command{Use: "token"}
	cmd.SetIn(strings.NewReader("piped content"))
	got, err := resolvePrompt(cmd, nil)
	require.NoError(t, err)
	assert.Equal(t, "piped content", got)
}

func TestResolvePromptStripsTrailingNewline(t *testing.T) {
	cmd := &cobra.Command{Use: "token"}
	cmd.SetIn(strings.NewReader("hi\n"))
	got, err := resolvePrompt(cmd, nil)
	require.NoError(t, err)
	assert.Equal(t, "hi", got)
}

func TestResolvePromptRejectsEmpty(t *testing.T) {
	cmd := &cobra.Command{Use: "token"}
	cmd.SetIn(strings.NewReader(""))
	_, err := resolvePrompt(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is empty")
}

func TestResolvePromptRejectsMultipleArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "token"}
	_, err := resolvePrompt(cmd, []string{"a", "b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at most one")
}
