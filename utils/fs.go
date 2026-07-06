package utils

import (
	"io"
	"os"
	"path/filepath"
)

// CopyTree recursively copies src (a file or directory) to dst. The
// destination's parent directories are created as needed. Source directory
// permissions (mode bits) are preserved; destination directories are created
// 0o755 so unreadable parents don't block later installs.
func CopyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return CopyDir(src, dst, info)
	}
	return CopyFile(src, dst, info)
}

// CopyDir walks src and recreates its layout under dst, recursing through
// CopyTree for each entry. We refuse to follow symlinks here because Apply's
// contract is "copy a skill directory", and a symlink in a downloaded skill
// tree would silently leak paths from outside the expected source.
func CopyDir(src, dst string, srcInfo os.FileInfo) error {
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
		if err := CopyTree(srcEntry, dstEntry); err != nil {
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

// CopyFile creates dst (with any missing parents) and writes src into it,
// preserving the source's permission bits. The parent directory is created
// 0o755 even if the source's mode would prefer otherwise — Apply only writes
// to locations we already trust (the agent's own install dir).
func CopyFile(src, dst string, srcInfo os.FileInfo) error {
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
