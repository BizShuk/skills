package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiscoverInstalled_EmptyWhenNoDirs is the cold-start case: no agent's
// project or user install dir exists, so the discovery returns zero items
// without error. We steer cwd + HOME into tempdirs so the real $HOME is
// never read.
func TestDiscoverInstalled_EmptyWhenNoDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	got, err := DiscoverInstalled()
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestDiscoverInstalled_SkillsAndSubagentsFromSameAgent seeds a single
// agent's project install dirs and verifies the discovery groups them into
// the right InstalledKind buckets. Other agents must contribute nothing.
func TestDiscoverInstalled_SkillsAndSubagentsFromSameAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	skillRoot := filepath.Join(cwd, ".claude", "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(skillRoot, "writer"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillRoot, "writer", "SKILL.md"), []byte("# writer"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(skillRoot, "helper"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillRoot, "helper", "SKILL.md"), []byte("# helper"), 0o644))

	agentsRoot := filepath.Join(cwd, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsRoot, "reviewer.md"), []byte("# reviewer"), 0o644))

	got, err := DiscoverInstalled()
	require.NoError(t, err)
	require.Len(t, got, 3)

	byKey := map[string]InstalledItem{}
	for _, it := range got {
		byKey[string(it.Scope)+"|"+string(it.Kind)+"|"+it.Name] = it
	}

	helper := byKey["project|skill|helper"]
	require.Len(t, helper.Locations, 1)
	assert.Equal(t, AgentType("claude-code"), helper.Locations[0].Agent)
	assert.Equal(t, filepath.Join(skillRoot, "helper"), helper.Locations[0].Path)

	reviewer := byKey["project|subagent|reviewer"]
	require.Len(t, reviewer.Locations, 1)
	assert.Equal(t, filepath.Join(agentsRoot, "reviewer.md"), reviewer.Locations[0].Path)
}

// TestDiscoverInstalled_SameSkillAcrossTwoAgents confirms that the same
// skill name in two agents' install dirs merges into one InstalledItem
// whose Locations lists both. This is the central "list & group" property
// the remove TUI depends on.
//
// Note that the project's .agents/skills directory is shared by four agents
// (antigravity, antigravity-cli, codex, opencode) per the embedded provider
// table — see the README's supported-agents table. So writing one
// .agents/skills/writer is seen by all four agents at once.
func TestDiscoverInstalled_SameSkillAcrossTwoAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	claudeSkills := filepath.Join(cwd, ".claude", "skills", "writer")
	require.NoError(t, os.MkdirAll(claudeSkills, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(claudeSkills, "SKILL.md"), []byte("# writer"), 0o644))

	antigravitySkills := filepath.Join(cwd, ".agents", "skills", "writer")
	require.NoError(t, os.MkdirAll(antigravitySkills, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(antigravitySkills, "SKILL.md"), []byte("# writer"), 0o644))

	got, err := DiscoverInstalled()
	require.NoError(t, err)
	require.Len(t, got, 1, "writer should appear as one row in project scope")
	assert.Equal(t, "writer", got[0].Name)
	assert.Equal(t, InstalledSkill, got[0].Kind)
	assert.Equal(t, ScopeProject, got[0].Scope)

	agents := map[AgentType]bool{}
	for _, loc := range got[0].Locations {
		agents[loc.Agent] = true
	}
	assert.True(t, agents["claude-code"], "expected claude-code in locations")
	assert.True(t, agents["antigravity"], "expected antigravity in locations")
	assert.GreaterOrEqual(t, len(got[0].Locations), 2)
}

