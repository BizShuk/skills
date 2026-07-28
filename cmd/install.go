package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/bizshuk/skills/svc/agent"
	"github.com/bizshuk/skills/svc/rule"
	"github.com/bizshuk/skills/svc/tui"
	"github.com/spf13/cobra"
)

type installDeps struct {
	agents   func() []agent.Agent
	detected func() []agent.Agent
	pick     func([]agent.Agent) ([]agent.AgentType, error)
	fetch    func(context.Context, string) ([]byte, error)
	apply    func([]byte, []rule.Target) ([]rule.Installed, error)
}

func installCmd() *cobra.Command {
	fetcher := rule.NewFetcher()
	return installCmdWithDeps(installDeps{
		agents:   agent.Agents,
		detected: agent.Detect,
		pick:     tui.RunAgentSelection,
		fetch:    fetcher.Fetch,
		apply:    rule.Apply,
	})
}

func installCmdWithDeps(deps installDeps) *cobra.Command {
	var agentNames []string
	var yes bool

	command := &cobra.Command{
		Use:   "install [url]",
		Short: "Install a global rule into agent configuration",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			sourceURL := rule.DefaultURL
			if len(args) == 1 {
				sourceURL = args[0]
			}
			return runInstall(command, sourceURL, agentNames, yes, deps)
		},
	}

	command.Flags().StringSliceVar(&agentNames, "agent", nil, "override detected target agents")
	command.Flags().BoolVar(&yes, "yes", false, "skip TUI and install into selected or detected agents")
	return command
}

func runInstall(
	command *cobra.Command,
	sourceURL string,
	agentNames []string,
	yes bool,
	deps installDeps,
) error {
	configured := deps.agents()
	candidates := configured
	var err error
	if len(agentNames) > 0 {
		candidates, err = agentsByName(configured, agentNames)
		if err != nil {
			return err
		}
	}

	var selected []agent.Agent
	switch {
	case yes && len(agentNames) > 0:
		selected = candidates
	case yes:
		selected, err = canonicalAgents(configured, agentTypes(deps.detected()))
	default:
		var selectedTypes []agent.AgentType
		selectedTypes, err = deps.pick(candidates)
		if err == nil {
			selected, err = canonicalAgents(candidates, selectedTypes)
		}
	}
	if err != nil {
		return fmt.Errorf("select agents: %w", err)
	}
	if len(selected) == 0 {
		return fmt.Errorf("no agents selected")
	}

	content, err := deps.fetch(command.Context(), sourceURL)
	if err != nil {
		return fmt.Errorf("download global rule: %w", err)
	}

	installed, installErr := deps.apply(content, targetsForAgents(selected))
	successfulAgents := make(map[string]struct{})
	for _, result := range installed {
		names := append([]string(nil), result.Agents...)
		sort.Strings(names)
		for _, name := range names {
			successfulAgents[name] = struct{}{}
		}
		fmt.Fprintf(command.OutOrStdout(), "installed global rule for %s -> %s\n",
			strings.Join(names, ", "), result.Path)
	}
	if len(installed) > 0 {
		fmt.Fprintf(command.OutOrStdout(), "installed global rule into %d path(s) for %d agent(s)\n",
			len(installed), len(successfulAgents))
	}
	if installErr != nil {
		return fmt.Errorf("install global rule: %w", installErr)
	}
	return nil
}

func agentsByName(configured []agent.Agent, names []string) ([]agent.Agent, error) {
	byName := make(map[agent.AgentType]agent.Agent, len(configured))
	supported := make([]string, 0, len(configured))
	for _, configuredAgent := range configured {
		byName[configuredAgent.Type] = configuredAgent
		supported = append(supported, string(configuredAgent.Type))
	}
	sort.Strings(supported)

	seen := make(map[agent.AgentType]struct{}, len(names))
	selected := make([]agent.Agent, 0, len(names))
	for _, name := range names {
		agentType := agent.AgentType(name)
		configuredAgent, ok := byName[agentType]
		if !ok {
			return nil, fmt.Errorf("unknown agent %q; supported agents: %s",
				name, strings.Join(supported, ", "))
		}
		if _, ok := seen[agentType]; ok {
			continue
		}
		seen[agentType] = struct{}{}
		selected = append(selected, configuredAgent)
	}
	return selected, nil
}

func canonicalAgents(configured []agent.Agent, types []agent.AgentType) ([]agent.Agent, error) {
	byType := make(map[agent.AgentType]agent.Agent, len(configured))
	for _, configuredAgent := range configured {
		byType[configuredAgent.Type] = configuredAgent
	}

	seen := make(map[agent.AgentType]struct{}, len(types))
	selected := make([]agent.Agent, 0, len(types))
	for _, agentType := range types {
		configuredAgent, ok := byType[agentType]
		if !ok {
			return nil, fmt.Errorf("unknown selected agent %q", agentType)
		}
		if _, ok := seen[agentType]; ok {
			continue
		}
		seen[agentType] = struct{}{}
		selected = append(selected, configuredAgent)
	}
	return selected, nil
}

func agentTypes(configured []agent.Agent) []agent.AgentType {
	types := make([]agent.AgentType, 0, len(configured))
	for _, configuredAgent := range configured {
		types = append(types, configuredAgent.Type)
	}
	return types
}

func targetsForAgents(configured []agent.Agent) []rule.Target {
	targets := make([]rule.Target, 0, len(configured))
	for _, configuredAgent := range configured {
		targets = append(targets, rule.Target{
			Path:   configuredAgent.GlobalRulePath,
			Agents: []string{string(configuredAgent.Type)},
		})
	}
	return targets
}
