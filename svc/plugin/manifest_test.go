// manifest_test.go covers manifest interpretation: what Scan makes of a
// marketplace.json / plugin.json / skill.json, and the manifest-presence
// predicates that decide whether the no-manifest fallback kicks in.
// On-disk skill and subagent scanning is exercised in scan_test.go.
package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScan_MarketplaceMixedLocalRemote verifies a marketplace.json with both
// a local string-source plugin (recursive skills/ scan) and an object-source
// plugin (github) is split into Locals and Remotes correctly.
func TestScan_MarketplaceMixedLocalRemote(t *testing.T) {
	base := t.TempDir()
	cpDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))

	marketplace := `{
		"metadata": { "pluginRoot": "./" },
		"plugins": [
			{
				"name": "local-p",
				"source": "./plugins/local-p",
				"skills": []
			},
			{
				"name": "remote-p",
				"source": { "source": "github", "repo": "anthropic/skills", "ref": "main" }
			}
		]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "marketplace.json"), []byte(marketplace), 0o644))

	// Conventional skill under the local plugin.
	pluginDir := filepath.Join(base, "plugins", "local-p")
	skillDir := filepath.Join(pluginDir, "skills", "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# skill"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)

	require.Len(t, parsed.Locals, 1, "exactly one local plugin")
	lp := parsed.Locals[0]
	assert.Equal(t, "local-p", lp.Name)
	require.Len(t, lp.Skills, 1, "exactly one conventional skill")
	assert.Equal(t, "my-skill", lp.Skills[0].Name)
	assert.Equal(t, skillDir, lp.Skills[0].Path)

	require.Len(t, parsed.Remotes, 1, "exactly one remote plugin")
	rp := parsed.Remotes[0]
	assert.Equal(t, "remote-p", rp.Name)
	assert.Equal(t, "anthropic/skills", rp.OwnerRepo)
	assert.Equal(t, "https://github.com/anthropic/skills.git", rp.URL)
	assert.Equal(t, "main", rp.Ref)
}

// TestScan_PluginJsonOnly verifies a plugin.json at root picks up both a
// conventional skill (skills/<name>/SKILL.md) AND an additive skill path
// from the manifest's `skills` array. Both must end up in the resulting
// Skills slice with no duplicates.
func TestScan_PluginJsonOnly(t *testing.T) {
	base := t.TempDir()
	cpDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))

	plugin := `{
		"name": "my-plugin",
		"skills": ["./extra/SKILL.md"]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "plugin.json"), []byte(plugin), 0o644))

	// Conventional skill.
	conventional := filepath.Join(base, "skills", "conventional")
	require.NoError(t, os.MkdirAll(conventional, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(conventional, "SKILL.md"), []byte("# c"), 0o644))

	// Additive skill.
	extra := filepath.Join(base, "extra")
	require.NoError(t, os.MkdirAll(extra, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(extra, "SKILL.md"), []byte("# e"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)

	require.Len(t, parsed.Locals, 1)
	lp := parsed.Locals[0]
	assert.Equal(t, "my-plugin", lp.Name)
	require.Len(t, lp.Skills, 2, "union of conventional and additive")

	names := make(map[string]bool, len(lp.Skills))
	for _, s := range lp.Skills {
		names[s.Name] = true
	}
	assert.True(t, names["conventional"], "conventional skill present")
	assert.True(t, names["extra"], "additive skill present")
}

// TestScan_MarketplaceNestedPluginManifestSkillsDirectory reproduces the
// blender-toolkit layout: the marketplace entry points at a local plugin,
// whose own plugin.json declares a directory that contains SKILL.md directly.
func TestScan_MarketplaceNestedPluginManifestSkillsDirectory(t *testing.T) {
	base := t.TempDir()
	marketplaceDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(marketplaceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(marketplaceDir, "marketplace.json"), []byte(`{
		"plugins": [{
			"name": "blender-toolkit",
			"source": "./plugins/blender-toolkit"
		}]
	}`), 0o644))

	pluginDir := filepath.Join(base, "plugins", "blender-toolkit")
	pluginManifestDir := filepath.Join(pluginDir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginManifestDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginManifestDir, "plugin.json"), []byte(`{
		"name": "blender-toolkit",
		"skills": ["./skills"]
	}`), 0o644))

	skillDir := filepath.Join(pluginDir, "skills")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: blender-toolkit
