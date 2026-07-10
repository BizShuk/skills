package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemove_DeletesSkillDirAndReturnsName is the happy path: one skill in
// one location, Remove should delete the directory and report its name
// under ProjectSkills.
func TestRemove_DeletesSkillDirAndReturnsName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "writer")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# writer"), 0o644))

	sel := RemoveSelection{
		Items: []InstalledItem{{
			Name:  "writer",
			Kind:  InstalledSkill,
			Scope: ScopeProject,
			Locations: []InstalledLocation{
				{Agent: "claude-code", Path: dir},
			},
		}},
	}

	got, err := Remove(sel)
	require.NoError(t, err)
	assert.Equal(t, []string{"writer"}, got.ProjectSkills)
	assert.Nil(t, got.ProjectSubagents)
	assert.Nil(t, got.GlobalSkills)
	assert.Nil(t, got.GlobalSubagents)

	_, statErr := os.Stat(dir)
	assert.True(t, os.IsNotExist(statErr), "skill directory should be gone")
}

// TestRemove_DeletesSubagentFile is the same happy path for a flat .md
// file. Distinguishes from the skill-dir case (which uses RemoveAll).
func TestRemove_DeletesSubagentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reviewer.md")
	require.NoError(t, os.WriteFile(path, []byte("# reviewer"), 0o644))

	sel := RemoveSelection{
		Items: []InstalledItem{{
			Name:  "reviewer",
			Kind:  InstalledSubagent,
			Scope: ScopeProject,
			Locations: []InstalledLocation{
				{Agent: "claude-code", Path: path},
			},
		}},
	}

	got, err := Remove(sel)
	require.NoError(t, err)
	assert.Nil(t, got.ProjectSkills)
	assert.Equal(t, []string{"reviewer"}, got.ProjectSubagents)
	assert.Nil(t, got.GlobalSkills)
	assert.Nil(t, got.GlobalSubagents)

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "subagent file should be gone")
}

// TestRemove_MultipleLocationsAllDeleted verifies that an item installed in
// several locations (e.g. shared install root across multiple agents) gets
// every copy removed and the name still surfaces once in the returned list.
func TestRemove_MultipleLocationsAllDeleted(t *testing.T) {
	dir1 := filepath.Join(t.TempDir(), "loc1")
	dir2 := filepath.Join(t.TempDir(), "loc2")
	for _, d := range []string{dir1, dir2} {
		require.NoError(t, os.MkdirAll(d, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("# x"), 0o644))
	}

	sel := RemoveSelection{
		Items: []InstalledItem{{
			Name:  "writer",
			Kind:  InstalledSkill,
			Scope: ScopeProject,
			Locations: []InstalledLocation{
				{Agent: "claude-code", Path: dir1},
				{Agent: "antigravity", Path: dir2},
			},
		}},
	}

	got, err := Remove(sel)
	require.NoError(t, err)
	assert.Equal(t, []string{"writer"}, got.ProjectSkills, "name should appear once")

	for _, d := range []string{dir1, dir2} {
		_, statErr := os.Stat(d)
		assert.True(t, os.IsNotExist(statErr), "%s should be gone", d)
	}
}

// TestRemove_MissingPathIsNotAnError exercises idempotency: the second
// pass of a retry shouldn't blow up just because the first pass already
// wiped the file. os.IsNotExist errors are swallowed inside removeLocation.
func TestRemove_MissingPathIsNotAnError(t *testing.T) {
	sel := RemoveSelection{
		Items: []InstalledItem{{
			Name:  "ghost",
			Kind:  InstalledSkill,
			Scope: ScopeProject,
			Locations: []InstalledLocation{
				{Agent: "claude-code", Path: "/nonexistent/path/to/ghost"},
			},
		}},
	}

	got, err := Remove(sel)
	require.NoError(t, err)
	// Even though the path was missing, we still surface the name so the
	// caller has a chance to clean up its installs.json tracking — a stale
	// entry is still stale.
	assert.Equal(t, []string{"ghost"}, got.ProjectSkills)
}

