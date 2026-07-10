package update

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestUpsert_ReplacesByKey(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	f := &InstallsFile{Version: 1}
	// First insert.
	Upsert(f, Entry{
		Source:      "acme/tools",
		ProjectPath: "/p1",
		Scope:       ScopeProject,
		Skills:      []string{"old"},
	})
	require.Len(t, f.Entries, 1)

	// Second insert with same key — replaces.
	Upsert(f, Entry{
		Source:      "acme/tools",
		ProjectPath: "/p1",
		Scope:       ScopeProject,
		Skills:      []string{"new"},
	})
	require.Len(t, f.Entries, 1)
	assert.Equal(t, []string{"new"}, f.Entries[0].Skills)
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

func TestDropNames_RemovesSkillFromOneEntry(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source:    "acme/tools",
		Scope:     ScopeProject,
		Skills:    []string{"writer", "helper"},
		Subagents: []string{"reviewer"},
	})
	Upsert(f, Entry{
		Source: "other/pkg",
		Scope:  ScopeProject,
		Skills: []string{"helper"}, // overlap on helper — both should keep helper
	})

	dropped := DropNames(f, []string{"writer"}, nil)
	assert.Empty(t, dropped, "entries with remaining content are not 'dropped'")
	require.Len(t, f.Entries, 2)

	// First entry: writer removed, helper + reviewer kept.
	e := f.Entries[0]
	assert.Equal(t, "acme/tools", e.Source)
	assert.Equal(t, []string{"helper"}, e.Skills)
	assert.Equal(t, []string{"reviewer"}, e.Subagents)

	// Second entry: untouched (writer was not in its list).
	assert.Equal(t, "other/pkg", f.Entries[1].Source)
	assert.Equal(t, []string{"helper"}, f.Entries[1].Skills)
}

func TestDropNames_RemovesSubagent(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source:    "acme/agents",
		Scope:     ScopeProject,
		Skills:    []string{"helper"},
		Subagents: []string{"reviewer", "tester"},
	})

	DropNames(f, nil, []string{"reviewer"})

	e := f.Entries[0]
	assert.Equal(t, []string{"helper"}, e.Skills)
	assert.Equal(t, []string{"tester"}, e.Subagents)
}

func TestDropNames_DropsEntryWhenBothListsEmpty(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source:    "acme/solo",
		Scope:     ScopeProject,
		Skills:    []string{"writer"},
		Subagents: []string{"reviewer"},
	})
	Upsert(f, Entry{
		Source: "other/keep",
		Scope:  ScopeProject,
		Skills: []string{"other"},
	})

	dropped := DropNames(f, []string{"writer"}, []string{"reviewer"})
	require.Len(t, dropped, 1, "first entry should be dropped")
	assert.Equal(t, "acme/solo", dropped[0].Source)

	require.Len(t, f.Entries, 1)
	assert.Equal(t, "other/keep", f.Entries[0].Source)
}

func TestDropNames_NoMatchIsNoop(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source: "acme/x",
		Scope:  ScopeProject,
		Skills: []string{"writer"},
	})

	dropped := DropNames(f, []string{"nonexistent"}, nil)
	assert.Empty(t, dropped)
	assert.Equal(t, []string{"writer"}, f.Entries[0].Skills)
}

func TestDropNames_DoesNotMutateInputSlices(t *testing.T) {
	f := &InstallsFile{Version: 1}
	Upsert(f, Entry{
		Source: "acme/x",
		Scope:  ScopeProject,
		Skills: []string{"writer", "helper"},
	})

	input := []string{"writer"}
	DropNames(f, input, nil)

	assert.Equal(t, []string{"writer"}, input, "caller's slice must be untouched")
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
	})
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
	})
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
		ProjectSkills:    []string{"writer"},
		GlobalSubagents:  []string{"reviewer"},
		// Subagents in Project, Skills in Global → each side picks up
		// exactly the half it owns.
	})
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
	})
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

	dropped := DropNamesByScope(f, agent.RemovedNames{})
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
