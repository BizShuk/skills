package session

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/bizshuk/skills/model"
	_ "modernc.org/sqlite"
)

func discoverCodex(indexPath, cwd string) ([]model.AgentSession, error) {
	info, err := os.Stat(indexPath)
	if errors.Is(err, os.ErrNotExist) {
		return []model.AgentSession{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("session index is not a regular file: %s", indexPath)
	}

	dsn := url.URL{Scheme: "file", Path: indexPath}
	query := dsn.Query()
	query.Set("mode", "ro")
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open Codex session index: %w", err)
	}
	defer func() {
		_ = db.Close() // Read-only connection has no pending writes to flush.
	}()
	db.SetMaxOpenConns(1)

	rows, err := db.Query(`SELECT id, rollout_path,
		COALESCE(created_at_ms, created_at * 1000),
		COALESCE(updated_at_ms, updated_at * 1000)
		FROM threads WHERE cwd = ?`, cwd)
	if err != nil {
		if unsupportedCodexIndexSchema(err) {
			return []model.AgentSession{}, nil
		}
		return nil, fmt.Errorf("query Codex session index: %w", err)
	}
	defer rows.Close()

	var sessions []model.AgentSession
	for rows.Next() {
		var id, path string
		var createdAtMS, updatedAtMS int64
		if err := rows.Scan(&id, &path, &createdAtMS, &updatedAtMS); err != nil {
			return nil, fmt.Errorf("scan Codex session index: %w", err)
		}
		if strings.TrimSpace(id) == "" || strings.TrimSpace(path) == "" {
			continue
		}
		sessions = append(sessions, model.AgentSession{
			Agent:        "codex",
			ID:           id,
			StartedAt:    time.UnixMilli(createdAtMS),
			LastActivity: time.UnixMilli(updatedAtMS),
			Path:         path,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Codex session index: %w", err)
	}
	return sessions, nil
}

func unsupportedCodexIndexSchema(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table: threads") ||
		strings.Contains(message, "no such column:")
}

func loadCodexDetail(item model.AgentSession) (model.AgentSessionDetail, error) {
	detail := model.AgentSessionDetail{Session: item}
	titleFromUser := false
	err := scanDetailFile(item.Path, func(record map[string]any, raw string) error {
		typ := strings.ToLower(strings.TrimSpace(stringValue(record["type"])))
		if typ == "session_meta" {
			if detail.CWD == "" {
				detail.CWD = codexRecordCWD(record)
			}
			return nil
		}
		if detail.CWD == "" {
			detail.CWD = codexRecordCWD(record)
		}

		events := normalizeCodexRecord(record, raw)
		detail.Events = append(detail.Events, events...)
		if !titleFromUser {
			for _, event := range events {
				if event.Role == "user" && event.Kind == "message" && event.Summary != "" {
					detail.Title = event.Summary
					titleFromUser = true
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return model.AgentSessionDetail{}, err
	}

	sortCodexEvents(detail.Events)
	return detail, nil
}

func normalizeCodexRecord(record map[string]any, raw string) []model.SessionEvent {
	switch strings.ToLower(strings.TrimSpace(stringValue(record["type"]))) {
	case "session_meta":
		return nil
	case "event_msg":
		return normalizeCodexEventMessage(record, raw)
	case "response_item":
		return normalizeCodexResponseItem(record, raw)
	default:
		if event, ok := normalizeGenericRecord(record, raw); ok {
			return []model.SessionEvent{event}
		}
		return nil
	}
}

func normalizeCodexEventMessage(record map[string]any, raw string) []model.SessionEvent {
	payload, ok := record["payload"].(map[string]any)
	if !ok {
		return codexRawEvent(record, raw)
	}

	typ := strings.ToLower(strings.TrimSpace(stringValue(payload["type"])))
	switch typ {
	case "user_message":
		return codexPayloadMessage(payload, "user", "message", record)
	case "agent_message":
		return codexPayloadMessage(payload, "assistant", "message", record)
	case "task_started", "task_complete", "task_completed", "turn_aborted", "turn_completed", "context_compacted", "token_count", "thread_started", "thread_ended", "patch_apply_end", "web_search_end", "sub_agent_activity":
		summary := codexPayloadSummary(payload)
		if summary == "" {
			summary = typ
		}
		return []model.SessionEvent{{
			Timestamp: eventTimestamp(record),
			Role:      "system",
			Kind:      "event",
			Summary:   summary,
		}}
	default:
		return codexRawEvent(record, raw)
	}
}

func normalizeCodexResponseItem(record map[string]any, raw string) []model.SessionEvent {
	payload, ok := record["payload"].(map[string]any)
	if !ok {
		return codexRawEvent(record, raw)
	}

	typ := strings.ToLower(strings.TrimSpace(stringValue(payload["type"])))
	switch typ {
	case "message":
		role := normalizeEventRole(stringValue(payload["role"]))
		return codexPayloadMessage(payload, role, "message", record)
	case "user_message":
		return codexPayloadMessage(payload, "user", "message", record)
	case "agent_message":
		return codexPayloadMessage(payload, "assistant", "message", record)
	case "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output":
		summary := codexToolSummary(payload)
		if summary == "" {
			summary = typ
		}
		return []model.SessionEvent{{
			Timestamp: eventTimestamp(record),
			Role:      "tool",
			Kind:      "tool_call",
			Summary:   summary,
		}}
	default:
		return codexRawEvent(record, raw)
	}
}

func codexPayloadMessage(payload map[string]any, fallbackRole, fallbackKey string, record map[string]any) []model.SessionEvent {
	role := normalizeEventRole(stringValue(payload["role"]))
	if role == "" {
		role = fallbackRole
	}

	var summary string
	for _, key := range []string{"content", fallbackKey, "text", "message", "output"} {
		if candidate := detailText(payload[key]); candidate != "" {
			summary = candidate
			break
		}
	}
	if summary == "" {
		return nil
	}
	return []model.SessionEvent{{
		Timestamp: eventTimestamp(record),
		Role:      role,
		Kind:      "message",
		Summary:   summary,
	}}
}

func codexPayloadSummary(payload map[string]any) string {
	for _, key := range []string{"message", "summary", "description", "status", "task_name", "name", "text", "output"} {
		if summary := detailText(payload[key]); summary != "" {
			return summary
		}
	}
	return ""
}

func codexToolSummary(payload map[string]any) string {
	for _, key := range []string{"name", "tool_name", "toolName", "command", "output", "result", "content", "message", "text", "arguments", "input", "call_id"} {
		if summary := detailText(payload[key]); summary != "" {
			return summary
		}
	}
	return ""
}

func codexRawEvent(record map[string]any, raw string) []model.SessionEvent {
	if event, ok := normalizeGenericRecord(record, raw); ok {
		return []model.SessionEvent{event}
	}
	return nil
}

func codexRecordCWD(record map[string]any) string {
	for _, key := range []string{"cwd", "working_directory", "workdir", "workingDirectory"} {
		if directory := strings.TrimSpace(stringValue(record[key])); directory != "" {
			return directory
		}
	}
	if payload, ok := record["payload"].(map[string]any); ok {
		for _, key := range []string{"cwd", "working_directory", "workdir", "workingDirectory"} {
			if directory := strings.TrimSpace(stringValue(payload[key])); directory != "" {
				return directory
			}
		}
	}
	directories := workingDirectories(record)
	if len(directories) > 0 {
		return strings.TrimSpace(directories[0])
	}
	return ""
}

func sortCodexEvents(events []model.SessionEvent) {
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