description: Blender automation toolkit.
---
`), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	assert.Equal(t, "blender-toolkit", parsed.Locals[0].Name)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, skillDir, parsed.Locals[0].Skills[0].Path)
}

// TestScan_PluginManifestSkillCollectionDirectory verifies that a custom
// manifest directory containing direct <name>/SKILL.md children is scanned.
func TestScan_PluginManifestSkillCollectionDirectory(t *testing.T) {
	base := t.TempDir()
	pluginManifestDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(pluginManifestDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginManifestDir, "plugin.json"), []byte(`{
		"name": "custom-toolkit",
		"skills": ["./custom-skills"]
	}`), 0o644))

	skillDir := filepath.Join(base, "custom-skills", "alpha")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Alpha\n"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "alpha", parsed.Locals[0].Skills[0].Name)
	assert.Equal(t, skillDir, parsed.Locals[0].Skills[0].Path)
}

// TestScan_SelfMarketplaceAndPluginJsonDedup reproduces the real-world
// bizshuk/gosdk layout: a repo that ships BOTH a marketplace.json whose only
// plugin points at itself (source "./") AND a plugin.json at root, both naming
// the same plugin. Scanning both used to surface the plugin twice; Scan must
// now collapse the same-base duplicate into exactly one model.LocalPlugin.
func TestScan_SelfMarketplaceAndPluginJsonDedup(t *testing.T) {
	base := t.TempDir()
	cpDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "marketplace.json"), []byte(`{
		"plugins": [{ "name": "gosdk", "source": "./" }]
	}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "plugin.json"), []byte(`{
		"name": "gosdk"
	}`), 0o644))

	skillDir := filepath.Join(base, "skills", "golang-dev")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# dev"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)

	require.Len(t, parsed.Locals, 1, "same-base marketplace-self + plugin.json must dedupe to one plugin")
	lp := parsed.Locals[0]
	assert.Equal(t, "gosdk", lp.Name)
	require.Len(t, lp.Skills, 1, "the one skill appears once, not twice")
	assert.Equal(t, "golang-dev", lp.Skills[0].Name)
}

// TestScan_SkillJsonOnly verifies that skill.json at root and the conventional
// skills/ directory are properly discovered.
func TestScan_SkillJsonOnly(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "skill.json"), []byte(`{"name":"ui-ux"}`), 0o644))

	// Put a skill in skills/design/SKILL.md
	skillDir := filepath.Join(base, "skills", "design")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Design\nDesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	assert.Equal(t, "ui-ux", parsed.Locals[0].Name)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "design", parsed.Locals[0].Skills[0].Name)
}

