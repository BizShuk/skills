package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bizshuk/skills/svc/agent"
	"github.com/bizshuk/skills/svc/rule"
	"github.com/stretchr/testify/require"
)

func TestInstallCommandUsesDefaultURLAndDetectedAgentsWithYes(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "codex", "AGENTS.md")
	configured := []agent.Agent{
		{Type: "codex", DisplayName: "Codex", GlobalRulePath: targetPath},
		{Type: "pi", DisplayName: "Pi", GlobalRulePath: filepath.Join(t.TempDir(), "pi", "AGENTS.md")},
	}

	var fetchedURL string
	var appliedContent []byte
	var appliedTargets []rule.Target
	deps := installDeps{
		agents:   func() []agent.Agent { return configured },
		detected: func() []agent.Agent { return configured[:1] },
		pick: func([]agent.Agent) ([]agent.AgentType, error) {
			t.Fatal("interactive picker must not run with --yes")
			return nil, nil
		},
		fetch: func(_ context.Context, sourceURL string) ([]byte, error) {
			fetchedURL = sourceURL
			return []byte("global rules\n"), nil
		},
		apply: func(content []byte, targets []rule.Target) ([]rule.Installed, error) {
			appliedContent = append([]byte(nil), content...)
			appliedTargets = append([]rule.Target(nil), targets...)
			return []rule.Installed{{Path: targetPath, Agents: []string{"codex"}}}, nil
		},
	}

	command := installCmdWithDeps(deps)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--yes"})

	require.NoError(t, command.Execute())
	require.Equal(t, rule.DefaultURL, fetchedURL)
	require.Equal(t, []byte("global rules\n"), appliedContent)
	require.Equal(t, []rule.Target{{Path: targetPath, Agents: []string{"codex"}}}, appliedTargets)
	require.Contains(t, output.String(), "installed global rule for codex -> "+targetPath)
	require.Contains(t, output.String(), "installed global rule into 1 path(s) for 1 agent(s)")
}

func TestInstallCommandUsesCustomURLAndExplicitAgent(t *testing.T) {
	configured := []agent.Agent{
		{Type: "codex", DisplayName: "Codex", GlobalRulePath: filepath.Join(t.TempDir(), "codex", "AGENTS.md")},
		{Type: "pi", DisplayName: "Pi", GlobalRulePath: filepath.Join(t.TempDir(), "pi", "AGENTS.md")},
	}
	const customURL = "https://example.test/rules.md"

	var fetchedURL string
	var appliedTargets []rule.Target
	deps := installDeps{
		agents: func() []agent.Agent { return configured },
		detected: func() []agent.Agent {
			t.Fatal("detection must not run for an explicit --agent selection")
			return nil
		},
		pick: func([]agent.Agent) ([]agent.AgentType, error) {
			t.Fatal("interactive picker must not run with --yes")
			return nil, nil
		},
		fetch: func(_ context.Context, sourceURL string) ([]byte, error) {
			fetchedURL = sourceURL
			return []byte("custom"), nil
		},
		apply: func(_ []byte, targets []rule.Target) ([]rule.Installed, error) {
			appliedTargets = append([]rule.Target(nil), targets...)
			return []rule.Installed{{Path: configured[0].GlobalRulePath, Agents: []string{"codex"}}}, nil
		},
	}

	command := installCmdWithDeps(deps)
	command.SetOut(&bytes.Buffer{})
	command.SetArgs([]string{"--yes", "--agent", "codex", "--agent", "codex", customURL})

	require.NoError(t, command.Execute())
	require.Equal(t, customURL, fetchedURL)
	require.Equal(t, []rule.Target{{
		Path:   configured[0].GlobalRulePath,
		Agents: []string{"codex"},
	}}, appliedTargets)
}

