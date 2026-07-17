package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bizshuk/skills/model"
)

// loadGrokDetail reads a Grok session directory and its parent prompt history.
func loadGrokDetail(item model.AgentSession) (model.AgentSessionDetail, error) {
	info, err := os.Stat(item.Path)
	if err != nil {
		return model.AgentSessionDetail{}, fmt.Errorf("stat grok session %s: %w", item.Path, err)
	}
	if !info.IsDir() {
		return model.AgentSessionDetail{}, fmt.Errorf("grok session path is not a directory: %s", item.Path)
	}

	detail := model.AgentSessionDetail{
		Session: item,
		Events:  make([]model.SessionEvent, 0),
	}
	var summaryTimestamp time.Time
	metadataFiles := []string{
		filepath.Join(item.Path, "session.json"),
		filepath.Join(item.Path, "summary.json"),
	}
	for _, path := range metadataFiles {
		metadata, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return model.AgentSessionDetail{}, fmt.Errorf("stat grok metadata %s: %w", path, err)
		}
		if metadata.IsDir() || !metadata.Mode().IsRegular() {
			continue
		}

		err = scanDetailFile(path, func(record map[string]any, _ string) error {
			if detail.CWD == "" {
				detail.CWD = firstDetailWorkingDirectory(record)
			}
			if title := strings.TrimSpace(stringValue(record["session_summary"])); title != "" {
				detail.Title = title
				if timestamp := eventTimestamp(record); !timestamp.IsZero() {
					summaryTimestamp = timestamp
				}
			}
			return nil
		})
		if err != nil {
			return model.AgentSessionDetail{}, fmt.Errorf("read grok metadata %s: %w", path, err)
		}
	}

	if detail.Title != "" {
		detail.Events = append(detail.Events, model.SessionEvent{
			Timestamp: summaryTimestamp,
			Role:      "system",
			Kind:      "event",
			Summary:   detail.Title,
		})
	}

	promptHistory := filepath.Join(filepath.Dir(item.Path), "prompt_history.jsonl")
	if _, err := os.Stat(promptHistory); err == nil {
		err = scanDetailFile(promptHistory, func(record map[string]any, _ string) error {
			if stringValue(record["session_id"]) != item.ID {
				return nil
			}
			prompt := detailText(record["prompt"])
			if prompt == "" {
				return nil
			}
			if detail.CWD == "" {
				detail.CWD = firstDetailWorkingDirectory(record)
			}
			detail.Events = append(detail.Events, model.SessionEvent{
				Timestamp: eventTimestamp(record),
				Role:      "user",
				Kind:      "message",
				Summary:   prompt,
			})
			return nil
		})
		if err != nil {
			return model.AgentSessionDetail{}, fmt.Errorf("read grok prompt history %s: %w", promptHistory, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return model.AgentSessionDetail{}, fmt.Errorf("stat grok prompt history %s: %w", promptHistory, err)
	}

	sortGrokDetailEvents(detail.Events)
	return detail, nil
}

func firstDetailWorkingDirectory(record map[string]any) string {
	for _, directory := range workingDirectories(record) {
		if directory = strings.TrimSpace(directory); directory != "" {
			return directory
		}
	}
	return ""
}

func sortGrokDetailEvents(events []model.SessionEvent) {
	sort.SliceStable(events, func(left, right int) bool {
		leftTimestamp := events[left].Timestamp
		rightTimestamp := events[right].Timestamp
		if leftTimestamp.IsZero() {
			return false
		}
		if rightTimestamp.IsZero() {
			return true
		}
		return leftTimestamp.Before(rightTimestamp)
	})
}
