package cmd

import (
	"github.com/bizshuk/skills/svc/update"
	"github.com/spf13/cobra"
)

func updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Re-install tracked skills from their original sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			return update.Run(args)
		},
	}
}
