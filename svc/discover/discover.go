// Package discover walks a source breadth-first, turning every plugin into
// a node in a plugin tree. Local plugins become Categories directly from
// their on-disk manifests; remote plugins are fetched in parallel per level,
// guarded by a visited set keyed on the lowercased ownerRepo and a max
// depth, and the fetched plugin becomes a child of its parent in the tree
// rather than a sibling at the same level. The tree shape lets the TUI
// render nested plugin/sub-plugin relationships directly.
package discover

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/bizshuk/skills/svc/fetch"
	"github.com/bizshuk/skills/svc/manifest"
	"github.com/bizshuk/skills/svc/source"
	"golang.org/x/sync/errgroup"
)

// Skill is one skill directory within a Category. Description is a short
// summary the TUI renders next to each skill row; it is precomputed by
// manifest.Scan from the first body line of SKILL.md so the TUI never
// has to read files.
type Skill struct {
	Name        string
	Path        string
	Description string
}

// Category is one node in the plugin tree. Skills are the direct leaves of
// this node; Children are nested remote plugins fetched during discovery
// and surfaced as sub-trees. The OwnerRepo is empty for the root of the
// walk and for purely local categories; remote plugins carry the
// lowercased "owner/repo" so the TUI can render provenance and so the
// visited set can dedupe repeats.
//
// FetchOK is true when either the local scan succeeded (Locals case) or
// the remote fetch returned a usable directory. FetchErr is non-empty
// only when FetchOK is false; the TUI surfaces its content next to the
// plugin name.
type Category struct {
	PluginName string
	OwnerRepo  string
	Skills     []Skill
	Children   []*Category // sub-plugins (empty for leaf nodes)
	FetchOK    bool
	FetchErr   string
}

// Catalog is the result of Walk: an ordered list of root-level plugins.
// Root-level plugins are what the user's source points to; remote plugins
// fetched during the walk become Children of their parent, not roots.
type Catalog struct {
	Roots []*Category
}

// AllSkills walks the tree in preorder and returns every Skill — leaf or
// nested — flattened into one slice. The TUI's --yes (non-interactive)
// branch and any other consumer that wants "every skill" without caring
// about the tree shape use this helper.
func (c *Catalog) AllSkills() []Skill {
	if c == nil {
		return nil
	}
	var out []Skill
	var walk func(n *Category)
	walk = func(n *Category) {
		if n == nil {
			return
		}
		out = append(out, n.Skills...)
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	for _, r := range c.Roots {
		walk(r)
	}
	return out
}

// queueEntry is one unit of work in the BFS queue: a parent pointer (nil
// for the initial root materialization), the materialized directory to
// scan, and the depth at which that directory was reached (0 for root).
type queueEntry struct {
	parent *Category
	dir    string
	depth  int
}

// Walk materializes root, then performs a level-by-level BFS over its
// plugins. At each level every queued entry is scanned with manifest.Scan;
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
func Walk(ctx context.Context, f fetch.Fetcher, root source.ParsedSource, maxDepth int) (*Catalog, error) {
	rootDir, err := f.Materialize(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("materialize root: %w", err)
	}

	cat := &Catalog{}
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
				parsed, err := manifest.Scan(n.dir)
				if err != nil {
					// Malformed manifest — drop the whole node silently per
					// the spec's error handling rules.
					return nil
				}

				// Local plugins: each one becomes a Category with its skills,
				// attached under the current parent (or to Roots when this
				// level is the walk's root).
				for _, lp := range parsed.Locals {
					c := &Category{PluginName: lp.Name, FetchOK: true}
					for _, s := range lp.Skills {
						c.Skills = append(c.Skills, Skill{Name: s.Name, Path: s.Path, Description: s.Description})
					}
					rootMu.Lock()
					if n.parent == nil {
						cat.Roots = append(cat.Roots, c)
					} else {
						n.parent.Children = append(n.parent.Children, c)
					}
					rootMu.Unlock()
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
					src := source.ParsedSource{
						Type: source.GitHub,
						URL:  srcURL,
						Ref:  rp.Ref,
					}

					dir, ferr := f.Materialize(gctx, src)
					if ferr != nil {
						rootMu.Lock()
						failed := &Category{
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
					placeholder := &Category{
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
