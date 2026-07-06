package utils

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bizshuk/skills/model"
	"github.com/bizshuk/skills/svc/plugin"
	"golang.org/x/sync/errgroup"
)

type queueEntry struct {
	parent *plugin.Category
	dir    string
	depth  int
}

// Walk materializes root, then performs a level-by-level BFS over its
// plugins. At each level every queued entry is scanned with Scan;
// local plugins attach Categories immediately (under their parent, or to
// Roots when parent is nil); remote plugins are fetched in parallel (via
// errgroup) and either attached as a Category placeholder (whose Children
// will be filled by the next BFS level) on success, or as a failed
// Category on error. The visited set, keyed on the lowercased ownerRepo,
// prevents both infinite loops and duplicate fetches. Remote plugins
// whose would-be depth exceeds maxDepth are silently dropped per the
// design's "depth > maxDepth 停止走訪" semantics — no placeholder
// Category is created for them.
//
// The only error path is failure to materialize root itself; every other
// failure (malformed manifest, unreachable remote) is recorded on the
// relevant Category and the walk continues.
func Walk(ctx context.Context, f plugin.Fetcher, root plugin.ParsedSource, maxDepth int) (*plugin.Catalog, error) {
	rootDir, err := f.Materialize(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("materialize root: %w", err)
	}

	cat := &plugin.Catalog{}
	visited := map[string]bool{}
	queue := []queueEntry{{parent: nil, dir: rootDir, depth: 0}}

	for len(queue) > 0 {
		// Snapshot the current level and reset the queue for the next pass.
		// All goroutines on this level append into a fresh `queue`, so by
		// the time g.Wait returns the next level is fully populated.
		level := queue
		queue = nil

		var (
			rootMu sync.Mutex // protects cat.Roots
			nextMu sync.Mutex // protects `queue`
		)
		g, gctx := errgroup.WithContext(ctx)

		for _, n := range level {
			n := n
			g.Go(func() error {
				parsed, err := plugin.Scan(n.dir)
				if err != nil {
					// Malformed manifest — drop the whole node silently per
					// the spec's error handling rules.
					return nil
				}

				// A local plugin whose base IS the scanned dir is the repo's
				// own root plugin. When we reached this dir by fetching a
				// remote placeholder (n.parent != nil), that root plugin and
				// the placeholder are the same plugin: absorb its skills into
				// the placeholder instead of nesting a same-repo child one
				// level deeper (which showed as a redundant "gosdk -> gosdk"
				// layer). Sub-directory plugins — the genuine members of a
				// multi-plugin marketplace — still nest normally below.
				dirKey := filepath.Clean(n.dir)
				if abs, aerr := filepath.Abs(n.dir); aerr == nil {
					dirKey = filepath.Clean(abs)
				}

				// Local plugins: each one becomes a Category with its skills
				// and subagents, attached under the current parent (or to
				// Roots when this level is the walk's root).
				for _, lp := range parsed.Locals {
					if n.parent != nil && (filepath.Clean(lp.Base) == dirKey || strings.EqualFold(lp.Name, n.parent.PluginName)) {
						rootMu.Lock()
						n.parent.Skills = dedupSkillsByName(append(n.parent.Skills, lp.Skills...))
						n.parent.Subagents = dedupSubagentsByName(append(n.parent.Subagents, lp.Subagents...))
						rootMu.Unlock()
						continue
					}
					c := &plugin.Category{PluginName: lp.Name, FetchOK: true}
					c.Skills = dedupSkillsByName(lp.Skills)
					c.Subagents = dedupSubagentsByName(lp.Subagents)
					rootMu.Lock()
					if n.parent == nil {
						cat.Roots = append(cat.Roots, c)
					} else {
						n.parent.Children = append(n.parent.Children, c)
					}
					rootMu.Unlock()
					// Enqueue the local plugin's dir for further BFS so its own
					// marketplace.json / plugin.json / skill.json gets scanned (handles
					// nested marketplaces like VoltAgent/awesome-claude-code-subagents
					// whose marketplace.json declares sub-plugins in sub-dirs).
					if n.depth+1 <= maxDepth {
						nextMu.Lock()
						queue = append(queue, queueEntry{parent: c, dir: lp.Base, depth: n.depth + 1})
						nextMu.Unlock()
					}
				}

				// Remote plugins: visit-gate, depth-gate, then fetch.
				for _, rp := range parsed.Remotes {
					key := strings.ToLower(rp.OwnerRepo)

					rootMu.Lock()
					if visited[key] {
						rootMu.Unlock()
						continue
					}
					visited[key] = true
					rootMu.Unlock()

					if n.depth+1 > maxDepth {
						// Per design spec §Recursion Semantics: simply stop
						// walking past this plugin. No placeholder Category.
						continue
					}

					srcURL := rp.URL
					if srcURL == "" {
						srcURL = "https://github.com/" + rp.OwnerRepo + ".git"
					}
					src := plugin.ParsedSource{
						Type: plugin.GitHub,
						URL:  srcURL,
						Ref:  rp.Ref,
					}

					dir, ferr := f.Materialize(gctx, src)
					if ferr != nil {
						rootMu.Lock()
						failed := &plugin.Category{
							PluginName: rp.Name,
							OwnerRepo:  rp.OwnerRepo,
							FetchOK:    false,
							FetchErr:   "unable to fetch",
						}
						if n.parent == nil {
							cat.Roots = append(cat.Roots, failed)
						} else {
							n.parent.Children = append(n.parent.Children, failed)
						}
						rootMu.Unlock()
						continue
					}

					// Successful fetch: surface the plugin as a Category
					// placeholder under its parent (whose Children will be
					// filled at the next BFS level once we scan its dir)
					// and enqueue the fetched dir for further discovery.
					placeholder := &plugin.Category{
						PluginName: rp.Name,
						OwnerRepo:  rp.OwnerRepo,
						FetchOK:    true,
					}
					rootMu.Lock()
					if n.parent == nil {
						cat.Roots = append(cat.Roots, placeholder)
					} else {
						n.parent.Children = append(n.parent.Children, placeholder)
					}
					rootMu.Unlock()

					nextMu.Lock()
					queue = append(queue, queueEntry{parent: placeholder, dir: dir, depth: n.depth + 1})
					nextMu.Unlock()
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return cat, err
		}
	}

	return cat, nil
}

// dedupSkillsByName returns a slice with the first occurrence of each
// unique Path preserved. Recursive BFS over local plugin dirs can re-scan
// the same physical skill directory (e.g. when a marketplace.json sub-plugin
// points to a dir that gets re-scanned on the next BFS level), so this dedup
// guarantees that even when the same Skill is appended twice, the user sees
// one row in the TUI.
func dedupSkillsByName(skills []model.Skill) []model.Skill {
	if len(skills) == 0 {
		return skills
	}
	seen := make(map[string]bool, len(skills))
	out := make([]model.Skill, 0, len(skills))
	for _, s := range skills {
		if seen[s.Path] {
			continue
		}
		seen[s.Path] = true
		out = append(out, s)
	}
	return out
}

// dedupSubagentsByName mirrors dedupSkillsByName for the Subagent type.
func dedupSubagentsByName(sas []model.Subagent) []model.Subagent {
	if len(sas) == 0 {
		return sas
	}
	seen := make(map[string]bool, len(sas))
	out := make([]model.Subagent, 0, len(sas))
	for _, sa := range sas {
		if seen[sa.Path] {
			continue
		}
		seen[sa.Path] = true
		out = append(out, sa)
	}
	return out
}
