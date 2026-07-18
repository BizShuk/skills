package utils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bizshuk/skills/model"
	"github.com/bizshuk/skills/svc/plugin"
	"golang.org/x/sync/errgroup"
)

type queueEntry struct {
	parent           *plugin.Category
	dir              string
	depth            int
	skipRemoteSkills bool
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

				attachCategory := func(parent *plugin.Category, c *plugin.Category) {
					rootMu.Lock()
					defer rootMu.Unlock()
					if parent == nil {
						cat.Roots = append(cat.Roots, c)
					} else {
						parent.Children = append(parent.Children, c)
					}
				}

				queueFetchedRemote := func(parent *plugin.Category, rp model.RemotePlugin, depth int, mergeLocalsIntoParent bool, useVisited bool) {
					if useVisited {
						key := strings.ToLower(rp.OwnerRepo)
						rootMu.Lock()
						if visited[key] {
							rootMu.Unlock()
							return
						}
						visited[key] = true
						rootMu.Unlock()
					}

					if depth+1 > maxDepth {
						// Per design spec §Recursion Semantics: simply stop
						// walking past this plugin. No placeholder Category.
						return
					}

					dir, ferr := f.Materialize(gctx, remoteParsedSource(rp))
					if ferr != nil {
						attachCategory(parent, &plugin.Category{
							PluginName: rp.Name,
							OwnerRepo:  rp.OwnerRepo,
							FetchOK:    false,
							FetchErr:   "unable to fetch",
						})
						return
					}
					dir, ferr = remoteScanDir(dir, rp)
					if ferr != nil {
						attachCategory(parent, &plugin.Category{
							PluginName: rp.Name,
							OwnerRepo:  rp.OwnerRepo,
							FetchOK:    false,
							FetchErr:   "unable to fetch",
						})
						return
					}

					if mergeLocalsIntoParent {
						if skill, ok := rootSkillInDir(dir, rp.Name); ok {
							rootMu.Lock()
							parent.Skills = dedupSkillsByName(append(parent.Skills, skill))
							rootMu.Unlock()
						}
						return
					}

					placeholder := &plugin.Category{
						PluginName: rp.Name,
						OwnerRepo:  rp.OwnerRepo,
						FetchOK:    true,
					}
					attachCategory(parent, placeholder)

					nextMu.Lock()
					queue = append(queue, queueEntry{parent: placeholder, dir: dir, depth: depth + 1})
					nextMu.Unlock()
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
						if !n.skipRemoteSkills {
							for _, rp := range lp.RemoteSkills {
								queueFetchedRemote(n.parent, rp, n.depth, true, false)
							}
						}
						continue
					}
					c := &plugin.Category{PluginName: lp.Name, FetchOK: true}
					c.Skills = dedupSkillsByName(lp.Skills)
					c.Subagents = dedupSubagentsByName(lp.Subagents)
					attachCategory(n.parent, c)
					// Enqueue the local plugin's dir for further BFS so its own
					// marketplace.json / plugin.json / skill.json gets scanned (handles
					// nested marketplaces like VoltAgent/awesome-claude-code-subagents
					// whose marketplace.json declares sub-plugins in sub-dirs).
					if n.depth+1 <= maxDepth {
						nextMu.Lock()
						queue = append(queue, queueEntry{parent: c, dir: lp.Base, depth: n.depth + 1, skipRemoteSkills: true})
						nextMu.Unlock()
					}
					for _, rp := range lp.RemoteSkills {
						queueFetchedRemote(c, rp, n.depth, true, false)
					}
				}

				// Remote plugins: visit-gate, depth-gate, then fetch.
				for _, rp := range parsed.Remotes {
					queueFetchedRemote(n.parent, rp, n.depth, false, true)
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

func rootSkillInDir(dir, name string) (model.Skill, bool) {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	if err != nil || info.IsDir() {
		return model.Skill{}, false
	}
	if name == "" {
		name = filepath.Base(dir)
	}
	return model.Skill{
		Name:        name,
		Path:        dir,
		Description: model.ReadDescription(filepath.Join(dir, "SKILL.md")),
	}, true
}

func remoteParsedSource(rp model.RemotePlugin) plugin.ParsedSource {
	srcURL := rp.URL
	if srcURL == "" {
		srcURL = "https://github.com/" + rp.OwnerRepo + ".git"
	}
	return plugin.ParsedSource{
		Type: plugin.GitHub,
		URL:  srcURL,
		Ref:  rp.Ref,
	}
}

func remoteScanDir(dir string, rp model.RemotePlugin) (string, error) {
	if rp.Subdir == "" {
		return dir, nil
	}
	candidate := filepath.Join(dir, rp.Subdir)
	rel, err := filepath.Rel(dir, candidate)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("remote subdir %q escapes fetched repo", rp.Subdir)
	}
	return candidate, nil
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
