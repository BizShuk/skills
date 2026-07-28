package rule

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyCreatesParentWithExactContentAndMode(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "nested", "AGENTS.md")
	content := []byte("# Rule\n\n保留尾端換行\n")

	installed, err := Apply(content, []Target{{
		Path:   targetPath,
		Agents: []string{"codex"},
	}})
	require.NoError(t, err)
	require.Equal(t, []Installed{{
		Path:   targetPath,
		Agents: []string{"codex"},
	}}, installed)

	got, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, content, got)

	info, err := os.Stat(targetPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestApplyDeduplicatesSharedPaths(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "GEMINI.md")

	installed, err := Apply([]byte("shared"), []Target{
		{Path: targetPath, Agents: []string{"antigravity-cli"}},
		{Path: targetPath, Agents: []string{"antigravity", "antigravity-cli"}},
	})
	require.NoError(t, err)
	require.Len(t, installed, 1)
	assert.Equal(t, targetPath, installed[0].Path)
	assert.Equal(t, []string{"antigravity", "antigravity-cli"}, installed[0].Agents)
}

func TestApplyOverwritesRegularFile(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "CLAUDE.md")
	require.NoError(t, os.WriteFile(targetPath, []byte("old"), 0o600))

	_, err := Apply([]byte("new"), []Target{{Path: targetPath}})
	require.NoError(t, err)

	got, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), got)

	info, err := os.Stat(targetPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestApplyReplacesSymlinkWithoutChangingSource(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.md")
	targetPath := filepath.Join(root, "AGENTS.md")
	require.NoError(t, os.WriteFile(sourcePath, []byte("source"), 0o644))
	if err := os.Symlink(sourcePath, targetPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, err := Apply([]byte("installed"), []Target{{Path: targetPath}})
	require.NoError(t, err)

	targetInfo, err := os.Lstat(targetPath)
	require.NoError(t, err)
	assert.Zero(t, targetInfo.Mode()&os.ModeSymlink)

	gotTarget, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("installed"), gotTarget)

	gotSource, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	assert.Equal(t, []byte("source"), gotSource)
}

func TestApplyContinuesAfterTargetFailure(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	badPath := filepath.Join(blocker, "AGENTS.md")
	goodPath := filepath.Join(root, "working", "AGENTS.md")
	installed, err := Apply([]byte("rule"), []Target{
		{Path: badPath, Agents: []string{"broken"}},
		{Path: goodPath, Agents: []string{"codex"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), badPath)
	require.Equal(t, []Installed{{
		Path:   goodPath,
		Agents: []string{"codex"},
	}}, installed)

	got, readErr := os.ReadFile(goodPath)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("rule"), got)
}
