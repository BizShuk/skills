package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
}

func TestSamePathResolvesEquivalentAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(root, link))
	assert.True(t, samePath(root, filepath.Join(link, ".")))
}

func TestParseTimestampSupportsRFC3339AndUnixUnits(t *testing.T) {
	want := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	for _, value := range []any{
		want.Format(time.RFC3339Nano),
		float64(want.Unix()),
		float64(want.UnixMilli()),
	} {
		got, ok := parseTimestamp(value)
		require.True(t, ok)
		assert.Equal(t, want, got)
	}
	_, ok := parseTimestamp("not-a-time")
	assert.False(t, ok)
}

func TestWorkingDirectoriesFindsNestedSupportedKeys(t *testing.T) {
	value := map[string]any{
		"payload": map[string]any{
			"args": map[string]any{
				"Cwd": "/workspace/project",
			},
		},
	}
	assert.Equal(t, []string{"/workspace/project"}, workingDirectories(value))
}
