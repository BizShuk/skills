package cmd

import (
	"fmt"
	"os"

	"github.com/bizshuk/skills/svc/session"
	"github.com/spf13/cobra"
)

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
			items, err := session.List(cwd)
			if err != nil {
				return fmt.Errorf("session: list: %w", err)
			}
			return session.Format(cmd.OutOrStdout(), cwd, items)
		},
	}
}
