package utils

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bizshuk/gosdk/config"
	"github.com/mitchellh/go-homedir"
)

// AppDataDir returns the per-user data directory for appName, following the
// gosdk convention of ~/.config/<appName>/data.
//
// gosdk owns the happy path (config.GetAppDataDir); this function only adds
// the fallback for when no app name has been registered. That case is not
// hypothetical: config.Default is called from main, so under `go test`
// GetAppConfigDir() returns "" and GetAppDataDir() degrades to the relative
// "data" — which is how a stray data/installs.json once ended up committed
// inside svc/update. Resolving XDG_CONFIG_HOME ourselves in that branch is
// also what lets a test point the whole store at t.TempDir().
func AppDataDir(appName string) string {
	if dir := strings.TrimSpace(config.GetAppConfigDir()); dir != "" {
		return config.GetAppDataDir()
	}
	return filepath.Join(AppConfigRoot(), appName, "data")
}

// AppConfigRoot returns the XDG config base directory: XDG_CONFIG_HOME when
// set, otherwise ~/.config.
func AppConfigRoot() string {
	if base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); base != "" {
		return base
	}

	homedir.DisableCache = true
	expanded, err := homedir.Expand(filepath.Join("~", ".config"))
	if err != nil {
		return filepath.Join("~", ".config")
	}
	return expanded
}
