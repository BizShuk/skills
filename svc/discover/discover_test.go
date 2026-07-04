package discover

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bizshuk/skills/svc/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFetcher maps an ownerRepo substring to a prepared local dir. Unknown
// repos fall through to os.ErrNotExist so the caller observes a fetch failure.
type fakeFetcher struct{ repos map[string]string }

func (f fakeFetcher) Materialize(_ context.Context, s source.ParsedSource) (string, error) {
	if s.Type == source.Local {
		return s.LocalPath, nil
	}
	for or, dir := range f.repos {
		if strings.Contains(s.URL, or) {
			return dir, nil
		}
	}
	return "", os.ErrNotExist
}

func mkMarketplace(t *testing.T, root, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".claude-plugin/marketplace.json"), []byte(body), 0o644))
}

func mkSkill(t *testing.T, base, plugin, skill string) string {
	t.Helper()
	dir := filepath.Join(base, plugin, "skills", skill)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+skill), 0o644))
	return dir
}

// TestWalk_LocalOnlyWalk seeds a root with one local plugin (skills/writer)
// and expects exactly one Category carrying that one skill.
func TestWalk_LocalOnlyWalk(t *testing.T) {
	root := t.TempDir()
	mkMarketplace(t, root, `{
		"metadata": {"pluginRoot": "./p"},
		"plugins": [{"name": "docs", "source": "./d"}]
	}`)
	skillPath := mkSkill(t, filepath.Join(root, "p"), "d", "writer")

	cat, err := Walk(
		context.Background(),
		fakeFetcher{},
		source.ParsedSource{Type: source.Local, LocalPath: root},
		3,
	)
	require.NoError(t, err)
	require.Len(t, cat, 1, "exactly one category for the one local plugin")
	c := cat[0]
	assert.Equal(t, "docs", c.PluginName)
	assert.Empty(t, c.OwnerRepo, "local category has no remote parent")
	assert.True(t, c.FetchOK)
	assert.Empty(t, c.FetchErr)
	require.Len(t, c.Skills, 1)
	assert.Equal(t, "writer", c.Skills[0].Name)
	assert.Equal(t, skillPath, c.Skills[0].Path)
}

// TestWalk_RemoteUnreachable seeds a root with one remote plugin whose repo
// the fakeFetcher cannot materialize. The walker must surface a Category with
// FetchOK=false and a non-empty FetchErr ("unable to fetch") but continue
// without erroring out.
func TestWalk_RemoteUnreachable(t *testing.T) {
	root := t.TempDir()
	mkMarketplace(t, root, `{
		"plugins": [
			{"name": "remote", "source": {"source": "github", "repo": "acme/missing"}}
		]
	}`)

	cat, err := Walk(
		context.Background(),
		fakeFetcher{repos: map[string]string{}},
		source.ParsedSource{Type: source.Local, LocalPath: root},
		3,
	)
	require.NoError(t, err)
	require.Len(t, cat, 1)
	c := cat[0]
	assert.Equal(t, "remote", c.PluginName)
	assert.Equal(t, "acme/missing", c.OwnerRepo)
	assert.False(t, c.FetchOK)
	assert.NotEmpty(t, c.FetchErr)
}

// TestWalk_DepthLimitStops verifies that maxDepth=1 stops the walker from
// recursing past the first remote hop:
//   - root marketplace declares remote "lvl1" (fetched).
//   - inner dir (the fetched lvl1 dir) declares remote "deep"; depth+1 would
//     be 2 which exceeds maxDepth=1, so the walker does NOT enqueue a fetch
//     and does NOT add a placeholder category for "deep".
//
// "lvl1" itself must surface as a Category with FetchOK=true so the TUI can
// display it (even though no local plugins live inside its fetched dir).
func TestWalk_DepthLimitStops(t *testing.T) {
	inner := t.TempDir()
	mkMarketplace(t, inner, `{
		"plugins": [
			{"name": "deep", "source": {"source": "github", "repo": "acme/deeper"}}
		]
	}`)

	root := t.TempDir()
	mkMarketplace(t, root, `{
		"plugins": [
			{"name": "lvl1", "source": {"source": "github", "repo": "acme/inner"}}
		]
	}`)

	ff := fakeFetcher{repos: map[string]string{"acme/inner": inner}}
	cat, err := Walk(
		context.Background(),
		ff,
		source.ParsedSource{Type: source.Local, LocalPath: root},
		1,
	)
	require.NoError(t, err)

	names := map[string]bool{}
	for _, c := range cat {
		names[c.PluginName] = true
	}
	assert.True(t, names["lvl1"], "lvl1 must surface as a successfully-fetched category")
	assert.False(t, names["deep"], "deep must NOT appear — depth+1 > maxDepth, no placeholder per spec")
}