// TestDiscoverInstalled_ProjectAndGlobalBecomeTwoRows is the key sectioning
// property: the same name installed at both project and global scopes shows
// as TWO InstalledItem rows, one per section. This is what enables
// per-scope toggling in the remove TUI.
func TestDiscoverInstalled_ProjectAndGlobalBecomeTwoRows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	proj := filepath.Join(cwd, ".claude", "skills", "writer")
	require.NoError(t, os.MkdirAll(proj, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, "SKILL.md"), []byte("# writer"), 0o644))

	glob := filepath.Join(home, ".claude", "skills", "writer")
	require.NoError(t, os.MkdirAll(glob, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(glob, "SKILL.md"), []byte("# writer"), 0o644))

	got, err := DiscoverInstalled()
	require.NoError(t, err)
	require.Len(t, got, 2, "one row per scope")

	scopes := map[Scope]bool{}
	for _, it := range got {
		assert.Equal(t, "writer", it.Name)
		scopes[it.Scope] = true
	}
	assert.True(t, scopes[ScopeProject])
	assert.True(t, scopes[ScopeGlobal])
}

// TestDiscoverInstalled_ProjectSectionFirst verifies the sort order: all
// project-scope rows precede all global-scope rows so the TUI renders
// project before global without an extra sort.
func TestDiscoverInstalled_ProjectSectionFirst(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	// Seed one project row, one global row, different names so they're
	// distinguishable by their (kind, name) pair alone.
	require.NoError(t, os.MkdirAll(filepath.Join(cwd, ".claude", "skills", "proj-only"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, ".claude", "skills", "proj-only", "SKILL.md"), []byte("# x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude", "skills", "global-only"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude", "skills", "global-only", "SKILL.md"), []byte("# x"), 0o644))

	got, err := DiscoverInstalled()
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, ScopeProject, got[0].Scope, "first row must be project scope")
	assert.Equal(t, "proj-only", got[0].Name)
	assert.Equal(t, ScopeGlobal, got[1].Scope)
	assert.Equal(t, "global-only", got[1].Name)
}

// TestDiscoverInstalled_SkipsDirWithoutSkillMd ensures that an unrelated
// subdir of the skills root (one without SKILL.md) is not picked up as a
// skill. Mirrors svc/plugin/manifest.go's contract.
func TestDiscoverInstalled_SkipsDirWithoutSkillMd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	root := filepath.Join(cwd, ".claude", "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notaskill"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "real"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "real", "SKILL.md"), []byte("# real"), 0o644))

	got, err := DiscoverInstalled()
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "real", got[0].Name)
}

// TestDiscoverInstalled_SkipsReadmeMd verifies that README.md in the
// agents root is not surfaced as a subagent.
func TestDiscoverInstalled_SkipsReadmeMd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	root := filepath.Join(cwd, ".claude", "agents")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# readme"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "reviewer.md"), []byte("# reviewer"), 0o644))

	got, err := DiscoverInstalled()
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "reviewer", got[0].Name)
	assert.Equal(t, InstalledSubagent, got[0].Kind)
}

// TestDiscoverInstalled_PopulatesDescription confirms the description is
// extracted from SKILL.md (skill case) and from the .md frontmatter
// (subagent case) at discovery time, so the TUI can render it inline next
// to the name without re-reading the file.
func TestDiscoverInstalled_PopulatesDescription(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	// Skill with a body-line description (no frontmatter).
	skillDir := filepath.Join(cwd, ".claude", "skills", "writer")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("# writer\nWrites things fluently.\n"), 0o644))

	// Subagent with a frontmatter description.
	subagentFile := filepath.Join(cwd, ".claude", "agents", "reviewer.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(subagentFile), 0o755))
	require.NoError(t, os.WriteFile(subagentFile,
		[]byte("---\ndescription: Reviews PRs skeptically\n---\n# reviewer\n"), 0o644))

	got, err := DiscoverInstalled()
	require.NoError(t, err)
	require.Len(t, got, 2)

	byKey := map[string]InstalledItem{}
	for _, it := range got {
		byKey[string(it.Kind)+"|"+it.Name] = it
	}
	assert.Equal(t, "Writes things fluently.", byKey["skill|writer"].Description)
	assert.Equal(t, "Reviews PRs skeptically", byKey["subagent|reviewer"].Description)
}