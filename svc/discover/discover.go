// Package discover walks a source breadth-first, turning every plugin into
// a Category. Local plugins become Categories directly from their on-disk
// manifests; remote plugins are fetched in parallel per level, guarded by
// a visited set keyed on the lowercased ownerRepo and a max depth.
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

// Skill is one skill directory within a Category.
type Skill struct {
	Name string
	Path string
}

// Category groups the skills contributed by one plugin.
//
// Local plugins (no remote parent) leave OwnerRepo empty. Remote plugins
// carry the normalized lowercase "owner/repo" so the TUI can render source
// provenance and so the visited set can dedupe repeats.
//
// FetchOK is true when either the local scan succeeded (Locals case) or the
// remote fetch returned a usable directory. FetchErr is non-empty only when
// FetchOK is false; the TUI surfaces its content next to the plugin name.
type Category struct {
	PluginName string
	OwnerRepo  string
	Skills     []Skill
	FetchOK    bool
	FetchErr   string
}

// Catalog is the full ordered result of Walk. Order matches BFS discovery
// (root first, then each level's plugins in the order manifest.Scan returned
// them, with parallel-fetched plugins interleaved at the whim of errgroup).
type Catalog []Category

// node is one unit of work in the BFS queue: an already-materialized dir
// plus the depth at which it was discovered.
type node struct {
	dir   string
	depth int
}

// Walk materializes root, then performs a level-by-level BFS over its
// plugins. At each level every queued node is scanned with manifest.Scan;
// local plugins append Categories immediately, remote plugins are fetched
// in parallel (via errgroup) and either appended as Categories on success
// or as failed Categories on error. The visited set, keyed on the
// lowercased ownerRepo, prevents both infinite loops and duplicate fetches.
// Remote plugins whose would-be depth exceeds maxDepth are silently dropped
// per the design's "depth > maxDepth 停止走訪" semantics — no placeholder
// Category is created for them.
//
// The only error path is failure to materialize root itself; every other
// failure (malformed manifest, unreachable remote) is recorded on the
// relevant Category and the walk continues.
func Walk(ctx context.Context, f fetch.Fetcher, root source.ParsedSource, maxDepth int) (Catalog, error) {
	rootDir, err := f.Materialize(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("materialize root: %w", err)
	}

	var (
		mu      sync.Mutex
		cat     Catalog
		visited = map[string]bool{}
	)
	queue := []node{{dir: rootDir, depth: 0}}

	for len(queue) > 0 {
		// Snapshot the current level and reset the queue for the next pass.
		// All goroutines on this level append into a fresh `queue`, so by
		// the time g.Wait returns the next level is fully populated.
		level := queue
		queue = nil

		var nextMu sync.Mutex
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

				// Local plugins: each one becomes a Category with its skills.
				for _, lp := range parsed.Locals {
					c := Category{PluginName: lp.Name, FetchOK: true}
					for _, s := range lp.Skills {
						c.Skills = append(c.Skills, Skill{Name: s.Name, Path: s.Path})
					}
					mu.Lock()
					cat = append(cat, c)
					mu.Unlock()
				}

				// Remote plugins: visit-gate, depth-gate, then fetch.
				for _, rp := range parsed.Remotes {
					key := strings.ToLower(rp.OwnerRepo)

					mu.Lock()
					if visited[key] {
						mu.Unlock()
						continue
					}
					visited[key] = true
					mu.Unlock()

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
						mu.Lock()
						cat = append(cat, Category{
							PluginName: rp.Name,
							OwnerRepo:  rp.OwnerRepo,
							FetchOK:    false,
							FetchErr:   "unable to fetch",
						})
						mu.Unlock()
						continue
					}

					// Successful fetch: surface the plugin as a Category so
					// the TUI can list it, and queue its dir for further
					// discovery at the next depth level.
					mu.Lock()
					cat = append(cat, Category{
						PluginName: rp.Name,
						OwnerRepo:  rp.OwnerRepo,
						FetchOK:    true,
					})
					mu.Unlock()

					nextMu.Lock()
					queue = append(queue, node{dir: dir, depth: n.depth + 1})
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