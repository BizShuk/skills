package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkSource writes a local marketplace source whose plugin "p" offers the
// given skills, and returns the source root.
func mkSource(t *testing.T, skills ...string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".claude-plugin/marketplace.json"),
		[]byte(`{"plugins": [{"name": "p", "source": "./p"}]}`), 0o644))
	for _, s := range skills {
		dir := filepath.Join(root, "p", "skills", s)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+s), 0o644))
	}
	return root
}

func projectEntry(source, project string, skills ...string) Entry {
	return Entry{
		Source:      source,
		ProjectPath: project,
		Agents:      []string{"claude-code"},
		Scope:       ScopeProject,
		Depth:       3,
		Skills:      skills,
	}
}

func installed(project, skill string) bool {
	_, err := os.Stat(filepath.Join(project, ".claude/skills", skill, "SKILL.md"))
	return err == nil
}

// TestRun_UntracksSkillDroppedBySource: a skill the source stops offering is
// dropped from the entry but its copy stays on disk.
func TestRun_UntracksSkillDroppedBySource(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	project := t.TempDir()
	src := mkSource(t, "keep", "drop")
	require.NoError(t, Save(&InstallsFile{Version: 1, Entries: []Entry{projectEntry(src, project, "keep", "drop")}}))

	require.NoError(t, Run(nil))
	require.True(t, installed(project, "keep"))
	require.True(t, installed(project, "drop"))

	require.NoError(t, os.RemoveAll(filepath.Join(src, "p/skills/drop")))
	require.NoError(t, Run(nil))

	assert.True(t, installed(project, "keep"))
	assert.True(t, installed(project, "drop"), "update must only touch records, never delete from disk")
	f, err := Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"keep"}, f.Entries[0].Skills)
}

// TestRun_LeavesUntrackedProjectSkills: skills in the project dir that no
// entry tracks are never touched by update.
func TestRun_LeavesUntrackedProjectSkills(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	project := t.TempDir()
	own := filepath.Join(project, ".claude/skills/own")
	require.NoError(t, os.MkdirAll(own, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(own, "SKILL.md"), []byte("# own"), 0o644))
	src := mkSource(t, "a")
	require.NoError(t, Save(&InstallsFile{Version: 1, Entries: []Entry{projectEntry(src, project, "a", "gone")}}))

	require.NoError(t, Run(nil))

	assert.True(t, installed(project, "own"))
	assert.True(t, installed(project, "a"))
}
