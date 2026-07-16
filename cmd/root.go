// Package cmd assembles the skills CLI's cobra command tree. The package
// exposes Execute() which returns an error rather than calling os.Exit
// directly so callers (e.g. the root-level main.go in this repo) decide
// how to surface failures. This package also accepts being embedded into
// a larger tool that wants the "skills" subcommand set as part of its
// own root.
package cmd

import (
	"github.com/bizshuk/skills/cmd/stats"
	"github.com/spf13/cobra"
)

// Execute builds the cobra command tree and runs it with os.Args.
// Errors from cobra or from any RunE are returned to the caller; the
// root-level main in this repo prints them and exits non-zero.
func Execute() error {
	root := &cobra.Command{Use: "skills", SilenceUsage: true}

	root.AddCommand(addCmd())
	root.AddCommand(updateCmd())
	root.AddCommand(removeCmd())
	root.AddCommand(stats.StatsCmd())

	return root.Execute()
}
