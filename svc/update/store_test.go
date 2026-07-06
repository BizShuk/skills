package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
