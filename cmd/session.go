package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/bizshuk/skills/model"
	"github.com/bizshuk/skills/svc/session"
	"github.com/bizshuk/skills/svc/tui"
	"github.com/spf13/cobra"
)

func runSessionCommand(
	out io.Writer,
	cwd string,
	list func(string) ([]model.AgentSession, error),
	runTUI func([]model.AgentSession) error,
) error {
	items, err := list(cwd)
	if err != nil {
		return fmt.Errorf("session: list: %w", err)
	}
	if len(items) == 0 {
		return session.Format(out, cwd, items)
	}
	if err := runTUI(items); err != nil {
		return fmt.Errorf("session: tui: %w", err)
	}
	return nil
}

func sessionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session",
		Short: "List agent sessions for the current directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("session: resolve cwd: %w", err)
			}
			return runSessionCommand(cmd.OutOrStdout(), cwd, session.List, tui.RunSession)
		},
	}
}
