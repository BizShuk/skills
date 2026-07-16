// Package cmd assembles the skills CLI's cobra command tree. The package
// exposes Execute() which returns an error rather than calling os.Exit
// directly so callers (e.g. the root-level main.go in this repo) decide
// how to surface failures. This package also accepts being embedded into
// a larger tool that wants the "skills" subcommand set as part of its
// own root.
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bizshuk/skills/cmd/stats"
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

	var (
		removeAgents  []string
		removeGlobal  bool
		removeProject bool
		removeYes     bool
	)
	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Interactively delete installed skills and subagents",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd, removeAgents, removeGlobal, removeProject, removeYes)
		},
	}
	removeCmd.Flags().StringSliceVar(&removeAgents, "agent", nil, "limit to specific agents (repeatable)")
	removeCmd.Flags().BoolVar(&removeGlobal, "global", false, "only show global-scope installs")
	removeCmd.Flags().BoolVar(&removeProject, "project", false, "only show project-scope installs")
	removeCmd.Flags().BoolVar(&removeYes, "yes", false, "auto-check all and skip the y/N confirm prompt")
	removeCmd.MarkFlagsMutuallyExclusive("global", "project")
	root.AddCommand(removeCmd)
	root.AddCommand(stats.StatsCmd())

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

// runRemove is the body of `skills remove`. It is split out of the inline
// closure so it can grow without making the cobra wiring unreadable.
//
// The flow:
//  1. Discover every installed skill/subagent (across all agents and
//     both scopes), then apply the --agent / --global / --project filters.
//  2. Hand the filtered list to the TUI — or auto-check everything under
//     --yes (the script-friendly path that skips both TUI and confirm).
//  3. Print a summary and ask "Delete N items? [y/N]" on stdin unless
//     --yes.
//  4. Call agent.Remove to delete from disk, then sync installs.json via
//     update.DropNames so future `skills update` doesn't re-create what
//     was just deleted.
func runRemove(cmd *cobra.Command, agentFilter []string, onlyGlobal, onlyProject, yes bool) error {
	allItems, err := agent.DiscoverInstalled()
	if err != nil {
		return fmt.Errorf("discover installed: %w", err)
	}

	items := filterItems(allItems, agentFilter, onlyGlobal, onlyProject)
	if len(items) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no installed skills or subagents match the filters")
		return nil
	}

	var sel agent.RemoveSelection
	if yes {
		// Auto-check every filtered item. The caller already opted in to
		// non-interactive mode; no TUI, no confirm.
		sel.Items = items
	} else {
		sel, err = tui.RunRemove(items)
		if err != nil {
			return fmt.Errorf("tui: %w", err)
		}
		if len(sel.Items) == 0 {
			// Cancelled (esc) or committed with zero checked rows.
			return nil
		}

		if !confirmDelete(cmd, sel.Items) {
			fmt.Fprintln(cmd.OutOrStdout(), "aborted")
			return nil
		}
	}

	removed, removeErr := agent.Remove(sel)
	syncErr := syncInstallsAfterRemove(removed)

	var n int
	n += len(removed.ProjectSkills)
	n += len(removed.ProjectSubagents)
	n += len(removed.GlobalSkills)
	n += len(removed.GlobalSubagents)
	fmt.Fprintf(cmd.OutOrStdout(), "removed %d item(s)\n", n)

	// removeErr wins over syncErr in the return — a failed disk delete is
	// more visible to the user than a metadata sync glitch.
	if removeErr != nil {
		return fmt.Errorf("remove: %w", removeErr)
	}
	if syncErr != nil {
		return fmt.Errorf("sync installs file: %w", syncErr)
	}
	return nil
}

// filterItems applies the --agent, --global, --project filters to the
// discovery result. An empty agentFilter means "every agent". Each item
// already carries a single Scope (DiscoverInstalled splits project and
// global into separate rows), so --global / --project filtering reduces
// to "keep / drop" per item rather than per location.
func filterItems(items []agent.InstalledItem, agentFilter []string, onlyGlobal, onlyProject bool) []agent.InstalledItem {
	if len(agentFilter) == 0 && !onlyGlobal && !onlyProject {
		return items
	}

	agentSet := make(map[agent.AgentType]bool, len(agentFilter))
	for _, name := range agentFilter {
		agentSet[agent.AgentType(name)] = true
	}

	out := make([]agent.InstalledItem, 0, len(items))
itemLoop:
	for _, it := range items {
		if onlyGlobal && it.Scope != agent.ScopeGlobal {
			continue
		}
		if onlyProject && it.Scope != agent.ScopeProject {
			continue
		}

		// Trim Locations to agents the user asked for; if no agent is left
		// after the filter, drop the item entirely.
		filtered := it.Locations[:0]
		for _, loc := range it.Locations {
			if len(agentSet) > 0 && !agentSet[loc.Agent] {
				continue
			}
			filtered = append(filtered, loc)
		}
		if len(filtered) == 0 {
			continue itemLoop
		}
		it.Locations = filtered
		out = append(out, it)
	}
	return out
}

// confirmDelete prints a human-readable summary of what would be deleted
// and reads a y/N answer from stdin. Returns true only on an exact "y" or
// "Y" answer; everything else (including EOF) is treated as "no".
func confirmDelete(cmd *cobra.Command, items []agent.InstalledItem) bool {
	var b strings.Builder
	b.WriteString("Will delete:\n")
	for _, it := range items {
		fmt.Fprintf(&b, "  - %s (%s, %s)", it.Name, it.Kind, it.Scope)
		agents := make([]string, 0, len(it.Locations))
		for _, loc := range it.Locations {
			agents = append(agents, string(loc.Agent))
		}
		b.WriteString("  [")
		b.WriteString(strings.Join(agents, ", "))
		b.WriteString("]\n")
	}
	b.WriteString(fmt.Sprintf("Delete %d item(s)? [y/N] ", len(items)))
	fmt.Fprint(cmd.ErrOrStderr(), b.String())

	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	ans := strings.TrimSpace(strings.ToLower(line))
	return ans == "y" || ans == "yes"
}

// syncInstallsAfterRemove drops the removed skill and subagent names from
// the installs.json metadata, broken into per-scope buckets. Project names
// never touch global entries (and vice versa) so a user can remove just
// the project-scope copy without invalidating the global tracking. Entries
// whose Skills and Subagents lists are both empty after the drop are
// removed outright, so a later `skills update` won't try to re-install
// into empty slots. Dropped entries are logged to stderr.
func syncInstallsAfterRemove(removed agent.RemovedNames) error {
	if len(removed.ProjectSkills) == 0 &&
		len(removed.ProjectSubagents) == 0 &&
		len(removed.GlobalSkills) == 0 &&
		len(removed.GlobalSubagents) == 0 {
		return nil
	}
	f, err := update.Load()
	if err != nil {
		return err
	}
	dropped := update.DropNamesByScope(f, removed)
	for _, e := range dropped {
		fmt.Fprintf(os.Stderr, "dropped install entry with no remaining items: source=%s scope=%s\n", e.Source, e.Scope)
	}
	return update.Save(f)
}
