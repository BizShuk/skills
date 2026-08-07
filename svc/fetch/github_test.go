package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGithubArchiveUsesCodeloadWithoutToken(t *testing.T) {
	t.Setenv("GITHUB_API_TOKEN", "")

	u, h := githubArchive("owner", "repo", "main")
	assert.Equal(t, githubCodeloadBase+"/owner/repo/tar.gz/main", u)
	assert.Empty(t, h)
}

func TestGithubArchiveUsesAPIWithToken(t *testing.T) {
	t.Setenv("GITHUB_API_TOKEN", "ghs_test_token")

	u, h := githubArchive("owner", "repo", "HEAD")
	assert.Equal(t, githubAPIBase+"/repos/owner/repo/tarball/HEAD", u)
	assert.Equal(t, "Bearer ghs_test_token", h.Get("Authorization"))
	assert.Equal(t, "application/vnd.github+json", h.Get("Accept"))
}

func TestGithubTokenReadsGITHUB_API_TOKEN(t *testing.T) {
	t.Setenv("GITHUB_API_TOKEN", "from-api")
	assert.Equal(t, "from-api", githubToken())

	t.Setenv("GITHUB_API_TOKEN", "")
	assert.Equal(t, "", githubToken())
}

func TestMaterializeGitHubFallsBackToGit(t *testing.T) {
	t.Setenv("GITHUB_API_TOKEN", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	prevCode, prevAPI, prevGit := githubCodeloadBase, githubAPIBase, gitMaterialize
	githubCodeloadBase = srv.URL
	githubAPIBase = srv.URL
	fallbackDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fallbackDir, "SKILL.md"), []byte("# ok"), 0o644))
	var gotURL string
	gitMaterialize = func(_ context.Context, s ParsedSource) (string, error) {
		gotURL = s.URL
		return fallbackDir, nil
	}
	t.Cleanup(func() {
		githubCodeloadBase = prevCode
		githubAPIBase = prevAPI
		gitMaterialize = prevGit
	})

	f := &httpFetcher{client: srv.Client()}
	dir, err := f.materializeGitHub(context.Background(), ParsedSource{
		Type: GitHub,
		URL:  "https://github.com/owner/private-repo.git",
	})
	require.NoError(t, err)
	assert.Equal(t, fallbackDir, dir)
	assert.Equal(t, "https://github.com/owner/private-repo.git", gotURL)
}

func TestMaterializeGitHubSendsBearerToken(t *testing.T) {
	t.Setenv("GITHUB_API_TOKEN", "secret-token")

	payload := tarGZ(t, "repo-main", map[string]string{
		"README.md": "hello\n",
	})
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		assert.Contains(t, r.URL.Path, "/repos/owner/repo/tarball/")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	prevAPI, prevGit := githubAPIBase, gitMaterialize
	githubAPIBase = srv.URL
	// Fail loudly if archive path somehow misses — do not mask with git.
	gitMaterialize = func(context.Context, ParsedSource) (string, error) {
		t.Fatal("git fallback must not run when archive succeeds")
		return "", nil
	}
	t.Cleanup(func() {
		githubAPIBase = prevAPI
		gitMaterialize = prevGit
	})

	f := &httpFetcher{client: srv.Client()}
	dir, err := f.materializeGitHub(context.Background(), ParsedSource{
		Type: GitHub,
		URL:  "https://github.com/owner/repo.git",
		Ref:  "main",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	assert.Equal(t, "Bearer secret-token", gotAuth)
	body, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(body))
}

func TestFetchArchiveSendsCustomHeaders(t *testing.T) {
	payload := tarGZ(t, "top", map[string]string{"f.txt": "x"})
	var gotUA, gotTok string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotTok = r.Header.Get("PRIVATE-TOKEN")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	h := make(http.Header)
	h.Set("PRIVATE-TOKEN", "glpat-x")
	f := &httpFetcher{client: srv.Client()}
	dir, err := f.fetchArchive(context.Background(), srv.URL, "label", h)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	assert.Equal(t, "skills-cli", gotUA)
	assert.Equal(t, "glpat-x", gotTok)
}

func TestAlternateGitSSHURL(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantOK  bool
	}{
		{"https://github.com/o/r.git", "git@github.com:o/r.git", true},
		{"https://github.com/o/r", "git@github.com:o/r.git", true},
		{"https://gitlab.com/g/sub/r.git", "git@gitlab.com:g/sub/r.git", true},
		{"git@github.com:o/r.git", "", false},
		{"https://example.com/o/r.git", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := alternateGitSSHURL(tt.in)
		assert.Equal(t, tt.wantOK, ok, tt.in)
		assert.Equal(t, tt.want, got, tt.in)
	}
}

func TestGitlabArchiveUsesAPIWithToken(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "glpat-test")
	t.Setenv("PRIVATE_TOKEN", "")

	u, h := gitlabArchive("group/sub/repo", "main")
	assert.Contains(t, u, "/api/v4/projects/")
	assert.Contains(t, u, "group%2Fsub%2Frepo")
	assert.Contains(t, u, "sha=main")
	assert.Equal(t, "glpat-test", h.Get("PRIVATE-TOKEN"))
}

func TestGitlabArchivePublicWebFormWithoutToken(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("PRIVATE_TOKEN", "")

	u, h := gitlabArchive("group/repo", "HEAD")
	assert.Contains(t, u, "/group/repo/-/archive/HEAD/repo-HEAD.tar.gz")
	assert.Empty(t, h)
}
