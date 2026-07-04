package plugin

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	abs, _ := filepath.Abs("./local/dir")

	tests := []struct {
		name string
		in   string
		want ParsedSource
	}{
		{"shorthand", "owner/repo", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git"}},
		{"shorthand-subpath", "owner/repo/skills/foo", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git", Subpath: "skills/foo"}},
		{"at-skill", "owner/repo@web-design", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git", SkillFilter: "web-design"}},
		{"github-tree", "https://github.com/owner/repo/tree/main/skills/x", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git", Ref: "main", Subpath: "skills/x"}},
		{"github-url", "https://github.com/owner/repo", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git"}},
		{"github-prefix", "github:owner/repo", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git"}},
		{"gitlab-url", "https://gitlab.com/group/sub/repo", ParsedSource{Type: GitLab, URL: "https://gitlab.com/group/sub/repo.git"}},
		{"fragment-ref", "owner/repo#v2", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git", Ref: "v2"}},
		{"wellknown", "https://example.com/team", ParsedSource{Type: WellKnown, URL: "https://example.com/team"}},
		{"local-rel", "./local/dir", ParsedSource{Type: Local, URL: abs, LocalPath: abs}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseSubpathTraversalRejected(t *testing.T) {
	_, err := Parse("owner/repo/../../etc")
	assert.Error(t, err)
}
