package session

import (
	"path/filepath"
	"strings"

	"github.com/bizshuk/skills/model"
)

func discoverCodex(root, cwd string) ([]model.AgentSession, error) {
	sessions := make([]model.AgentSession, 0)
	err := walkJSONLFiles(root, func(path string) error {
		var metadata sessionMetadata
		err := scanJSONL(path, func(record map[string]any) {
			metadata.addTimestamp(record["timestamp"])
			if record["type"] != "session_meta" {
				return
			}
			payload, ok := record["payload"].(map[string]any)
			if !ok {
				return
			}
			if id, ok := payload["id"].(string); ok {
				metadata.addID(id)
			}
			if directory, ok := payload["cwd"].(string); ok {
				metadata.addWorkingDirectories([]string{directory}, cwd)
			}
		})
		if err != nil {
			return nil
		}
		fallbackID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if session, ok := metadata.session("codex", path, fallbackID); ok {
			sessions = append(sessions, session)
		}
		return nil
	})
	return sessions, err
}
