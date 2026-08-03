// scan_test.go covers the disk-facing half of the package: which directories
// become skills, which .md files become subagents, and what each entry's
// description resolves to. Manifest parsing itself lives in manifest_test.go.
package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bizshuk/skills/model"
)

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

	var parsed model.Parsed
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

// TestScan_SkipsReadmeMDInAgentsDir verifies that README.md inside an agents/
// directory is NOT treated as a subagent. README.md is a directory-level doc,
// not a subagent definition.
func TestScan_SkipsReadmeMDInAgentsDir(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "skill.json"), []byte(`{"name":"demo"}`), 0o644))

	agentsDir := filepath.Join(base, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	// Real subagent .md file — should be picked up
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "code-reviewer.md"),
		[]byte("# Code Reviewer\nReviews PRs."), 0o644))

	// README.md — must be skipped
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "README.md"),
		[]byte("# Subagents\nDocumentation."), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	subs := parsed.Locals[0].Subagents
	require.Len(t, subs, 1, "README.md must not be included as a subagent")
	assert.Equal(t, "code-reviewer", subs[0].Name,
		"only the real subagent .md file should be picked up")
}

// TestScan_TopLevelAgentsDefaultOff verifies that WITHOUT the opt-in flag,
// top-level .md files in the plugin base are NOT picked up as subagents.
// This is the safe default — unrelated .md docs (README, CHANGELOG, etc.)
// in the plugin root must not be auto-included.
func TestScan_TopLevelAgentsDefaultOff(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "skill.json"), []byte(`{"name":"p"}`), 0o644))

	// Top-level .md files in lp.Base (which equals `base` here).
	require.NoError(t, os.WriteFile(filepath.Join(base, "stray.md"),
		[]byte("# Stray\nUnrelated doc."), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(base, "README.md"),
		[]byte("# Plugin\nReadme."), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	assert.Empty(t, parsed.Locals[0].Subagents,
		"default (no topLevelAgents flag) must NOT scan top-level .md files")
}

// TestScan_NestedMarketplaceSubPluginAgentsDir verifies that a sub-plugin
// using the conventional agents/ subdir layout (instead of top-level .md)
// is also picked up correctly. Mirrors the "review" plugin layout in the
// cc-plugin marketplace.
func TestScan_NestedMarketplaceSubPluginAgentsDir(t *testing.T) {
	base := t.TempDir()
	cpDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))

	marketplace := `{
		"name": "test-mp",
		"plugins": [
			{"name": "review", "source": "./plugins/review"}
		]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "marketplace.json"), []byte(marketplace), 0o644))

	agentsDir := filepath.Join(base, "plugins", "review", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "coordinator.md"),
		[]byte("# Coordinator\nCoordinates reviews."), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	lp := parsed.Locals[0]
	assert.Equal(t, "review", lp.Name)
	require.Len(t, lp.Subagents, 1)
	assert.Equal(t, "coordinator", lp.Subagents[0].Name)
}

// TestScan_AgentsFieldInPluginManifest verifies that a plugin.json's "agents"
// array (relative paths to .md files in the plugin base) is loaded as
// subagents. Mirrors the canonical pattern used by both cc-plugin's "review"
// sub-plugin entry and the voltagent-* category plugin.json files in
// VoltAgent/awesome-claude-code-subagents.
func TestScan_AgentsFieldInPluginManifest(t *testing.T) {
	base := t.TempDir()

	// plugin.json declares the subagents explicitly
	cpDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))
	pluginJSON := `{
		"name": "review",
		"agents": [
			"./agents/coordinator.md",
			"./agents/linter.md"
		]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "plugin.json"), []byte(pluginJSON), 0o644))

	// Create the .md files referenced by the agents array
	agentsDir := filepath.Join(base, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "coordinator.md"),
		[]byte("# Coordinator\nCoordinates."), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "linter.md"),
		[]byte("# Linter\nLints code."), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	lp := parsed.Locals[0]
	assert.Equal(t, "review", lp.Name)
	require.Len(t, lp.Subagents, 2, "agents array should produce 2 subagent entries")
	names := []string{lp.Subagents[0].Name, lp.Subagents[1].Name}
	assert.Contains(t, names, "coordinator")
	assert.Contains(t, names, "linter")
}

// TestScan_AgentsFieldRejectsMissingFile verifies that an entry in the
// "agents" array that doesn't exist on disk is silently skipped (not
// created as a subagent).
func TestScan_AgentsFieldRejectsMissingFile(t *testing.T) {
	base := t.TempDir()
	cpDir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cpDir, 0o755))
	pluginJSON := `{
		"name": "broken",
		"agents": [
			"./agents/real.md",
			"./agents/missing.md"
		]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "plugin.json"), []byte(pluginJSON), 0o644))

	agentsDir := filepath.Join(base, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "real.md"),
		[]byte("# Real\nExists."), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	lp := parsed.Locals[0]
	require.Len(t, lp.Subagents, 1, "missing file should be skipped; only real.md should appear")
	assert.Equal(t, "real", lp.Subagents[0].Name)
}
