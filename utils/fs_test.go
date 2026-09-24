package utils

import (
	"os"
	"path/filepath"
	"testing"
)

const skillBody = "---\nname: demo\ndescription: d\n---\nbody\n"

func writeSkill(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "skills", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillBody), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertSkillIntact(t *testing.T, src string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(src, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != skillBody {
		t.Fatalf("source SKILL.md = %q, want %q — copy truncated its own source", got, skillBody)
	}
}

// A project whose .claude/skills is a symlink back to its own skills/ makes
// dst resolve to src; opening dst with O_TRUNC would empty the source.
func TestCopyTreeSkipsWhenDstDirIsSymlinkToSrc(t *testing.T) {
	root := t.TempDir()
	src := writeSkill(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../skills", filepath.Join(root, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}

	if err := CopyTree(src, filepath.Join(root, ".claude", "skills", "demo")); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}
	assertSkillIntact(t, src)
}

func TestCopyTreeSkipsWhenDstFileIsSymlinkToSrc(t *testing.T) {
	root := t.TempDir()
	src := writeSkill(t, root)
	dst := filepath.Join(root, ".claude", "skills", "demo")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(src, "SKILL.md"), filepath.Join(dst, "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	if err := CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}
	assertSkillIntact(t, src)
}

func TestCopyTreeCopiesIntoDistinctDst(t *testing.T) {
	root := t.TempDir()
	src := writeSkill(t, root)
	dst := filepath.Join(root, ".claude", "skills", "demo")

	if err := CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != skillBody {
		t.Fatalf("dst SKILL.md = %q, want %q", got, skillBody)
	}
}