// TestRemove_PartialFailureReturnsErrAndDeletesWhatItCan mixes a real path
// (will succeed) with an unreadable one (will fail). The successful
// deletion must still happen, and the returned err must be non-nil so
// the CLI exits non-zero.
func TestRemove_PartialFailureReturnsErrAndDeletesWhatItCan(t *testing.T) {
	good := filepath.Join(t.TempDir(), "writer")
	require.NoError(t, os.MkdirAll(good, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(good, "SKILL.md"), []byte("# x"), 0o644))

	// Build a path under a directory we cannot enter (no read perm on parent).
	banned := t.TempDir()
	require.NoError(t, os.Chmod(banned, 0o000))
	t.Cleanup(func() { _ = os.Chmod(banned, 0o755) })
	bad := filepath.Join(banned, "writer")

	sel := RemoveSelection{
		Items: []InstalledItem{{
			Name:  "writer",
			Kind:  InstalledSkill,
			Scope: ScopeProject,
			Locations: []InstalledLocation{
				{Agent: "claude-code", Path: good},
				{Agent: "antigravity", Path: bad},
			},
		}},
	}

	got, err := Remove(sel)
	require.Error(t, err, "expected partial-failure error")
	assert.Equal(t, []string{"writer"}, got.ProjectSkills, "name still reported even on partial failure")
	_, statErr := os.Stat(good)
	assert.True(t, os.IsNotExist(statErr), "good path should still be deleted")
}

// TestRemove_GlobalScopeIsSeparateBucket confirms that a global-scope item
// surfaces in the Global* lists and NOT in the Project* lists. This is the
// core property that lets update.DropNamesByScope keep project and global
// entries independent.
func TestRemove_GlobalScopeIsSeparateBucket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "writer")
	require.NoError(t, os.MkdirAll(path, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("# x"), 0o644))

	sel := RemoveSelection{
		Items: []InstalledItem{{
			Name:  "writer",
			Kind:  InstalledSkill,
			Scope: ScopeGlobal,
			Locations: []InstalledLocation{
				{Agent: "claude-code", Path: path},
			},
		}},
	}

	got, err := Remove(sel)
	require.NoError(t, err)
	assert.Equal(t, []string{"writer"}, got.GlobalSkills)
	assert.Nil(t, got.ProjectSkills)
	assert.Nil(t, got.ProjectSubagents)
	assert.Nil(t, got.GlobalSubagents)
}

// TestRemove_ProjectAndGlobalSameNameBothBuckets covers the most common
// case in the new sectioned TUI: the user picks BOTH the project row AND
// the global row of the same skill, and both buckets get populated.
func TestRemove_ProjectAndGlobalSameNameBothBuckets(t *testing.T) {
	projDir := filepath.Join(t.TempDir(), "proj")
	globDir := filepath.Join(t.TempDir(), "global")
	for _, d := range []string{projDir, globDir} {
		require.NoError(t, os.MkdirAll(d, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("# x"), 0o644))
	}

	sel := RemoveSelection{
		Items: []InstalledItem{
			{
				Name: "writer", Kind: InstalledSkill, Scope: ScopeProject,
				Locations: []InstalledLocation{{Agent: "claude-code", Path: projDir}},
			},
			{
				Name: "writer", Kind: InstalledSkill, Scope: ScopeGlobal,
				Locations: []InstalledLocation{{Agent: "claude-code", Path: globDir}},
			},
		},
	}

	got, err := Remove(sel)
	require.NoError(t, err)
	assert.Equal(t, []string{"writer"}, got.ProjectSkills)
	assert.Equal(t, []string{"writer"}, got.GlobalSkills)
}

// TestRemove_DistinctKindsAccumulate verifies that one batch with both a
// skill and a subagent returns both name lists in the appropriate scope.
func TestRemove_DistinctKindsAccumulate(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "writer")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# x"), 0o644))

	subagentFile := filepath.Join(t.TempDir(), "reviewer.md")
	require.NoError(t, os.WriteFile(subagentFile, []byte("# y"), 0o644))

	sel := RemoveSelection{
		Items: []InstalledItem{
			{
				Name: "writer", Kind: InstalledSkill, Scope: ScopeProject,
				Locations: []InstalledLocation{{Agent: "claude-code", Path: skillDir}},
			},
			{
				Name: "reviewer", Kind: InstalledSubagent, Scope: ScopeProject,
				Locations: []InstalledLocation{{Agent: "claude-code", Path: subagentFile}},
			},
		},
	}

	got, err := Remove(sel)
	require.NoError(t, err)
	assert.Equal(t, []string{"writer"}, got.ProjectSkills)
	assert.Equal(t, []string{"reviewer"}, got.ProjectSubagents)
	assert.Nil(t, got.GlobalSkills)
	assert.Nil(t, got.GlobalSubagents)
}