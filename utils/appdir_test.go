package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppDataDirHonorsXDGConfigHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := AppDataDir("skills")
	want := filepath.Join(tmp, "skills", "data")
	if got != want {
		t.Fatalf("AppDataDir = %q, want %q", got, want)
	}
}

func TestAppDataDirFallsBackToHomeConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	got := AppDataDir("skills")
	if !filepath.IsAbs(got) {
		t.Fatalf("AppDataDir = %q, want an absolute path — a relative one would "+
			"resolve against the caller's working directory", got)
	}
	if !strings.HasSuffix(got, filepath.Join(".config", "skills", "data")) {
		t.Fatalf("AppDataDir = %q, want it under ~/.config/skills/data", got)
	}
	if home := os.Getenv("HOME"); home != "" && !strings.HasPrefix(got, home) {
		t.Fatalf("AppDataDir = %q, want it under HOME %q", got, home)
	}
}