// TestScan_NestedMarketplaceSubPlugins_OptInTopLevel verifies that a nested
// marketplace whose sub-plugin uses the "flat .md" layout (top-level .md
// files in the sub-plugin base) only picks them up as subagents when the
// marketplace entry explicitly sets "topLevelAgents": true.
func TestScan_NestedMarketplaceSubPlugins_OptInTopLevel(t *testing.T) {
	base := t.TempDir()
	cpDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))

	marketplace := `{
		"name": "test-mp",
		"plugins": [
			{
				"name": "voltagent-core",
				"source": "./categories/01-core",
				"topLevelAgents": true
			}
		]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "marketplace.json"), []byte(marketplace), 0o644))

	coreDir := filepath.Join(base, "categories", "01-core")
	require.NoError(t, os.MkdirAll(coreDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(coreDir, "api-designer.md"),
		[]byte("# API Designer\nDesigns APIs."), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(coreDir, "backend-dev.md"),
		[]byte("# Backend Dev\nBackend work."), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(coreDir, "README.md"),
		[]byte("# Core\nDocs."), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	lp := parsed.Locals[0]
	assert.Equal(t, "voltagent-core", lp.Name)
	assert.Equal(t, filepath.Join(base, "categories", "01-core"), lp.Base)
	require.Len(t, lp.Subagents, 2, "with topLevelAgents=true, .md files at base should be picked up; README.md skipped")
	names := []string{lp.Subagents[0].Name, lp.Subagents[1].Name}
	assert.Contains(t, names, "api-designer")
	assert.Contains(t, names, "backend-dev")
	assert.NotContains(t, names, "README")
}

// TestHasAnyManifest verifies the helper that gates the no-manifest
// fallback in Scan(): true if any of marketplace.json / plugin.json /
// skill.json is reachable; false when all are missing.
func TestHasAnyManifest(t *testing.T) {
	t.Run("none present", func(t *testing.T) {
		base := t.TempDir()
		assert.False(t, hasAnyManifest(base))
	})

	t.Run("marketplace only", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude-plugin"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(base, ".claude-plugin", "marketplace.json"), []byte("{}"), 0o644))
		assert.True(t, hasAnyManifest(base))
	})

	t.Run("plugin only", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude-plugin"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(base, ".claude-plugin", "plugin.json"), []byte("{}"), 0o644))
		assert.True(t, hasAnyManifest(base))
	})

	t.Run("skill only", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(base, "skill.json"), []byte("{}"), 0o644))
		assert.True(t, hasAnyManifest(base))
	})

	t.Run("malformed json still counts as present", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude-plugin"), 0o755))
		// Intentionally invalid JSON — should still be treated as "manifest exists"
		// so the fallback does not silently mask a real parse bug.
		require.NoError(t, os.WriteFile(filepath.Join(base, ".claude-plugin", "plugin.json"), []byte("not json"), 0o644))
		assert.True(t, hasAnyManifest(base))
	})
}

// TestHasAnyConventionalSkillsDir verifies that the helper recognizes only the
// repo's own skills/ directory — agent install destinations do not count — and
// ignores a file with the same name.
func TestHasAnyConventionalSkillsDir(t *testing.T) {
	t.Run("none present", func(t *testing.T) {
		base := t.TempDir()
		assert.False(t, hasAnyConventionalSkillsDir(base))
	})

	t.Run("skills only", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, "skills"), 0o755))
		assert.True(t, hasAnyConventionalSkillsDir(base))
	})

	t.Run("dotclaude skills does not count", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude", "skills"), 0o755))
		assert.False(t, hasAnyConventionalSkillsDir(base))
	})

	t.Run("dotagents skills does not count", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, ".agents", "skills"), 0o755))
		assert.False(t, hasAnyConventionalSkillsDir(base))
	})

	t.Run("file with the name skills is not a directory", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(base, "skills"), []byte("x"), 0o644))
		assert.False(t, hasAnyConventionalSkillsDir(base))
	})
}

// TestIsInsideAgentDir verifies the helper that protects the fallback from
// mistaking a conventional agents/ layout (subagent definitions) for a
// plugin. A repo whose ROOT is literally named "agents" is still a valid
// fallback target — only nested agents/ directories count.
func TestIsInsideAgentDir(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"plain root", "/repo/project", false},
		{"repo root literally named agents", "/agents", false},
		{"agents as last segment with trailing slash", "/agents/", false},
		{"nested agents subdir", "/repo/project/agents", true},
		{"nested agents subdir with file", "/repo/project/agents/foo.md", true},
		{"dotclaude agents subdir", "/repo/project/.claude/agents", true},
		{"dotclaude agents subdir with file", "/repo/project/.claude/agents/foo.md", true},
		{"dotagents agents subdir", "/repo/project/.agents/agents", true},
		{"partial name agents-keeper", "/repo/agents-keeper", false},
		{"partial name myagents", "/repo/myagents", false},
		{"agents hidden inside unrelated name", "/repo/data/agents-export/x", false},
		{"relative plain", "plugins/foo", false},
		{"relative inside agents", "plugins/agents/foo", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isInsideAgentDir(tc.path))
		})
	}
}

// TestScan_NoManifest_Fallback_SkillsDir verifies the headline fix: a repo
// with NO plugin.json / marketplace.json / skill.json and only a top-level
// skills/<name>/SKILL.md layout still surfaces its skills.
func TestScan_NoManifest_Fallback_SkillsDir(t *testing.T) {
	base := t.TempDir()
	skillDir := filepath.Join(base, "skills", "my-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# my-skill\nDoes things."), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1, "no-manifest repo should still get a synthetic plugin")
	assert.Equal(t, filepath.Base(base), parsed.Locals[0].Name)
	assert.Equal(t, base, parsed.Locals[0].Base)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "my-skill", parsed.Locals[0].Skills[0].Name)
}

// E2: <base>/.claude/skills/ is an agent INSTALL destination, not a source —
// a repo whose only skills live there surfaces nothing.
func TestScan_NoManifest_DotClaudeSkillsDirIgnored(t *testing.T) {
	base := t.TempDir()
	skillDir := filepath.Join(base, ".claude", "skills", "design")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# design\ndesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	assert.Empty(t, parsed.Locals)
}

// E3: same for <base>/.agents/skills/.
func TestScan_NoManifest_DotAgentsSkillsDirIgnored(t *testing.T) {
	base := t.TempDir()
	skillDir := filepath.Join(base, ".agents", "skills", "agent-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# agent-skill\ndesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	assert.Empty(t, parsed.Locals)
}

// E4: skills/ sitting INSIDE an agents/ subdir must NOT be picked up
// as a fallback plugin. The conventional agents/ layout is for subagents.
func TestScan_NoManifest_SkillsInsideAgentDir_Ignored(t *testing.T) {
	base := filepath.Join(t.TempDir(), "agents")
	skillDir := filepath.Join(base, "skills", "not-a-plugin")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# not-a-plugin\ndesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	assert.Empty(t, parsed.Locals, "nested agents/skills/ must not trigger the fallback")
}

// E5: empty skills/ dir → still synthesize a plugin, but with no skills.
// We do not suppress the plugin on empty skills; the user may add one
// later and we want it discoverable.
func TestScan_NoManifest_EmptySkillsDir_PluginStillCreated(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "skills"), 0o755))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1, "empty skills/ should still produce a plugin")
	assert.Empty(t, parsed.Locals[0].Skills)
}

// E6: no manifest, no conventional skills dir → no plugin.
func TestScan_NoManifest_NoSkillsDir_NoFallback(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "README.md"),
		[]byte("# just a readme"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	assert.Empty(t, parsed.Locals)
}

// E7: an existing plugin.json must short-circuit the fallback. We do not
// get a synthetic duplicate alongside the manifest-declared plugin.
func TestScan_PluginJsonExists_NoFallback(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"declared"}`), 0o644))

	// Also create a conventional skills/ dir — must NOT trigger fallback.
	skillDir := filepath.Join(base, "skills", "extra")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# extra\ndesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1, "plugin.json must suppress the fallback")
	assert.Equal(t, "declared", parsed.Locals[0].Name)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "extra", parsed.Locals[0].Skills[0].Name,
		"the skills/ dir still feeds the declared plugin via A4")
}

