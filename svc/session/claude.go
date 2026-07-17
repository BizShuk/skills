package session

import "github.com/bizshuk/skills/model"

func discoverClaude(root, cwd string) ([]model.AgentSession, error) {
	sessions := make([]model.AgentSession, 0)
	err := walkJSONLFiles(root, func(path string) error {
		var metadata sessionMetadata
		err := scanJSONL(path, func(record map[string]any) {
			if id, ok := record["sessionId"].(string); ok {
				metadata.addID(id)
			}
			metadata.addWorkingDirectories(workingDirectories(record), cwd)
			metadata.addTimestamp(record["timestamp"])
		})
		if err != nil {
			return nil
		}
		if session, ok := metadata.session("claude-code", path, ""); ok {
			sessions = append(sessions, session)
		}
		return nil
	})
	return sessions, err
}
