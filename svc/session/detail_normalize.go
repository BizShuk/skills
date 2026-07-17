package session

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/bizshuk/skills/model"
)

var detailTimestampKeys = []string{
	"timestamp",
	"created_at",
	"createdAt",
	"updated_at",
	"updatedAt",
	"started_at",
	"startedAt",
	"last_activity",
	"lastActivity",
}

// normalizeGenericRecord converts a common JSON record into a session event.
// It retains unknown valid records as compact raw events for forward compatibility.
func normalizeGenericRecord(record map[string]any, raw string) (model.SessionEvent, bool) {
	if len(record) == 0 {
		return model.SessionEvent{}, false
	}

	typ := strings.ToLower(strings.TrimSpace(stringValue(record["type"])))
	role := normalizeEventRole(stringValue(record["role"]))
	if role == "" {
		role = normalizeEventRole(findNestedString(record, "role"))
	}
	if role == "" && typ == "system" {
		role = "system"
	}

	event := model.SessionEvent{
		Timestamp: eventTimestamp(record),
		Role:      role,
	}
	if isToolEventType(typ) || role == "tool" {
		event.Role = "tool"
		event.Kind = "tool_call"
		event.Summary = toolRecordSummary(record)
		if event.Summary == "" {
			event.Summary = typ
		}
		return event, true
	}

	event.Summary = messageRecordSummary(record)
	if event.Summary != "" {
		event.Kind = "message"
		if isEventType(typ) && role == "system" {
			event.Kind = "event"
		}
		return event, true
	}

	event.Summary = eventRecordSummary(record, typ)
	if event.Summary != "" {
		event.Kind = "event"
		return event, true
	}

	event.Kind = "raw"
	event.Raw = detailRaw(record, raw)
	return event, true
}

// eventTimestamp returns the first recognized timestamp from a record.
func eventTimestamp(record map[string]any) time.Time {
	if timestamp, ok := timestampFromMap(record); ok {
		return timestamp
	}

	for _, key := range []string{"payload", "message", "meta", "info", "event", "data"} {
		if nested, ok := record[key].(map[string]any); ok {
			if timestamp := eventTimestamp(nested); !timestamp.IsZero() {
				return timestamp
			}
		}
	}
	return time.Time{}
}

func timestampFromMap(record map[string]any) (time.Time, bool) {
	for _, key := range detailTimestampKeys {
		if timestamp, ok := parseTimestamp(record[key]); ok {
			return timestamp, true
		}
	}
	return time.Time{}, false
}

func normalizeEventRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "human":
		return "user"
	case "assistant", "agent", "ai", "model":
		return "assistant"
	case "tool", "function":
		return "tool"
	case "system", "developer":
		return "system"
	default:
		return ""
	}
}

func isToolEventType(typ string) bool {
	if typ == "" {
		return false
	}
	for _, marker := range []string{
		"tool_use",
		"tool_call",
		"tool_result",
		"function_call",
		"function_output",
		"custom_tool_call",
		"custom_tool_call_output",
	} {
		if typ == marker || strings.Contains(typ, marker) {
			return true
		}
	}
	return false
}

func isEventType(typ string) bool {
	switch typ {
	case "event", "system", "hook", "status", "error", "warning", "progress", "notification", "summary", "session_summary":
		return true
	default:
		return false
	}
}

func messageRecordSummary(record map[string]any) string {
	for _, key := range []string{"content", "message", "text", "prompt", "response"} {
		if summary := detailText(record[key]); summary != "" {
			return summary
		}
	}
	return ""
}

func eventRecordSummary(record map[string]any, typ string) string {
	for _, key := range []string{"summary", "description", "status", "event"} {
		if summary := detailText(record[key]); summary != "" {
			return summary
		}
	}
	if isEventType(typ) {
		if subtype := strings.TrimSpace(stringValue(record["subtype"])); subtype != "" {
			return subtype
		}
		return typ
	}
	return ""
}

func toolRecordSummary(record map[string]any) string {
	for _, key := range []string{"content", "message", "text", "name", "command", "tool_name", "toolName"} {
		if summary := detailText(record[key]); summary != "" {
			return summary
		}
	}
	return ""
}

func detailText(value any) string {
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, nested := range value {
			if text := detailText(nested); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	case map[string]any:
		for _, key := range []string{"text", "content", "message", "summary", "prompt", "description", "name"} {
			if text := detailText(value[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func findNestedString(value any, key string) string {
	switch value := value.(type) {
	case map[string]any:
		if candidate, ok := value[key].(string); ok {
			return candidate
		}
		for _, nestedKey := range []string{"message", "payload", "data", "event", "item"} {
			if nested := findNestedString(value[nestedKey], key); nested != "" {
				return nested
			}
		}
	case []any:
		for _, nested := range value {
			if found := findNestedString(nested, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func stringValue(value any) string {
	candidate, ok := value.(string)
	if !ok {
		return ""
	}
	return candidate
}

func detailRaw(record map[string]any, raw string) string {
	if strings.TrimSpace(raw) != "" {
		return raw
	}
	data, err := json.Marshal(record)
	if err != nil {
		return ""
	}
	return string(data)
}

func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func sortedMapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
