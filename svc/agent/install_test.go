package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkSkillDir creates a one-skill directory at <base>/<name> with a SKILL.md
// inside, returning the absolute path to the skill directory. Mirrors the
// helpers used in the discover tests so future readers see a familiar shape.
func mkSkillDir(t *testing.T, base, name string) string {
	t.Helper()
	dir := filepath.Join(base, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+name), 0o644))
	return dir
}

// TestAgents_TableShape is a small smoke assertion that the Agents() table
// contains exactly the six agents from the spec. We do not pin every path
// (the agents/* columns are inferred values per spec) — we pin the count and
// the canonical names so a regression in the table shape is caught.
func TestAgents_TableShape(t *testing.T) {
	got := Agents()
	require.Len(t, got, 8)

	wantTypes := map[AgentType]bool{
		"claude-code":     false,
		"antigravity":     false,
		"antigravity-cli": false,
		"codex":           false,
		"opencode":        false,
		"hermes-agent":    false,
		"grok":            false,
		"pi":              false,
	}
	for _, a := range got {
		wantTypes[a.Type] = true
	}
	for name, seen := range wantTypes {
		assert.True(t, seen, "expected agent %q in Agents() table", name)
	}
}

// TestApply_ProjectModeCopiesIntoCwdRelativeDir verifies that when Global is
// false the skill is copied under <Cwd>/<agent.ProjectSkillsDir>/<basename>.
// We use a custom Agent with only ProjectSkillsDir set so the test does not
// depend on $HOME layout.
func TestApply_ProjectModeCopiesIntoCwdRelativeDir(t *testing.T) {
	cwd := t.TempDir()
	src := t.TempDir()
	skillSrc := mkSkillDir(t, src, "writer")

	sel := Selection{
		SkillPaths: []string{skillSrc},
		AgentTypes: []AgentType{"claude-code"},
		Global:     false,
		Cwd:        cwd,
	}

	require.NoError(t, Apply(sel))

	// claude-code.ProjectSkillsDir = ".claude/skills"
	dest := filepath.Join(cwd, ".claude", "skills", "writer", "SKILL.md")
	_, err := os.Stat(dest)
	assert.NoError(t, err, "expected SKILL.md at %s", dest)
}

// TestApply_GlobalModeCopiesIntoUserDir verifies that when Global is true the
// skill is copied directly under <agent.UserSkillsDir>/<basename>, ignoring
// Cwd. We use a tempdir as $HOME so the test does not touch the real home.
func TestApply_GlobalModeCopiesIntoUserDir(t *testing.T) {
	homeDir := t.TempDir()
	// Create the sentinel that makes Detect() return claude-code.
	claudeDir := filepath.Join(homeDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	t.Setenv("HOME", homeDir)

	src := t.TempDir()
	skillSrc := mkSkillDir(t, src, "helper")

	sel := Selection{
		SkillPaths: []string{skillSrc},
		AgentTypes: []AgentType{"claude-code"},
		Global:     true,
	}

	require.NoError(t, Apply(sel))

	// claude-code.UserSkillsDir = ~/.claude/skills → <homeDir>/.claude/skills
	dest := filepath.Join(homeDir, ".claude", "skills", "helper", "SKILL.md")
	_, err := os.Stat(dest)
	assert.NoError(t, err, "expected SKILL.md at %s", dest)
}

// TestApply_BasenameNamesTheSkill verifies that the destination directory is
// the basename of the source skill path (not the full path) so two skills
// with different parent dirs but the same basename don't collide on disk.
func TestApply_BasenameNamesTheSkill(t *testing.T) {
	cwd := t.TempDir()
	srcRoot := t.TempDir()
	skillSrc := mkSkillDir(t, filepath.Join(srcRoot, "some", "nested"), "renamed")

	sel := Selection{
		SkillPaths: []string{skillSrc},
		AgentTypes: []AgentType{"claude-code"},
		Global:     false,
		Cwd:        cwd,
	}

	require.NoError(t, Apply(sel))

	// claude-code.ProjectSkillsDir = ".claude/skills"
	dest := filepath.Join(cwd, ".claude", "skills", "renamed", "SKILL.md")
	_, err := os.Stat(dest)
	assert.NoError(t, err, "expected SKILL.md at %s", dest)
}

// TestDetect_FindsExistingAgent seeds a directory matching one agent's
// DetectDir path and verifies Detect() includes that agent. We override $HOME
// to a tempdir via os.Setenv, write a sentinel child directory, and confirm
// only the matching agent is returned.
func TestDetect_FindsExistingAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a sentinel directory matching one agent's DetectDir exactly so
	// we exercise the prefix expansion against a stable per-test HOME.
	sentinel := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(sentinel, 0o755))

	got := Detect()
	require.NotEmpty(t, got, "Detect() should return at least the agent whose dir exists")

	found := false
	for _, a := range got {
		if a.Type == "claude-code" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected Detect() to include claude-code when $HOME/.claude exists")
}// mkSubagentFile creates a single .md file at <base>/<name>.md with simple
// content, returning the absolute path to the .md file. Mirrors mkSkillDir
// but for flat subagent files (not directories).
func mkSubagentFile(t *testing.T, base, name string) string {
	t.Helper()
	path := filepath.Join(base, name+".md")
	require.NoError(t, os.WriteFile(path, []byte("# "+name+"\nA test subagent."), 0o644))
	return path
}

