package plugin

import (
	"os"
	"path/filepath"
	"strings"
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

// TestScan_SelfMarketplaceAndPluginJsonDedup reproduces the real-world
// bizshuk/gosdk layout: a repo that ships BOTH a marketplace.json whose only
// plugin points at itself (source "./") AND a plugin.json at root, both naming
// the same plugin. Scanning both used to surface the plugin twice; Scan must
// now collapse the same-base duplicate into exactly one LocalPlugin.
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

// TestScan_AdditiveTraversalRejected verifies that an additive skill path
// whose parent directory escapes `base` is silently dropped — the plugin
// still surfaces (with its valid plugins intact) but the bad skill does not.
// Must not panic.
func TestScan_AdditiveTraversalRejected(t *testing.T) {
	base := t.TempDir()
	cpDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))

	plugin := `{
		"name": "bad-plugin",
		"skills": ["./../escape/SKILL.md", "./ok/SKILL.md"]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "plugin.json"), []byte(plugin), 0o644))

	// The escaped path's file should NOT contribute. The in-bounds additive
	// skill dir does not need SKILL.md to exist — but the test focuses on
	// the rejection, so the plugin should still appear with empty Skills.
	// (Existence of SKILL.md is checked only when the path is in-bounds.)
	ok := filepath.Join(base, "ok")
	require.NoError(t, os.MkdirAll(ok, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ok, "SKILL.md"), []byte("# ok"), 0o644))

	var parsed Parsed
	require.NotPanics(t, func() {
		var err error
		parsed, err = Scan(base)
		require.NoError(t, err)
	})

	require.Len(t, parsed.Locals, 1)
	lp := parsed.Locals[0]
	assert.Equal(t, "bad-plugin", lp.Name)
	// Only the ./ok additive skill should appear; ../escape was rejected.
	require.Len(t, lp.Skills, 1, "traversal rejected, only in-bounds additive kept")
	assert.Equal(t, "ok", lp.Skills[0].Name)
}

// TestScan_DescriptionReadsFirstBodyLine verifies that the Description
// field populated by Scan is the first non-heading, non-empty line of
// SKILL.md, trimmed. Headings (lines starting with #) are skipped.
func TestScan_DescriptionReadsFirstBodyLine(t *testing.T) {
	base := t.TempDir()
	cpDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "plugin.json"),
		[]byte(`{"name":"p"}`), 0o644))

	skillDir := filepath.Join(base, "skills", "alpha")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	body := "# Heading One\n\n# Heading Two\n\nUse when fooing the bar.\n"
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "Use when fooing the bar.", parsed.Locals[0].Skills[0].Description,
		"description should be the first non-heading, non-empty body line, trimmed")
}

// TestScan_DescriptionReadsLongLinesRaw verifies that descriptions
// longer than 60 runes are read in full without truncation.
func TestScan_DescriptionReadsLongLinesRaw(t *testing.T) {
	base := t.TempDir()
	cpDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "plugin.json"),
		[]byte(`{"name":"p"}`), 0o644))

	skillDir := filepath.Join(base, "skills", "long")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	expected := strings.Repeat("abcdefghij", 10) // 100 ascii chars
	long := "# title\n\n" + expected
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(long), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals[0].Skills, 1)
	got := parsed.Locals[0].Skills[0].Description
	assert.Equal(t, expected, got, "description should be read in full")
}

// TestScan_SkillJsonOnly verifies that skill.json at root and .claude/skills conventional
// directories are properly discovered.
func TestScan_SkillJsonOnly(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "skill.json"), []byte(`{"name":"ui-ux"}`), 0o644))

	// Put a skill in .claude/skills/design/SKILL.md
	skillDir := filepath.Join(base, ".claude", "skills", "design")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Design\nDesc"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	assert.Equal(t, "ui-ux", parsed.Locals[0].Name)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "design", parsed.Locals[0].Skills[0].Name)
}

// TestScan_DescriptionReadsYAMLFrontmatter verifies that YAML frontmatter
// description field is parsed and preferred over the name/other fields.
func TestScan_DescriptionReadsYAMLFrontmatter(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "skill.json"), []byte(`{"name":"ui-ux"}`), 0o644))

	skillDir := filepath.Join(base, ".claude", "skills", "design")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	body := `---\nname: design-skill\ndescription: "This is a beautiful skill description"\n---\n# Title\nSome body text`
	body = strings.ReplaceAll(body, "\\n", "\n")
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "This is a beautiful skill description", parsed.Locals[0].Skills[0].Description)
}

// TestScan_DescriptionReadsYAMLFrontmatterMultiline verifies that multiline YAML
// description fields (e.g. using folded style) are successfully parsed.
func TestScan_DescriptionReadsYAMLFrontmatterMultiline(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "skill.json"), []byte(`{"name":"ui-ux"}`), 0o644))

	skillDir := filepath.Join(base, ".claude", "skills", "design")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	body := `---\nname: apple-reminders\ndescription: >\n    Use when managing Apple reminders\n    on macOS.\nversion: "1.0.0"\n---\n# Title`
	body = strings.ReplaceAll(body, "\\n", "\n")
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "Use when managing Apple reminders on macOS.", parsed.Locals[0].Skills[0].Description)
}



