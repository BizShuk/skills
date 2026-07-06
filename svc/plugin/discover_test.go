package plugin_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bizshuk/skills/svc/plugin"
	"github.com/bizshuk/skills/utils"
)

// fakeFetcher maps an ownerRepo substring to a prepared local dir. Unknown
// repos fall through to os.ErrNotExist so the caller observes a fetch failure.
type fakeFetcher struct{ repos map[string]string }

func (f fakeFetcher) Materialize(_ context.Context, s plugin.ParsedSource) (string, error) {
	if s.Type == plugin.Local {
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

func mkSkill(t *testing.T, base, pluginName, skill string) string {
	t.Helper()
	dir := filepath.Join(base, pluginName, "skills", skill)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+skill), 0o644))
	return dir
}

// findByName returns the first Category in the tree whose PluginName
// matches. Useful for tests that don't care about the tree shape — they
// just want the category by name.
func findByName(c *plugin.Catalog, name string) *plugin.Category {
	if c == nil {
		return nil
	}
	var found *plugin.Category
	var walk func(n *plugin.Category) bool
	walk = func(n *plugin.Category) bool {
		if n == nil {
			return false
		}
		if n.PluginName == name {
			found = n
			return true
		}
		for _, ch := range n.Children {
			if walk(ch) {
				return true
			}
		}
		return false
	}
	for _, r := range c.Roots {
		if walk(r) {
			return found
		}
	}
	return nil
}

// TestWalk_LocalOnlyWalk seeds a root with one local plugin (skills/writer)
// and expects exactly one root Category carrying that one skill.
func TestWalk_LocalOnlyWalk(t *testing.T) {
	root := t.TempDir()
	mkMarketplace(t, root, `{
		"metadata": {"pluginRoot": "./p"},
		"plugins": [{"name": "docs", "source": "./d"}]
	}`)
	skillPath := mkSkill(t, filepath.Join(root, "p"), "d", "writer")

	cat, err := utils.Walk(
		context.Background(),
		fakeFetcher{},
		plugin.ParsedSource{Type: plugin.Local, LocalPath: root},
		3,
	)
	require.NoError(t, err)
	require.Len(t, cat.Roots, 1, "exactly one root category for the one local plugin")
	c := cat.Roots[0]
	assert.Equal(t, "docs", c.PluginName)
	assert.Empty(t, c.OwnerRepo, "local category has no remote parent")
	assert.True(t, c.FetchOK)
	assert.Empty(t, c.FetchErr)
	assert.Empty(t, c.Children, "local-only walk should produce no remote children")
	require.Len(t, c.Skills, 1)
	assert.Equal(t, "writer", c.Skills[0].Name)
	assert.Equal(t, skillPath, c.Skills[0].Path)
}

// TestWalk_RemoteUnreachable seeds a root with one remote plugin whose
// repo the fakeFetcher cannot materialize. The walker must surface a
// failed Category (FetchOK=false, FetchErr non-empty) but continue without
// erroring out. The failed category lives at the root level since its
// parent is the walk's root.
func TestWalk_RemoteUnreachable(t *testing.T) {
	root := t.TempDir()
	mkMarketplace(t, root, `{
		"plugins": [
			{"name": "remote", "source": {"source": "github", "repo": "acme/missing"}}
		]
	}`)

	cat, err := utils.Walk(
		context.Background(),
		fakeFetcher{repos: map[string]string{}},
		plugin.ParsedSource{Type: plugin.Local, LocalPath: root},
		3,
	)
	require.NoError(t, err)
	c := findByName(cat, "remote")
	require.NotNil(t, c, "remote must appear as a category even on fetch failure")
	assert.Equal(t, "acme/missing", c.OwnerRepo)
	assert.False(t, c.FetchOK)
	assert.NotEmpty(t, c.FetchErr)
	assert.Empty(t, c.Children, "fetch-failed plugin has no children")
}

