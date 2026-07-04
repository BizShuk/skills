package agent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Selection is the input to Apply: which skill directories the user wants
// installed, into which agents' install locations, and whether those
// locations are the project-relative ones or the user-level ones.
//
// SkillPaths are absolute paths to skill directories, each containing a
// SKILL.md file (validated upstream by discover). Cwd is used to anchor
// relative ProjectSkillsDir / ProjectAgentsDir values; when empty, Apply
// calls os.Getwd() so callers don't have to plumb the cwd themselves.
type Selection struct {
	SkillPaths []string
	AgentTypes []AgentType
	Global     bool
	Cwd        string
}

// Apply copies each SkillPath into the destination root of each Agent.
// In global mode the destination is Agent.UserSkillsDir (absolute); in
// project mode it is Agent.ProjectSkillsDir joined with Cwd (when relative).
//
// An agent with empty UserSkillsDir in global mode, or empty ProjectSkillsDir
// in project mode, is skipped silently. Missing source paths or copy errors
// bubble up immediately so a partial failure is visible to the user.
func Apply(sel Selection) error {
	cwd := sel.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("install: resolve cwd: %w", err)
		}
	}

	// Build a lookup table of all known agents.
	agentTable := Agents()
	byType := make(map[AgentType]Agent, len(agentTable))
	for _, a := range agentTable {
		byType[a.Type] = a
	}

	for _, t := range sel.AgentTypes {
		a, ok := byType[t]
		if !ok {
			continue
		}
		destRoot := a.ProjectSkillsDir
		if sel.Global {
			destRoot = a.UserSkillsDir
		}
		if destRoot == "" {
			// Missing/empty directory in this mode → silent skip per spec.
			continue
		}
		if !sel.Global && !filepath.IsAbs(destRoot) {
			destRoot = filepath.Join(cwd, destRoot)
		}

		for _, src := range sel.SkillPaths {
			name := filepath.Base(src)
			dst := filepath.Join(destRoot, name)
			if err := copyTree(src, dst); err != nil {
				return fmt.Errorf("install: copy %s -> %s: %w", src, dst, err)
			}
		}
	}
	return nil
}

// copyTree recursively copies src (a file or directory) to dst. The
// destination's parent directories are created as needed. Source directory
// permissions (mode bits) are preserved; destination directories are created
// 0o755 so unreadable parents don't block later installs.
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst, info)
	}
	return copyFile(src, dst, info)
}

// copyDir walks src and recreates its layout under dst, recursing through
// copyTree for each entry. We refuse to follow symlinks here because Apply's
// contract is "copy a skill directory", and a symlink in a downloaded skill
// tree would silently leak paths from outside the expected source.
func copyDir(src, dst string, srcInfo os.FileInfo) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 {
			// Symlinks intentionally skipped — see copyTree comment.
			continue
		}
		srcEntry := filepath.Join(src, e.Name())
		dstEntry := filepath.Join(dst, e.Name())
		if err := copyTree(srcEntry, dstEntry); err != nil {
			return err
		}
	}
	// Best-effort: align dst's mode with src so a 0o700 skill doesn't end up
	// world-readable silently. Failure here is non-fatal.
	if srcInfo.Mode() != 0 {
		_ = os.Chmod(dst, srcInfo.Mode())
	}
	return nil
}

// copyFile creates dst (with any missing parents) and writes src into it,
// preserving the source's permission bits. The parent directory is created
// 0o755 even if the source's mode would prefer otherwise — Apply only writes
// to locations we already trust (the agent's own install dir).
func copyFile(src, dst string, srcInfo os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	mode := srcInfo.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}