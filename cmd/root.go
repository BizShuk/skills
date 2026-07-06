// Package cmd assembles the skills CLI's cobra command tree. The package
// exposes Execute() which returns an error rather than calling os.Exit
// directly so callers (e.g. the root-level main.go in this repo) decide
// how to surface failures. This package also accepts being embedded into
// a larger tool that wants the "skills" subcommand set as part of its
// own root.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bizshuk/skills/svc/agent"
	"github.com/bizshuk/skills/svc/plugin"
	"github.com/bizshuk/skills/svc/tui"
	"github.com/bizshuk/skills/svc/update"
	"github.com/bizshuk/skills/utils"
)

// Execute builds the cobra command tree and runs it with os.Args.
// Errors from cobra or from any RunE are returned to the caller; the
// root-level main in this repo prints them and exits non-zero.
func Execute() error {
	root := &cobra.Command{Use: "skills", SilenceUsage: true}
	var global bool
	var agents []string
	var depth int
	var yes bool

	add := &cobra.Command{
		Use:   "add [path]",
		Short: "Discover and install skills from a source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			src, err := plugin.Parse(args[0])
			if err != nil {
				return fmt.Errorf("source: %w", err)
			}

			cat, err := utils.Walk(ctx, plugin.New(), src, depth)
			if err != nil {
				return fmt.Errorf("discover: %w", err)
			}

			var targets []agent.Agent
			switch {
			case len(agents) > 0:
				// --agent explicitly overrides the target set, for both the
				// TUI and --yes paths.
				table := agent.Agents()
				byName := make(map[agent.AgentType]agent.Agent, len(table))
				for _, a := range table {
					byName[a.Type] = a
				}
				for _, name := range agents {
					if a, ok := byName[agent.AgentType(name)]; ok {
						targets = append(targets, a)
					}
				}
			case yes:
				// Non-interactive: install into whatever's already detected.
				targets = agent.Detect()
			default:
				// Interactive: show every known agent so the user can pick
				// freely; the TUI's agent phase pre-checks only the agents
				// it detects on disk (see tui.defaultCheckedAgentTypes).
				targets = agent.Agents()
			}

			var sel agent.Selection
			if yes {
				for _, s := range cat.AllSkills() {
					sel.SkillPaths = append(sel.SkillPaths, s.Path)
				}
				for _, sa := range cat.AllSubagents() {
					sel.SubagentPaths = append(sel.SubagentPaths, sa.Path)
				}
				for _, a := range targets {
					sel.AgentTypes = append(sel.AgentTypes, a.Type)
				}
				sel.Global = global
			} else {
				// tui.Run drives the full skill/agent/level selection; its
				// returned Selection already reflects the user's choices at
				// every phase, so nothing further needs to be merged in.
				sel, err = tui.Run(cat, targets, global)
				if err != nil {
					return fmt.Errorf("tui: %w", err)
				}
			}

			if len(sel.SkillPaths) == 0 && len(sel.SubagentPaths) == 0 {
				return fmt.Errorf("no skills or subagents selected")
			}

			if err := agent.Apply(sel); err != nil {
				return fmt.Errorf("install: %w", err)
			}

			// Record install metadata for future "skills update".
			recordInstall(args[0], sel)

			fmt.Fprintf(cmd.OutOrStdout(), "installed %d skill(s), %d subagent(s) into %d agent(s)\n",
				len(sel.SkillPaths), len(sel.SubagentPaths), len(sel.AgentTypes))
			return nil
		},
	}
	add.Flags().BoolVar(&global, "global", false, "install into user-level dirs")
	add.Flags().StringSliceVar(&agents, "agent", nil, "override detected target agents")
	add.Flags().IntVar(&depth, "depth", 3, "max recursion depth")
	add.Flags().BoolVar(&yes, "yes", false, "skip TUI, install all detected")
	root.AddCommand(add)

	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Re-install tracked skills from their original sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			return update.Run(args)
		},
	}
	root.AddCommand(updateCmd)

	return root.Execute()
}

// recordInstall persists the install metadata so "skills update" can
// reproduce this installation later.
func recordInstall(source string, sel agent.Selection) {
	f, err := update.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot load installs file: %v\n", err)
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot resolve cwd: %v\n", err)
		return
	}

	scope := update.ScopeProject
	if sel.Global {
		scope = update.ScopeGlobal
		cwd = "" // project path irrelevant for global installs
	}

	// Collect skill and subagent names from their paths.
	var skillNames []string
	for _, p := range sel.SkillPaths {
		skillNames = append(skillNames, agent.SkillNameFromPath(p))
	}
	var subagentNames []string
	for _, p := range sel.SubagentPaths {
		subagentNames = append(subagentNames, agent.SubagentNameFromPath(p))
	}

	var agentNames []string
	for _, t := range sel.AgentTypes {
		agentNames = append(agentNames, string(t))
	}

	update.Upsert(f, update.Entry{
		Source:      source,
		ProjectPath: cwd,
		Agents:      agentNames,
		Scope:       scope,
		Depth:       3, // default depth; not currently plumbed from sel
		Skills:      skillNames,
		Subagents:   subagentNames,
	})

	if err := update.Save(f); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot save installs file: %v\n", err)
	}
}