// TestWalk_DepthLimitStops verifies that maxDepth=1 stops the walker from
// recursing past the first remote hop:
//   - root marketplace declares remote "lvl1" (fetched).
//   - inner dir (the fetched lvl1 dir) declares remote "deep"; depth+1
//     would be 2 which exceeds maxDepth=1, so the walker does NOT enqueue
//     a fetch and does NOT add a placeholder category for "deep".
//
// "lvl1" itself must surface as a Category with FetchOK=true so the TUI
// can display it (even though no local plugins live inside its fetched
// dir). Because "deep" was depth-gated, "lvl1" has no children in the
// resulting tree.
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
	cat, err := utils.Walk(
		context.Background(),
		ff,
		plugin.ParsedSource{Type: plugin.Local, LocalPath: root},
		1,
	)
	require.NoError(t, err)

	lvl1 := findByName(cat, "lvl1")
	require.NotNil(t, lvl1, "lvl1 must surface as a successfully-fetched category")
	assert.True(t, lvl1.FetchOK)
	assert.Empty(t, lvl1.Children, "lvl1 has no children — its 'deep' remote was depth-gated")

	assert.Nil(t, findByName(cat, "deep"), "deep must NOT appear — depth+1 > maxDepth, no placeholder per spec")
}

// TestWalk_RemoteRootPluginAbsorbedNotNested reproduces the bizshuk/gosdk
// case end-to-end: the root marketplace declares a remote "gosdk" plugin whose
// fetched repo is itself a single root plugin (marketplace self-source "./"
// PLUS a plugin.json, both named "gosdk"). The walker must NOT render a
// redundant "gosdk → gosdk (×2)" tree; instead the placeholder absorbs the
// repo's own skills and gains no children.
func TestWalk_RemoteRootPluginAbsorbedNotNested(t *testing.T) {
	// The fetched repo: a root plugin declared twice (marketplace self + plugin.json).
	repo := t.TempDir()
	cpDir := filepath.Join(repo, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "marketplace.json"), []byte(`{
		"plugins": [{ "name": "gosdk", "source": "./" }]
	}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "plugin.json"), []byte(`{
		"name": "gosdk"
	}`), 0o644))
	skillDir := filepath.Join(repo, "skills", "golang-dev")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# dev"), 0o644))

	root := t.TempDir()
	mkMarketplace(t, root, `{
		"plugins": [
			{"name": "gosdk", "source": {"source": "github", "repo": "bizshuk/gosdk"}}
		]
	}`)

	ff := fakeFetcher{repos: map[string]string{"bizshuk/gosdk": repo}}
	cat, err := utils.Walk(
		context.Background(),
		ff,
		plugin.ParsedSource{Type: plugin.Local, LocalPath: root},
		3,
	)
	require.NoError(t, err)

	require.Len(t, cat.Roots, 1, "exactly one root category — the remote placeholder")
	c := cat.Roots[0]
	assert.Equal(t, "gosdk", c.PluginName)
	assert.Equal(t, "bizshuk/gosdk", c.OwnerRepo)
	assert.True(t, c.FetchOK)
	assert.Empty(t, c.Children, "the repo's own root plugin is absorbed, not nested as a same-name child")
	require.Len(t, c.Skills, 1, "skills are absorbed once — not duplicated, not one layer deep")
	assert.Equal(t, "golang-dev", c.Skills[0].Name)
}

// TestWalk_NestedRemotePluginAppearsAsChild verifies the tree shape: a
// remote plugin fetched from the root is NOT a sibling at the root, but a
// child of the root. And if that fetched remote itself declares a local
// plugin, that local plugin lives inside the remote's subtree — not back
// at the root level.
//
// Layout:
//   - root marketplace.json declares remote "lvl1".
//   - lvl1's fetched dir contains marketplace.json declaring local
//     "inner-doc" with one skill.
//
// Expected tree:
//   - cat.Roots: [lvl1]
//   - lvl1.Children: [inner-doc]
//   - inner-doc.Skills: [writer]
func TestWalk_NestedRemotePluginAppearsAsChild(t *testing.T) {
	// The directory the fakeFetcher serves for "acme/inner". It contains a
	// marketplace.json that declares a *local* plugin "inner-doc".
	inner := t.TempDir()
	mkSkill(t, inner, "inner-doc", "writer")
	mkMarketplace(t, inner, `{
		"metadata": {"pluginRoot": "./"},
		"plugins": [{"name": "inner-doc", "source": "./inner-doc"}]
	}`)

	root := t.TempDir()
	mkMarketplace(t, root, `{
		"plugins": [
			{"name": "lvl1", "source": {"source": "github", "repo": "acme/inner"}}
		]
	}`)

	ff := fakeFetcher{repos: map[string]string{"acme/inner": inner}}
	cat, err := utils.Walk(
		context.Background(),
		ff,
		plugin.ParsedSource{Type: plugin.Local, LocalPath: root},
		3,
	)
	require.NoError(t, err)

	require.Len(t, cat.Roots, 1, "only 'lvl1' is at the root — 'inner-doc' is its child")
	lvl1 := cat.Roots[0]
	assert.Equal(t, "lvl1", lvl1.PluginName)
	assert.Equal(t, "acme/inner", lvl1.OwnerRepo)
	assert.True(t, lvl1.FetchOK)

	require.Len(t, lvl1.Children, 1, "lvl1 has exactly one child: inner-doc")
	innerDoc := lvl1.Children[0]
	assert.Equal(t, "inner-doc", innerDoc.PluginName)
	assert.Empty(t, innerDoc.OwnerRepo, "inner-doc is a local plugin inside lvl1, no remote parent")
	assert.True(t, innerDoc.FetchOK)
	require.Len(t, innerDoc.Skills, 1)
	assert.Equal(t, "writer", innerDoc.Skills[0].Name)
}

// TestWalk_RedundantSubPluginAbsorbed verifies that sub-plugins with the same
// name as the parent remote category are absorbed, avoiding redundant layers.
func TestWalk_RedundantSubPluginAbsorbed(t *testing.T) {
	inner := t.TempDir()
	subDir := filepath.Join(inner, "understand-anything-plugin")
	mkSkill(t, subDir, "", "explainer")
	mkMarketplace(t, inner, `{
		"metadata": {"pluginRoot": "./"},
		"plugins": [{"name": "understand-anything", "source": "./understand-anything-plugin"}]
	}`)

	root := t.TempDir()
	mkMarketplace(t, root, `{
		"plugins": [
			{"name": "understand-anything", "source": {"source": "github", "repo": "egonex-ai/understand-anything"}}
		]
	}`)

	ff := fakeFetcher{repos: map[string]string{"egonex-ai/understand-anything": inner}}
	cat, err := utils.Walk(
		context.Background(),
		ff,
		plugin.ParsedSource{Type: plugin.Local, LocalPath: root},
		3,
	)
	require.NoError(t, err)

	require.Len(t, cat.Roots, 1)
	ua := cat.Roots[0]
	assert.Equal(t, "understand-anything", ua.PluginName)
	assert.Len(t, ua.Children, 0, "the sub-plugin should be absorbed, leaving no redundant child layers")
	require.Len(t, ua.Skills, 1)
	assert.Equal(t, "explainer", ua.Skills[0].Name)
}

// TestWalk_DedupesSkillsByName verifies that when the same skill appears at
// two different physical paths (e.g. once in the marketplace root and once
// in a sub-plugin dir that the recursive BFS re-scans), the final Catalog
// only shows one row. This regression test pins the dedup behavior that
// prevents the "apple-calendar x2" UI bug.
func TestWalk_DedupesSkillsByName(t *testing.T) {
	base := t.TempDir()

	// Root-level skills/apple-calendar (will be found by Scan of the root)
	rootSkillDir := filepath.Join(base, "skills", "apple-calendar")
	require.NoError(t, os.MkdirAll(rootSkillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rootSkillDir, "SKILL.md"),
		[]byte("# Apple Calendar\nFrom root."), 0o644))

	// Sub-plugin with the same skill name (will be found by recursive BFS)
	cpDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))
	marketplace := `{
		"name": "dup-mp",
		"plugins": [
			{"name": "tools", "source": "./plugins/tools"}
		]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "marketplace.json"), []byte(marketplace), 0o644))

	pluginSkillDir := filepath.Join(base, "plugins", "tools", "skills", "apple-calendar")
	require.NoError(t, os.MkdirAll(pluginSkillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginSkillDir, "SKILL.md"),
		[]byte("# Apple Calendar\nFrom sub-plugin."), 0o644))

	src, err := plugin.Parse(base)
	require.NoError(t, err)
	cat, err := utils.Walk(context.Background(), plugin.New(), src, 3)
	require.NoError(t, err)
	require.NotEmpty(t, cat.Roots)

	// Walk the whole tree and count "apple-calendar" occurrences.
	count := 0
	var walk func(n *plugin.Category)
	walk = func(n *plugin.Category) {
		for _, s := range n.Skills {
			if s.Name == "apple-calendar" {
				count++
			}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	for _, r := range cat.Roots {
		walk(r)
	}
	assert.Equal(t, 1, count,
		"apple-calendar must appear exactly once even though it exists at two physical paths")
}
