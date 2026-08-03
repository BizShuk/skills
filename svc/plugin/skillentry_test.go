package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSkill creates <base>/<rel>/SKILL.md with the given body.
func writeSkill(t *testing.T, base, rel, body string) {
	t.Helper()
	dir := filepath.Join(base, rel)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644))
}

// writePluginManifest writes <base>/.claude-plugin/plugin.json.
func writePluginManifest(t *testing.T, base, body string) {
	t.Helper()
	dir := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(body), 0o644))
}

func skillNames(t *testing.T, base string) []string {
	t.Helper()
	parsed, err := Scan(base)
	require.NoError(t, err)
	var names []string
	for _, lp := range parsed.Locals {
		for _, s := range lp.Skills {
			names = append(names, s.Name)
		}
	}
	return names
}

// TestSkillEntry_BareRelativePathIsAPath is the core of the repo-or-path
// decision: "custom/writer" is simultaneously valid GitHub shorthand and a
// valid relative path. When it exists on disk it must be read as a path,
// not fetched from github.com/custom/writer.
func TestSkillEntry_BareRelativePathIsAPath(t *testing.T) {
	base := t.TempDir()
	writeSkill(t, base, filepath.Join("custom", "writer"), "# writer\n\nWrites things.\n")
	writePluginManifest(t, base, `{"name": "demo", "skills": ["custom/writer"]}`)

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	assert.Empty(t, parsed.Locals[0].RemoteSkills, "an on-disk path must not be fetched as a repo")
	assert.Equal(t, []string{"writer"}, skillNames(t, base))
}

// TestSkillEntry_BareRelativePathMissingIsARepo is the other half of the
// same decision: the identical spelling, with nothing at that path, is
// GitHub shorthand.
func TestSkillEntry_BareRelativePathMissingIsARepo(t *testing.T) {
	base := t.TempDir()
	writePluginManifest(t, base, `{"name": "demo", "skills": ["acme/writer"]}`)

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].RemoteSkills, 1)
	rs := parsed.Locals[0].RemoteSkills[0]
	assert.Equal(t, "acme/writer", rs.OwnerRepo)
	assert.Equal(t, "https://github.com/acme/writer.git", rs.URL)
}

// TestSkillEntry_ExplicitRemoteWinsOverLocalPath asserts the disk check is
// only a tiebreaker for ambiguous spellings — "github:acme/writer" is
// unambiguous and must stay a fetch even when acme/writer exists locally.
func TestSkillEntry_ExplicitRemoteWinsOverLocalPath(t *testing.T) {
	base := t.TempDir()
	writeSkill(t, base, filepath.Join("acme", "writer"), "# decoy\n")
	writePluginManifest(t, base, `{"name": "demo", "skills": ["github:acme/writer#v2"]}`)

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].RemoteSkills, 1)
	assert.Equal(t, "acme/writer", parsed.Locals[0].RemoteSkills[0].OwnerRepo)
	assert.Equal(t, "v2", parsed.Locals[0].RemoteSkills[0].Ref)
}

// TestSkillEntry_PathFormsAreEquivalent asserts a "./"-prefixed entry and a
// bare one resolve to the same skills. The "./" prefix used to be mandatory;
// manifests in the wild write it both ways.
func TestSkillEntry_PathFormsAreEquivalent(t *testing.T) {
	for _, entry := range []string{"./bundled", "bundled"} {
		t.Run(entry, func(t *testing.T) {
			base := t.TempDir()
			writeSkill(t, base, filepath.Join("bundled", "one"), "# one\n")
			writeSkill(t, base, filepath.Join("bundled", "two"), "# two\n")
			writePluginManifest(t, base, `{"name": "demo", "skills": ["`+entry+`"]}`)

			assert.ElementsMatch(t, []string{"one", "two"}, skillNames(t, base))
		})
	}
}

