package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bizshuk/skills/svc/agent"
)

func TestLoad_EmptyWhenMissing(t *testing.T) {
	// Override XDG so Load doesn't see the real installs file.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	f, err := Load()
	require.NoError(t, err)
	require.NotNil(t, f)
	assert.Equal(t, 1, f.Version)
	assert.Empty(t, f.Entries)
}

func TestSaveAndLoad_Roundtrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	f := &InstallsFile{Version: 1}
	f = Upsert(f, Entry{
		Source:      "acme/tools",
		ProjectPath: "/Users/test/proj",
		Agents:      []string{"claude-code"},
		Scope:       ScopeProject,
		Depth:       3,
		Skills:      []string{"writer", "helper"},
	})
	require.NoError(t, Save(f))

	got, err := Load()
	require.NoError(t, err)
	require.Len(t, got.Entries, 1)
	e := got.Entries[0]
	assert.Equal(t, "acme/tools", e.Source)
	assert.Equal(t, ScopeProject, e.Scope)
	assert.Equal(t, []string{"claude-code"}, e.Agents)
	assert.Equal(t, []string{"writer", "helper"}, e.Skills)
	assert.False(t, e.UpdatedAt.IsZero())
}

func TestUpsert_DifferentKeyAppends(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source:      "acme/tools",
		ProjectPath: "/p1",
		Scope:       ScopeProject,
	})
	Upsert(f, Entry{
		Source:      "acme/tools",
		ProjectPath: "/p2",
		Scope:       ScopeProject,
	})
	Upsert(f, Entry{
		Source:      "acme/tools",
		ProjectPath: "",
		Scope:       ScopeGlobal,
	})
	require.Len(t, f.Entries, 3)
}

func TestRemove(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source:      "acme/tools",
		ProjectPath: "/p1",
		Scope:       ScopeProject,
	})
	Upsert(f, Entry{
		Source:      "acme/tools",
		ProjectPath: "/p2",
		Scope:       ScopeProject,
	})
	require.Len(t, f.Entries, 2)

	_, removed := Remove(f, Entry{
		Source:      "acme/tools",
		ProjectPath: "/p1",
		Scope:       ScopeProject,
	})
	assert.True(t, removed)
	require.Len(t, f.Entries, 1)
	assert.Equal(t, "/p2", f.Entries[0].ProjectPath)
}

func TestRemove_NotPresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source:      "acme/tools",
		ProjectPath: "/p1",
		Scope:       ScopeProject,
	})

	_, removed := Remove(f, Entry{
		Source:      "acme/tools",
		ProjectPath: "/nonexistent",
		Scope:       ScopeProject,
	})
	assert.False(t, removed)
	require.Len(t, f.Entries, 1)
}

func TestKey_Stable(t *testing.T) {
	e := Entry{
		Source:      "owner/repo",
		ProjectPath: "/Users/test",
		Scope:       ScopeProject,
	}
	assert.Equal(t, "project|owner/repo|/Users/test", e.Key())

	e2 := Entry{
		Source:      "owner/repo",
		ProjectPath: "",
		Scope:       ScopeGlobal,
	}
	assert.Equal(t, "global|owner/repo|", e2.Key())
}

func TestDropNamesByScope_ProjectDropsOnlyProjectEntries(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source: "acme/proj",
		Scope:  ScopeProject,
		Skills: []string{"writer", "helper"},
	})
	Upsert(f, Entry{
		Source: "acme/global",
		Scope:  ScopeGlobal,
		Skills: []string{"writer", "helper"},
	})

	dropped := DropNamesByScope(f, agent.RemovedNames{
		ProjectSkills: []string{"writer"},
	}, "")
	assert.Empty(t, dropped, "project entry still has helper, must not be dropped")

	require.Len(t, f.Entries, 2)

	// Project entry: writer removed, helper kept.
	proj := f.Entries[0]
	assert.Equal(t, ScopeProject, proj.Scope)
	assert.Equal(t, []string{"helper"}, proj.Skills)

	// Global entry: untouched.
	glob := f.Entries[1]
	assert.Equal(t, ScopeGlobal, glob.Scope)
	assert.Equal(t, []string{"writer", "helper"}, glob.Skills)
}

func TestDropNamesByScope_GlobalDropsOnlyGlobalEntries(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source: "acme/proj",
		Scope:  ScopeProject,
		Skills: []string{"writer"},
	})
	Upsert(f, Entry{
		Source: "acme/global",
		Scope:  ScopeGlobal,
		Skills: []string{"writer"},
	})

	dropped := DropNamesByScope(f, agent.RemovedNames{
		GlobalSkills: []string{"writer"},
	}, "")
	require.Len(t, dropped, 1, "global entry should be the only one dropped")
	assert.Equal(t, "acme/global", dropped[0].Source)
	assert.Equal(t, ScopeGlobal, dropped[0].Scope)

	require.Len(t, f.Entries, 1)
	assert.Equal(t, ScopeProject, f.Entries[0].Scope)
	assert.Equal(t, []string{"writer"}, f.Entries[0].Skills)
}

