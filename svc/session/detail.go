package session

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bizshuk/skills/model"
)

var (
	errEmptySessionPath = errors.New("empty session path")
	errUnsupportedAgent = errors.New("unsupported-agent")
)

// LoadDetail reads and normalizes the selected agent session transcript.
func LoadDetail(item model.AgentSession) (model.AgentSessionDetail, error) {
	if strings.TrimSpace(item.Path) == "" {
		return model.AgentSessionDetail{}, detailLoadError(item, errEmptySessionPath)
	}

	var loader func(model.AgentSession) (model.AgentSessionDetail, error)
	switch item.Agent {
	case "claude-code":
		loader = loadClaudeDetail
	case "codex":
		loader = loadCodexDetail
	case "grok":
		loader = loadGrokDetail
	case "antigravity", "antigravity-cli", "hermes-agent", "opencode", "pi":
		loader = loadStructuredDetail
	default:
		return model.AgentSessionDetail{}, detailLoadError(item, errUnsupportedAgent)
	}

	detail, err := loader(item)
	if err != nil {
		return model.AgentSessionDetail{}, detailLoadError(item, err)
	}
	return detail, nil
}

func detailLoadError(item model.AgentSession, err error) error {
	return fmt.Errorf("session: load %s %s: %w", item.Agent, item.Path, err)
}
