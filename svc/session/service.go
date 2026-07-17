package session

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bizshuk/skills/model"
	"github.com/bizshuk/skills/svc/agent"
)

// List discovers sessions whose recorded working directory matches cwd.
func List(cwd string) ([]model.AgentSession, error) {
	normalizedCWD, err := normalizePath(cwd)
	if err != nil {
		return nil, err
	}

	byKey := make(map[string]model.AgentSession)
	for _, configured := range agent.Agents() {
		agentName := string(configured.Type)
		for _, root := range configured.SessionDirs {
			if strings.TrimSpace(root) == "" {
				continue
			}
			items, err := discoverProvider(agentName, root, normalizedCWD)
			if err != nil {
				return nil, fmt.Errorf("session: discover %s at %s: %w", agentName, root, err)
			}
			for _, item := range items {
				key := item.Agent + "\x00" + item.ID
				previous, exists := byKey[key]
				if !exists || item.LastActivity.After(previous.LastActivity) {
					byKey[key] = item
				}
			}
		}
	}

	sessions := make([]model.AgentSession, 0, len(byKey))
	for _, item := range byKey {
		sessions = append(sessions, item)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if !sessions[i].LastActivity.Equal(sessions[j].LastActivity) {
			return sessions[i].LastActivity.After(sessions[j].LastActivity)
		}
		if sessions[i].Agent != sessions[j].Agent {
			return sessions[i].Agent < sessions[j].Agent
		}
		return sessions[i].ID < sessions[j].ID
	})
	return sessions, nil
}

func discoverProvider(agentName, root, cwd string) ([]model.AgentSession, error) {
	switch agentName {
	case "claude-code":
		return discoverClaude(root, cwd)
	case "codex":
		return discoverCodex(root, cwd)
	case "grok":
		return discoverGrok(root, cwd)
	case "antigravity", "antigravity-cli", "hermes-agent", "opencode", "pi":
		return discoverStructured(root, cwd, agentName)
	default:
		return nil, nil
	}
}
