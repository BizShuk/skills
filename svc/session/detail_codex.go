package session

import (
	"sort"
	"strings"

	"github.com/bizshuk/skills/model"
)

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
		return codexPayloadMessage(payload, "user", record)
	case "agent_message":
		return codexPayloadMessage(payload, "assistant", record)
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
		return codexPayloadMessage(payload, role, record)
	case "user_message":
		return codexPayloadMessage(payload, "user", record)
	case "agent_message":
		return codexPayloadMessage(payload, "assistant", record)
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

func codexPayloadMessage(payload map[string]any, fallbackRole string, record map[string]any) []model.SessionEvent {
	role := normalizeEventRole(stringValue(payload["role"]))
	if role == "" {
		role = fallbackRole
	}

	var summary string
	for _, key := range []string{"content", "message", "text", "output"} {
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
