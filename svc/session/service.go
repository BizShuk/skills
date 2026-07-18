package session

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/bizshuk/skills/model"
	"github.com/bizshuk/skills/svc/agent"
)

type providerDiscoverer func(agentName, root, cwd string) ([]model.AgentSession, error)

type agentScanResult struct {
	items []model.AgentSession
	err   error
}

// List discovers sessions whose recorded working directory matches cwd.
func List(cwd string) ([]model.AgentSession, error) {
	normalizedCWD, err := normalizePath(cwd)
	if err != nil {
		return nil, err
	}
	return listAgents(agent.Agents(), normalizedCWD, discoverProvider)
}

func listAgents(configured []agent.Agent, cwd string, discover providerDiscoverer) ([]model.AgentSession, error) {
	results := make([]agentScanResult, len(configured))
	var wait sync.WaitGroup
	wait.Add(len(configured))
	for index, configuredAgent := range configured {
		go func(index int, configuredAgent agent.Agent) {
			defer wait.Done()
			results[index].items, results[index].err = scanAgent(configuredAgent, cwd, discover)
		}(index, configuredAgent)
	}
	wait.Wait()

	byKey := make(map[string]model.AgentSession)
	for _, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		for _, item := range result.items {
			key := item.Agent + "\x00" + item.ID
			previous, exists := byKey[key]
			if !exists || item.LastActivity.After(previous.LastActivity) {
				byKey[key] = item
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

func scanAgent(configured agent.Agent, cwd string, discover providerDiscoverer) ([]model.AgentSession, error) {
	agentName := string(configured.Type)
	var sessions []model.AgentSession
	sources := configured.SessionDirs
	if strings.TrimSpace(configured.SessionIndex) != "" {
		sources = []string{configured.SessionIndex}
	}
	for _, root := range sources {
		if strings.TrimSpace(root) == "" {
			continue
		}
		items, err := discover(agentName, root, cwd)
		if err != nil {
			return nil, fmt.Errorf("session: discover %s at %s: %w", agentName, root, err)
		}
		sessions = append(sessions, items...)
	}
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
	default:
		return nil, nil
	}
}
