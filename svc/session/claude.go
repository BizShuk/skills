package session

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bizshuk/skills/model"
)

func discoverClaude(root, cwd string) ([]model.AgentSession, error) {
	projectPath := filepath.Join(root, claudeProjectKey(cwd))
	entries, err := os.ReadDir(projectPath)
	if errors.Is(err, os.ErrNotExist) {
		return []model.AgentSession{}, nil
	}
	if err != nil {
		return nil, err
	}

	sessions := make([]model.AgentSession, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}

		path := filepath.Join(projectPath, entry.Name())
		activity := info.ModTime()
		sessions = append(sessions, model.AgentSession{
			Agent:        "claude-code",
			ID:           strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			StartedAt:    activity,
			LastActivity: activity,
			Path:         path,
		})
	}
	return sessions, nil
}

func claudeProjectKey(cwd string) string {
	return strings.ReplaceAll(filepath.ToSlash(cwd), "/", "-")
}

func loadClaudeDetail(item model.AgentSession) (model.AgentSessionDetail, error) {
	detail := model.AgentSessionDetail{Session: item}
	err := scanDetailFile(item.Path, func(record map[string]any, raw string) error {
		if detail.CWD == "" {
			detail.CWD = claudeRecordCWD(record)
		}

		events := normalizeClaudeRecord(record, raw)
		detail.Events = append(detail.Events, events...)
		if detail.Title == "" {
			for _, event := range events {
				if event.Role == "user" && event.Kind == "message" && event.Summary != "" {
					detail.Title = event.Summary
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return model.AgentSessionDetail{}, err
	}

	sortClaudeEvents(detail.Events)
	return detail, nil
}

func normalizeClaudeRecord(record map[string]any, raw string) []model.SessionEvent {
	typ := strings.ToLower(strings.TrimSpace(stringValue(record["type"])))
	role := normalizeEventRole(stringValue(record["role"]))
	if message, ok := record["message"].(map[string]any); ok {
		if messageRole := normalizeEventRole(stringValue(message["role"])); messageRole != "" {
			role = messageRole
		}
	}
	if role == "" {
		role = normalizeEventRole(typ)
	}

	if typ == "system" || role == "system" {
		if event, ok := normalizeGenericRecord(record, raw); ok {
			return []model.SessionEvent{event}
		}
	}

	timestamp := eventTimestamp(record)
	if message, ok := record["message"].(map[string]any); ok {
		if events := normalizeClaudeContent(message["content"], role, timestamp); len(events) > 0 {
			return events
		}
	}
	if events := normalizeClaudeContent(record["content"], role, timestamp); len(events) > 0 {
		return events
	}

	if event, ok := normalizeGenericRecord(record, raw); ok {
		return []model.SessionEvent{event}
	}
	return nil
}

func normalizeClaudeContent(content any, role string, timestamp time.Time) []model.SessionEvent {
	switch content := content.(type) {
	case string:
		return claudeTextEvent(content, role, timestamp)
	case map[string]any:
		if event, ok := normalizeClaudeContentBlock(content, role, timestamp); ok {
			return []model.SessionEvent{event}
		}
	case []any:
		events := make([]model.SessionEvent, 0, len(content))
		for _, block := range content {
			switch block := block.(type) {
			case string:
				events = append(events, claudeTextEvent(block, role, timestamp)...)
			case map[string]any:
				if event, ok := normalizeClaudeContentBlock(block, role, timestamp); ok {
					events = append(events, event)
				}
			}
		}
		return events
	}
	return nil
}

func normalizeClaudeContentBlock(block map[string]any, role string, timestamp time.Time) (model.SessionEvent, bool) {
	typ := strings.ToLower(strings.TrimSpace(stringValue(block["type"])))
	if typ == "tool_use" || typ == "tool_result" {
		summary := toolRecordSummary(block)
		if summary == "" {
			summary = typ
		}
		return model.SessionEvent{
			Timestamp: timestamp,
			Role:      "tool",
			Kind:      "tool_call",
			Summary:   summary,
		}, true
	}

	if typ == "text" || typ == "input_text" || typ == "output_text" || typ == "" {
		events := claudeTextEvent(detailText(block["text"]), role, timestamp)
		if len(events) == 0 {
			return model.SessionEvent{}, false
		}
		return events[0], true
	}
	return model.SessionEvent{}, false
}

func claudeTextEvent(text, role string, timestamp time.Time) []model.SessionEvent {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	kind := "message"
	if role == "tool" {
		kind = "tool_call"
	}
	return []model.SessionEvent{{
		Timestamp: timestamp,
		Role:      role,
		Kind:      kind,
		Summary:   text,
	}}
}

func claudeRecordCWD(record map[string]any) string {
	for _, key := range []string{"cwd", "working_directory", "workdir", "workingDirectory"} {
		if directory := strings.TrimSpace(stringValue(record[key])); directory != "" {
			return directory
		}
	}
	directories := workingDirectories(record)
	if len(directories) > 0 {
		return strings.TrimSpace(directories[0])
	}
	return ""
}

func sortClaudeEvents(events []model.SessionEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		left, right := events[i].Timestamp, events[j].Timestamp
		if left.IsZero() {
			return false
		}
		if right.IsZero() {
			return true
		}
		return left.Before(right)
	})
}
