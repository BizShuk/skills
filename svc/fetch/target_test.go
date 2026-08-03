package fetch

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tarGZ builds a gzipped tarball whose entries are wrapped in a leading
// "<top>/" directory, mimicking what codeload and the GitLab archive
// endpoint return.
func tarGZ(t *testing.T, top string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     top + "/" + name,
			Mode:     0o644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// TestMaterializeArchiveURLStripsTopLevelDir verifies the WellKnown .tar.gz
// branch: the returned directory IS the archive root, not the "<top>/"
// wrapper the archive carries.
func TestMaterializeArchiveURLStripsTopLevelDir(t *testing.T) {
	payload := tarGZ(t, "repo-main", map[string]string{
		"skills/alpha/SKILL.md": "# alpha\n\nAlpha does things.\n",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	f := New()
	dir, err := f.Materialize(context.Background(), ParsedSource{
		Type: WellKnown,
		URL:  srv.URL + "/repo.tar.gz",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	body, err := os.ReadFile(filepath.Join(dir, "skills", "alpha", "SKILL.md"))
	require.NoError(t, err, "archive must extract with its top-level dir stripped")
	assert.Contains(t, string(body), "Alpha does things")
}

// TestMaterializeMarkdownURLBecomesSkillDir verifies the WellKnown .md
// branch: a single markdown document is laid out as
// <tmp>/skills/<name>/SKILL.md, the conventional layout the scanner walks.
func TestMaterializeMarkdownURLBecomesSkillDir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# web design\n\nDesign web pages.\n"))
	}))
	defer srv.Close()

	for _, tt := range []struct {
		name    string
		urlPath string
		want    string
	}{
		{"bare document", "/docs/web-design.md", "web-design"},
		{"skill file in a named dir", "/skills/web-design/SKILL.md", "web-design"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			dir, err := f.Materialize(context.Background(), ParsedSource{
				Type: WellKnown,
				URL:  srv.URL + tt.urlPath,
			})
			require.NoError(t, err)
			t.Cleanup(func() { _ = os.RemoveAll(dir) })

			body, err := os.ReadFile(filepath.Join(dir, "skills", tt.want, "SKILL.md"))
			require.NoError(t, err, "document must land at skills/<name>/SKILL.md")
			assert.Contains(t, string(body), "Design web pages")
		})
	}
}

// TestMaterializeURLRejectsUnsupportedDocument asserts we refuse to guess at
// an arbitrary page rather than handing the scanner a directory it cannot
// interpret.
func TestMaterializeURLRejectsUnsupportedDocument(t *testing.T) {
	f := New()
	_, err := f.Materialize(context.Background(), ParsedSource{
		Type: WellKnown,
		URL:  "https://example.com/team",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported url target")
}

// TestMaterializeAppliesSubpath verifies that a target naming a path inside
// the source ("owner/repo/skills/foo") returns that inner directory, not the
// whole repo.
func TestMaterializeAppliesSubpath(t *testing.T) {
	payload := tarGZ(t, "repo-main", map[string]string{
		"skills/alpha/SKILL.md": "# alpha\n",
		"skills/beta/SKILL.md":  "# beta\n",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	f := New()
	dir, err := f.Materialize(context.Background(), ParsedSource{
		Type:    WellKnown,
		URL:     srv.URL + "/repo.tar.gz",
		Subpath: "skills/beta",
	})
	require.NoError(t, err)

	assert.Equal(t, "beta", filepath.Base(dir))
	_, err = os.Stat(filepath.Join(dir, "SKILL.md"))
	assert.NoError(t, err)
}

// TestMaterializeMissingSubpathErrors asserts a subpath that isn't in the
// fetched source fails loudly instead of silently falling back to the repo
// root — the user asked for one skill, not the whole repo.
func TestMaterializeMissingSubpathErrors(t *testing.T) {
	payload := tarGZ(t, "repo-main", map[string]string{"README.md": "hi"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	f := New()
	_, err := f.Materialize(context.Background(), ParsedSource{
		Type:    WellKnown,
		URL:     srv.URL + "/repo.tar.gz",
		Subpath: "skills/nope",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in the fetched source")
}

// TestApplySubpathRejectsEscape guards the containment check directly, since
// a subpath is attacker-controllable through a manifest.
func TestApplySubpathRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	_, err := applySubpath(dir, "../../etc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes the fetched source")
}

// TestOwnerRepoFromURL covers the identity helper the manifest scanner uses
// to key remote entries, across the URL forms that appear in manifests.
func TestOwnerRepoFromURL(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want string
	}{
		{"https://github.com/Owner/Repo.git", "owner/repo"},
		{"https://github.com/owner/repo", "owner/repo"},
		{"git@github.com:owner/repo.git", "owner/repo"},
		{"https://gitlab.com/group/sub/repo.git", "group/sub/repo"},
		{"https://example.com/owner/repo.git", ""},
		{"", ""},
	} {
		assert.Equal(t, tt.want, OwnerRepoFromURL(tt.in), tt.in)
	}
}

// TestParseRawGitHubURLIsDocument pins the routing decision for
// raw.githubusercontent.com: it serves files, so it must reach the HTTP
// document materializer rather than the git clone fallback.
func TestParseRawGitHubURLIsDocument(t *testing.T) {
	got, err := Parse("https://raw.githubusercontent.com/owner/repo/main/skills/foo/SKILL.md")
	require.NoError(t, err)
	assert.Equal(t, WellKnown, got.Type)
}