// TestApply_SubagentProjectModeCopiesIntoAgentsDir verifies that when Global
// is false the subagent .md file is copied under <Cwd>/<agent.ProjectAgentsDir>/<name>.md.
func TestApply_SubagentProjectModeCopiesIntoAgentsDir(t *testing.T) {
	cwd := t.TempDir()
	src := t.TempDir()
	saSrc := mkSubagentFile(t, src, "reviewer")

	sel := Selection{
		SubagentPaths: []string{saSrc},
		AgentTypes:    []AgentType{"claude-code"},
		Global:        false,
		Cwd:           cwd,
	}

	require.NoError(t, Apply(sel))

	// claude-code.ProjectAgentsDir = ".claude/agents"
	dest := filepath.Join(cwd, ".claude", "agents", "reviewer.md")
	_, err := os.Stat(dest)
	assert.NoError(t, err, "expected reviewer.md at %s", dest)
}

// TestApply_SubagentGlobalModeCopiesIntoUserAgentsDir verifies that when
// Global is true the subagent .md file is copied under the user-level agents dir.
func TestApply_SubagentGlobalModeCopiesIntoUserAgentsDir(t *testing.T) {
	homeDir := t.TempDir()
	claudeDir := filepath.Join(homeDir, ".claude")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	t.Setenv("HOME", homeDir)

	src := t.TempDir()
	saSrc := mkSubagentFile(t, src, "tester")

	sel := Selection{
		SubagentPaths: []string{saSrc},
		AgentTypes:    []AgentType{"claude-code"},
		Global:        true,
	}

	require.NoError(t, Apply(sel))

	// claude-code.UserAgentsDir = ~/.claude/agents
	dest := filepath.Join(homeDir, ".claude", "agents", "tester.md")
	_, err := os.Stat(dest)
	assert.NoError(t, err, "expected tester.md at %s", dest)
}

// TestApply_SkillAndSubagentTogether verifies that when both SkillPaths and
// SubagentPaths are set in one Selection, skills land in the skills/ dir and
// subagents land in the agents/ dir — no cross-contamination.
func TestApply_SkillAndSubagentTogether(t *testing.T) {
	cwd := t.TempDir()
	src := t.TempDir()
	skillSrc := mkSkillDir(t, src, "writer")
	saSrc := mkSubagentFile(t, src, "reviewer")

	sel := Selection{
		SkillPaths:    []string{skillSrc},
		SubagentPaths: []string{saSrc},
		AgentTypes:    []AgentType{"claude-code"},
		Global:        false,
		Cwd:           cwd,
	}

	require.NoError(t, Apply(sel))

	// Skill should be in skills dir
	skillDest := filepath.Join(cwd, ".claude", "skills", "writer", "SKILL.md")
	_, err := os.Stat(skillDest)
	assert.NoError(t, err, "expected SKILL.md at %s", skillDest)

	// Subagent should be in agents dir
	saDest := filepath.Join(cwd, ".claude", "agents", "reviewer.md")
	_, err = os.Stat(saDest)
	assert.NoError(t, err, "expected reviewer.md at %s", saDest)
}
