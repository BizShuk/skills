package session

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bizshuk/skills/model"
)

var workingDirectoryKeys = map[string]struct{}{
	"cwd":               {},
	"Cwd":               {},
	"working_directory": {},
	"workdir":           {},
	"workingDirectory":  {},
}

var timestampKeys = map[string]struct{}{
	"created_at":    {},
	"createdAt":     {},
	"last_activity": {},
	"lastActivity":  {},
	"started_at":    {},
	"startedAt":     {},
	"timestamp":     {},
	"updated_at":    {},
	"updatedAt":     {},
}

func normalizePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved), nil
	}
	return abs, nil
}

func samePath(left, right string) bool {
	normalizedLeft, err := normalizePath(left)
	if err != nil {
		return false
	}
	normalizedRight, err := normalizePath(right)
	if err != nil {
		return false
	}
	return normalizedLeft == normalizedRight
}

func parseTimestamp(value any) (time.Time, bool) {
	switch value := value.(type) {
	case time.Time:
		return value, !value.IsZero()
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
		if err != nil {
			return time.Time{}, false
		}
		return parsed, true
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return time.Time{}, false
		}
		return parseUnixTimestamp(parsed)
	case float64:
		return parseUnixTimestamp(value)
	case float32:
		return parseUnixTimestamp(float64(value))
	case int:
		return parseUnixTimestamp(float64(value))
	case int64:
		return parseUnixTimestamp(float64(value))
	case uint:
		return parseUnixTimestamp(float64(value))
	case uint64:
		return parseUnixTimestamp(float64(value))
	default:
		return time.Time{}, false
	}
}

func parseUnixTimestamp(value float64) (time.Time, bool) {
	if value <= 0 || value != value {
		return time.Time{}, false
	}
	if value >= 1e12 {
		return time.UnixMilli(int64(value)).UTC(), true
	}
	return time.Unix(int64(value), 0).UTC(), true
}

func workingDirectories(value any) []string {
	var directories []string
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, nested := range value {
				if _, ok := workingDirectoryKeys[key]; ok {
					if directory, ok := nested.(string); ok && strings.TrimSpace(directory) != "" {
						directories = append(directories, directory)
					}
				}
				visit(nested)
			}
		case []any:
			for _, nested := range value {
				visit(nested)
			}
		}
	}
	visit(value)
	return directories
}

func timestamps(value any) []any {
	var values []any
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, nested := range value {
				if _, ok := timestampKeys[key]; ok {
					values = append(values, nested)
				}
				visit(nested)
			}
		case []any:
			for _, nested := range value {
				visit(nested)
			}
		}
	}
	visit(value)
	return values
}

type sessionMetadata struct {
	ID           string
	MatchesCWD   bool
	StartedAt    time.Time
	LastActivity time.Time
}

func (metadata *sessionMetadata) addID(id string) {
	if id = strings.TrimSpace(id); id != "" {
		metadata.ID = id
	}
}

func (metadata *sessionMetadata) addWorkingDirectories(directories []string, cwd string) {
	for _, directory := range directories {
		if samePath(directory, cwd) {
			metadata.MatchesCWD = true
			return
		}
	}
}

func (metadata *sessionMetadata) addTimestamp(value any) {
	timestamp, ok := parseTimestamp(value)
	if !ok {
		return
	}
	if metadata.StartedAt.IsZero() || timestamp.Before(metadata.StartedAt) {
		metadata.StartedAt = timestamp
	}
	if metadata.LastActivity.IsZero() || timestamp.After(metadata.LastActivity) {
		metadata.LastActivity = timestamp
	}
}

func (metadata sessionMetadata) session(agentName, path, fallbackID string) (model.AgentSession, bool) {
	if !metadata.MatchesCWD {
		return model.AgentSession{}, false
	}
	id := strings.TrimSpace(metadata.ID)
	if id == "" {
		id = strings.TrimSpace(fallbackID)
	}
	if id == "" {
		return model.AgentSession{}, false
	}
	return model.AgentSession{
		Agent:        agentName,
		ID:           id,
		StartedAt:    metadata.StartedAt,
		LastActivity: metadata.LastActivity,
		Path:         path,
	}, true
}