func TestDropNamesByScope_BothScopesInOneCall(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source:    "acme/proj",
		Scope:     ScopeProject,
		Skills:    []string{"writer"},
		Subagents: []string{"reviewer"},
	})
	Upsert(f, Entry{
		Source:    "acme/global",
		Scope:     ScopeGlobal,
		Skills:    []string{"writer"},
		Subagents: []string{"reviewer"},
	})

	dropped := DropNamesByScope(f, agent.RemovedNames{
		ProjectSkills:   []string{"writer"},
		GlobalSubagents: []string{"reviewer"},
		// Subagents in Project, Skills in Global → each side picks up
		// exactly the half it owns.
	}, "")
	assert.Empty(t, dropped, "neither entry ends up empty after partial drops")

	require.Len(t, f.Entries, 2)

	proj := f.Entries[0]
	assert.Equal(t, ScopeProject, proj.Scope)
	assert.Empty(t, proj.Skills, "writer was dropped from project")
	assert.Equal(t, []string{"reviewer"}, proj.Subagents, "project subagent untouched")

	glob := f.Entries[1]
	assert.Equal(t, ScopeGlobal, glob.Scope)
	assert.Equal(t, []string{"writer"}, glob.Skills, "global skill untouched")
	assert.Empty(t, glob.Subagents, "reviewer was dropped from global")
}

func TestDropNamesByScope_DropsEntriesLeftEmpty(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source: "acme/proj",
		Scope:  ScopeProject,
		Skills: []string{"writer"},
	})
	Upsert(f, Entry{
		Source: "acme/global",
		Scope:  ScopeGlobal,
		Skills: []string{"helper"},
	})

	dropped := DropNamesByScope(f, agent.RemovedNames{
		ProjectSkills: []string{"writer"},
		GlobalSkills:  []string{"helper"},
	}, "")
	require.Len(t, dropped, 2, "both entries should be dropped")
	require.Empty(t, f.Entries)
}

func TestDropNamesByScope_EmptyNamesIsNoop(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source: "acme/x",
		Scope:  ScopeProject,
		Skills: []string{"writer"},
	})

	dropped := DropNamesByScope(f, agent.RemovedNames{}, "")
	assert.Empty(t, dropped)
	assert.Equal(t, []string{"writer"}, f.Entries[0].Skills)
}

func TestSave_CreatesStoreDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Remove the skills dir if it exists, to test MkdirAll.
	cfgDir, err := os.UserConfigDir()
	require.NoError(t, err)
	os.RemoveAll(filepath.Join(cfgDir, "skills"))

	f := &InstallsFile{Version: 1}
	require.NoError(t, Save(f))

	// Verify file exists.
	p, err := storePath()
	require.NoError(t, err)
	data, err := os.ReadFile(p)
	require.NoError(t, err)

	var roundtrip InstallsFile
	require.NoError(t, json.Unmarshal(data, &roundtrip))
	assert.Equal(t, 1, roundtrip.Version)
}

// TestStorePathStaysAbsoluteAndIsolated is the regression guard for the bug
// that seeded this file's isolation: storePath once resolved through an empty
// config dir, producing the RELATIVE path "data/installs.json". Every test
// here then wrote into the source tree instead of the temp dir, and the
// t.Setenv("XDG_CONFIG_HOME") lines above did nothing — silently, because a
// relative path also happens to miss the user's real installs.json, so the
// assertions still passed.
//
// Assert both halves: absolute (never relative to cwd) and inside the
// temp dir this test controls (isolation actually connected).
func TestStorePathStaysAbsoluteAndIsolated(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got, err := storePath()
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(got),
		"storePath() = %q — a relative path would resolve against the caller's cwd", got)
	assert.True(t, strings.HasPrefix(got, tmp),
		"storePath() = %q, want it under the test's XDG_CONFIG_HOME %q", got, tmp)
}

func TestDropNamesByScope_OtherProjectUntouched(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{Source: "s", Scope: ScopeProject, ProjectPath: "/a", Skills: []string{"writer"}})
	Upsert(f, Entry{Source: "s", Scope: ScopeProject, ProjectPath: "/b", Skills: []string{"writer"}})

	dropped := DropNamesByScope(f, agent.RemovedNames{ProjectSkills: []string{"writer"}}, "/a")
	require.Len(t, dropped, 1)
	assert.Equal(t, "/a", dropped[0].ProjectPath)
	require.Len(t, f.Entries, 1)
	assert.Equal(t, "/b", f.Entries[0].ProjectPath)
	assert.Equal(t, []string{"writer"}, f.Entries[0].Skills)
}

func TestUpsert_MergesSameKey(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{Source: "s", Scope: ScopeGlobal, Agents: []string{"claude"}, Skills: []string{"a", "b"}, Depth: 3})
	Upsert(f, Entry{Source: "s", Scope: ScopeGlobal, Agents: []string{"codex"}, Skills: []string{"b", "c"}, Subagents: []string{"r"}, Depth: 5})

	require.Len(t, f.Entries, 1)
	e := f.Entries[0]
	assert.Equal(t, []string{"claude", "codex"}, e.Agents)
	assert.Equal(t, []string{"a", "b", "c"}, e.Skills)
	assert.Equal(t, []string{"r"}, e.Subagents)
	assert.Equal(t, 5, e.Depth)
}