// TestSkillEntry_URLFormIsARepo covers a full git URL in the skills array,
// including a host neither GitHub nor GitLab claims — it still has to become
// a fetchable entry with a stable identity.
func TestSkillEntry_URLFormIsARepo(t *testing.T) {
	base := t.TempDir()
	writePluginManifest(t, base, `{"name": "demo", "skills": [
		"https://github.com/acme/gh-writer.git",
		"https://git.example.com/team/self-hosted.git"
	]}`)

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].RemoteSkills, 2)

	gh := parsed.Locals[0].RemoteSkills[0]
	assert.Equal(t, "acme/gh-writer", gh.OwnerRepo)
	assert.Equal(t, "https://github.com/acme/gh-writer.git", gh.URL)

	self := parsed.Locals[0].RemoteSkills[1]
	assert.Equal(t, "team/self-hosted", self.OwnerRepo, "identity falls back to trailing path segments")
	assert.Equal(t, "https://git.example.com/team/self-hosted.git", self.URL,
		"the original URL must be preserved for the fetch")
}

// TestSkillEntry_TraversalStaysDropped asserts the ambiguity check cannot be
// used to reach outside the plugin: an escaping path neither resolves as a
// path nor gets rewritten into something fetchable.
func TestSkillEntry_TraversalStaysDropped(t *testing.T) {
	outer := t.TempDir()
	writeSkill(t, outer, "secret", "# secret\n")

	base := filepath.Join(outer, "plugin")
	require.NoError(t, os.MkdirAll(base, 0o755))
	writeSkill(t, base, filepath.Join("skills", "ok"), "# ok\n")
	writePluginManifest(t, base, `{"name": "demo", "skills": ["../secret", "./skills/ok"]}`)

	assert.Equal(t, []string{"ok"}, skillNames(t, base))
}

// TestSkillEntry_AbsolutePathOutsideBaseDropped asserts the same boundary
// holds for absolute entries, which resolveManifestSkillDirs now accepts in
// principle but only inside base.
func TestSkillEntry_AbsolutePathOutsideBaseDropped(t *testing.T) {
	outer := t.TempDir()
	writeSkill(t, outer, "secret", "# secret\n")

	base := filepath.Join(outer, "plugin")
	require.NoError(t, os.MkdirAll(base, 0o755))
	writePluginManifest(t, base,
		`{"name": "demo", "skills": ["`+filepath.ToSlash(filepath.Join(outer, "secret"))+`"]}`)

	assert.Empty(t, skillNames(t, base))
}

// TestScan_BaseIsItselfASkill covers the shape a subpath fetch produces
// ("owner/repo/skills/foo") and the shape a .md URL is laid out as: a bare
// directory holding SKILL.md, with no manifest and no skills/ dir.
func TestScan_BaseIsItselfASkill(t *testing.T) {
	base := filepath.Join(t.TempDir(), "web-design")
	require.NoError(t, os.MkdirAll(base, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "SKILL.md"),
		[]byte("# web design\n\nDesign web pages.\n"), 0o644))

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)
	require.Len(t, parsed.Locals[0].Skills, 1)
	assert.Equal(t, "web-design", parsed.Locals[0].Skills[0].Name)
	assert.Equal(t, base, parsed.Locals[0].Skills[0].Path)
	assert.Contains(t, parsed.Locals[0].Skills[0].Description, "Design web pages")
}

// TestScan_MarketplaceChainsIntoPluginJSONSkills is the end-to-end shape the
// feature is about: marketplace.json names a sub-plugin by directory, and
// that directory's plugin.json declares where its skills live. Both the
// path entry and the repo entry from the inner plugin.json must surface.
func TestScan_MarketplaceChainsIntoPluginJSONSkills(t *testing.T) {
	base := t.TempDir()
	cp := filepath.Join(base, ".claude-plugin")
	require.NoError(t, os.MkdirAll(cp, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cp, "marketplace.json"), []byte(`{
		"plugins": [{"name": "writer-pack", "source": "./packs/writer"}]
	}`), 0o644))

	pack := filepath.Join(base, "packs", "writer")
	writeSkill(t, pack, filepath.Join("library", "outline"), "# outline\n")
	writePluginManifest(t, pack, `{"name": "writer-pack", "skills": ["library/outline", "acme/remote-writer"]}`)

	parsed, err := Scan(base)
	require.NoError(t, err)
	require.Len(t, parsed.Locals, 1)

	lp := parsed.Locals[0]
	assert.Equal(t, "writer-pack", lp.Name)
	require.Len(t, lp.Skills, 1)
	assert.Equal(t, "outline", lp.Skills[0].Name)
	require.Len(t, lp.RemoteSkills, 1)
	assert.Equal(t, "acme/remote-writer", lp.RemoteSkills[0].OwnerRepo)
}
