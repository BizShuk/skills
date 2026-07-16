package stats

import (
	"os"

	"github.com/bizshuk/skills/svc/stat"
	"github.com/spf13/cobra"
)

// StatsCmd returns the stats command.
func StatsCmd() *cobra.Command {
	var bucketDuration string
	var period string

	stat.InitDefaults()

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show usage statistics for Claude Code, Codex, and Antigravity",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := stat.Run(period, bucketDuration)
			if err != nil {
				return err
			}
			stat.FormatReport(os.Stdout, result)
			return nil
		},
	}

	cmd.Flags().StringVarP(&bucketDuration, "bucket-duration", "b", "1h", "Bucket duration size (e.g. 1h, 24h, 1d)")
	cmd.Flags().StringVarP(&period, "period", "p", "7d", "Calculation period (e.g. 7d, 30d)")

	return cmd
}