// E8: a malformed plugin.json is still a "manifest present" signal — the
// fallback must not mask a real parse bug by emitting an empty synthetic
// plugin.
func TestScan_MalformedPluginJson_StillHasManifest_NoFallback(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, ".claude-plugin", "plugin.json"),
		[]byte("not json"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "skills"), 0o755))

	parsed, err := Scan(base)
	require.NoError(t, err)
	assert.Empty(t, parsed.Locals,
		"malformed plugin.json counts as 'manifest present' — fallback must NOT run")
}

// E9: an existing skill.json must short-circuit the fallback too.
func TestScan_SkillJsonExists_NoFallback(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "skill.json"),
		[]byte(`{"name":"ui-ux"}`), 0o644))
	skillDir := filepath.Join(base, "skills", "design")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# design\ndesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	assert.Equal(t, "ui-ux", parsed.Locals[0].Name)
}

// E10: BFS mid-level safety. A sub-plugin dir reached via a parent's
// marketplace.json declaration has a skills/ child but no own manifest.
// Scan() at the sub level has Locals==[], so the fallback fires for the
// sub dir — which is the intended behavior for a remote-fetched sub-plugin
// without its own manifest. (BFS-level dedup happens in Walk(), not Scan().)
func TestScan_NoManifest_SkillsAtBFSMidLevel_NoFallback(t *testing.T) {
	subDir := t.TempDir()
	skillDir := filepath.Join(subDir, "skills", "leaf")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# leaf\ndesc"), 0o644))

	parsed, err := Scan(subDir)
	require.NoError(t, err)
	// sub has no manifest of its own AND has skills/ → fallback SHOULD
	// fire for Scan(sub) at this level, producing exactly one plugin.
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "leaf", parsed.Locals[0].Skills[0].Name)
}
