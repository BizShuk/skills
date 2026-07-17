package session

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/bizshuk/skills/model"
)

const sessionTimeLayout = "2006-01-02 15:04:05"

// Format writes a human-readable session table or an empty-result message.
func Format(w io.Writer, cwd string, sessions []model.AgentSession) error {
	if len(sessions) == 0 {
		_, err := fmt.Fprintf(w, "no agent sessions found for %s\n", cwd)
		return err
	}

	table := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "AGENT\tSESSION ID\tSTARTED\tLAST ACTIVITY\tPATH"); err != nil {
		return err
	}
	for _, session := range sessions {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n",
			session.Agent,
			session.ID,
			formatSessionTime(session.StartedAt),
			formatSessionTime(session.LastActivity),
			session.Path,
		); err != nil {
			return err
		}
	}
	return table.Flush()
}

func formatSessionTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format(sessionTimeLayout)
}
