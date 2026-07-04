package fetch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bizshuk/skills/svc/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLocalMaterializeSuccess verifies that for a ParsedSource of type Local,
// the Fetcher returns the source's LocalPath verbatim, without copying or
// modifying anything on disk. We seed the dir with a SKILL.md to make sure
// the Fetcher doesn't require any particular file shape — it just has to
// confirm the directory exists.
func TestLocalMaterializeSuccess(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# test skill"), 0o644))

	f := New()
	got, err := f.Materialize(context.Background(), source.ParsedSource{
		Type:      source.Local,
		URL:       dir,
		LocalPath: dir,
	})
	require.NoError(t, err)
	assert.Equal(t, dir, got, "Local Materialize must return the source path unchanged")
}

// TestLocalMaterializeMissingPath verifies that pointing a Local source at a
// nonexistent path produces an error and the message includes the offending
// path so users can debug it. (The spec mandates the exact form
// "local path not found: X".)
func TestLocalMaterializeMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	f := New()
	got, err := f.Materialize(context.Background(), source.ParsedSource{
		Type:      source.Local,
		URL:       missing,
		LocalPath: missing,
	})
	assert.Empty(t, got, "no path should be returned on failure")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local path not found")
	assert.Contains(t, err.Error(), missing, "error must name the missing path")
}

// TestNewReturnsUsableFetcher is a smoke test asserting the public surface
// compiles and the Fetcher returned by New() is non-nil. It guards against
// accidental signature drift.
func TestNewReturnsUsableFetcher(t *testing.T) {
	f := New()
	require.NotNil(t, f)
}

// Note: real network tests of the GitHub tarball download path are out of
// scope for this package. They would require either (a) a recorded HTTP
// fixture (e.g. httptest.Server serving a canned tar.gz) or (b) hitting
// codeload.github.com over the network, both of which are intentionally
// deferred. The local path above exercises the same Fetcher interface and
// the local branch of Materialize, and unit-level coverage of the GitHub
// branch will live in the discover package via a stub Fetcher.