func TestInstallCommandRejectsUnknownAgentBeforeFetch(t *testing.T) {
	fetched := false
	deps := installDeps{
		agents: func() []agent.Agent {
			return []agent.Agent{{Type: "codex", GlobalRulePath: filepath.Join(t.TempDir(), "AGENTS.md")}}
		},
		detected: func() []agent.Agent { return nil },
		pick:     func([]agent.Agent) ([]agent.AgentType, error) { return nil, nil },
		fetch: func(context.Context, string) ([]byte, error) {
			fetched = true
			return nil, nil
		},
		apply: func([]byte, []rule.Target) ([]rule.Installed, error) { return nil, nil },
	}

	command := installCmdWithDeps(deps)
	command.SetArgs([]string{"--yes", "--agent", "missing"})

	err := command.Execute()
	require.ErrorContains(t, err, `unknown agent "missing"`)
	require.False(t, fetched)
}

func TestInstallCommandInteractivePickerReceivesAllAgents(t *testing.T) {
	configured := []agent.Agent{
		{Type: "codex", DisplayName: "Codex", GlobalRulePath: filepath.Join(t.TempDir(), "codex", "AGENTS.md")},
		{Type: "pi", DisplayName: "Pi", GlobalRulePath: filepath.Join(t.TempDir(), "pi", "AGENTS.md")},
	}

	var pickerAgents []agent.Agent
	var appliedTargets []rule.Target
	deps := installDeps{
		agents: func() []agent.Agent { return configured },
		detected: func() []agent.Agent {
			t.Fatal("detection is performed by the interactive picker")
			return nil
		},
		pick: func(candidates []agent.Agent) ([]agent.AgentType, error) {
			pickerAgents = append([]agent.Agent(nil), candidates...)
			return []agent.AgentType{"pi"}, nil
		},
		fetch: func(context.Context, string) ([]byte, error) {
			return []byte("rules"), nil
		},
		apply: func(_ []byte, targets []rule.Target) ([]rule.Installed, error) {
			appliedTargets = append([]rule.Target(nil), targets...)
			return []rule.Installed{{Path: configured[1].GlobalRulePath, Agents: []string{"pi"}}}, nil
		},
	}

	command := installCmdWithDeps(deps)
	command.SetOut(&bytes.Buffer{})

	require.NoError(t, command.Execute())
	require.Equal(t, configured, pickerAgents)
	require.Equal(t, []rule.Target{{
		Path:   configured[1].GlobalRulePath,
		Agents: []string{"pi"},
	}}, appliedTargets)
}

func TestInstallCommandGroupsSharedPath(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "gemini", "GEMINI.md")
	configured := []agent.Agent{
		{Type: "antigravity", DisplayName: "Antigravity", GlobalRulePath: targetPath},
		{Type: "antigravity-cli", DisplayName: "Antigravity CLI", GlobalRulePath: targetPath},
	}
	deps := installDeps{
		agents:   func() []agent.Agent { return configured },
		detected: func() []agent.Agent { return nil },
		pick:     func([]agent.Agent) ([]agent.AgentType, error) { return nil, nil },
		fetch: func(context.Context, string) ([]byte, error) {
			return []byte("shared rules\n"), nil
		},
		apply: rule.Apply,
	}

	command := installCmdWithDeps(deps)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--yes", "--agent", "antigravity,antigravity-cli"})

	require.NoError(t, command.Execute())
	content, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, []byte("shared rules\n"), content)
	require.Contains(t, output.String(), "installed global rule for antigravity, antigravity-cli -> "+targetPath)
	require.Contains(t, output.String(), "installed global rule into 1 path(s) for 2 agent(s)")
}

func TestInstallCommandDoesNotFetchWithoutSelection(t *testing.T) {
	fetched := false
	deps := installDeps{
		agents:   func() []agent.Agent { return []agent.Agent{{Type: "codex", GlobalRulePath: "unused"}} },
		detected: func() []agent.Agent { return nil },
		pick:     func([]agent.Agent) ([]agent.AgentType, error) { return nil, nil },
		fetch: func(context.Context, string) ([]byte, error) {
			fetched = true
			return nil, nil
		},
		apply: func([]byte, []rule.Target) ([]rule.Installed, error) { return nil, nil },
	}

	command := installCmdWithDeps(deps)
	err := command.Execute()

	require.ErrorContains(t, err, "no agents selected")
	require.False(t, fetched)
}
