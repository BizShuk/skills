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

	// Seed .claude/skills/writer and .claude/skills/helper (claude-code's
	// project skills dir), plus .claude/agents/reviewer.
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

	// Sorted by (Kind, Name): "helper" < "writer" < "code-reviewer" is
	// wrong: skills come before subagents, so helper, writer, reviewer.
	byKey := map[string]InstalledItem{}
	for _, it := range got {
		byKey[string(it.Kind)+"|"+it.Name] = it
	}

	helper := byKey["skill|helper"]
	require.Len(t, helper.Locations, 1)
	assert.Equal(t, AgentType("claude-code"), helper.Locations[0].Agent)
	assert.Equal(t, "project", string(helper.Locations[0].Scope))
	assert.Equal(t, filepath.Join(skillRoot, "helper"), helper.Locations[0].Path)

	reviewer := byKey["subagent|reviewer"]
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

	// writer installed into both claude-code (project) and antigravity (project).
	claudeSkills := filepath.Join(cwd, ".claude", "skills", "writer")
	require.NoError(t, os.MkdirAll(claudeSkills, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(claudeSkills, "SKILL.md"), []byte("# writer"), 0o644))

	antigravitySkills := filepath.Join(cwd, ".agents", "skills", "writer")
	require.NoError(t, os.MkdirAll(antigravitySkills, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(antigravitySkills, "SKILL.md"), []byte("# writer"), 0o644))

	got, err := DiscoverInstalled()
	require.NoError(t, err)
	require.Len(t, got, 1, "writer should appear as one row, not multiple")
	assert.Equal(t, "writer", got[0].Name)
	assert.Equal(t, InstalledSkill, got[0].Kind)

	// Both .claude/skills/writer and .agents/skills/writer are represented.
	// The shared .agents/skills dir contributes four agents; claude-code
	// contributes one. Total: 5 location entries.
	agents := map[AgentType]bool{}
	for _, loc := range got[0].Locations {
		agents[loc.Agent] = true
	}
	assert.True(t, agents["claude-code"], "expected claude-code in locations")
	assert.True(t, agents["antigravity"], "expected antigravity in locations")
	assert.GreaterOrEqual(t, len(got[0].Locations), 2)
}

// TestDiscoverInstalled_ProjectAndGlobalForSameAgent verifies that a skill
// installed in both project and global dirs of one agent produces a single
// row whose Locations list has two entries (one per scope). This matters
// because both copies are removed when the user picks the row.
func TestDiscoverInstalled_ProjectAndGlobalForSameAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	t.Chdir(cwd)

	// .claude/skills/writer (project)
	proj := filepath.Join(cwd, ".claude", "skills", "writer")
	require.NoError(t, os.MkdirAll(proj, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, "SKILL.md"), []byte("# writer"), 0o644))

	// ~/.claude/skills/writer (global)
	glob := filepath.Join(home, ".claude", "skills", "writer")
	require.NoError(t, os.MkdirAll(glob, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(glob, "SKILL.md"), []byte("# writer"), 0o644))

	got, err := DiscoverInstalled()
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Len(t, got[0].Locations, 2)
	scopes := map[string]bool{}
	for _, loc := range got[0].Locations {
		scopes[string(loc.Scope)] = true
	}
	assert.True(t, scopes["project"])
	assert.True(t, scopes["global"])
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
