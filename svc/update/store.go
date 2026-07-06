// Package update handles the "skills update" subcommand: it reads the
// install metadata persisted during "skills add", validates that tracked
// project paths still exist, re-runs discovery and installation for each,
// and removes entries whose project paths are gone.
//
// # Store location
//
// Metadata is stored at <user-config-dir>/skills/data/installs.json,
// following the github.com/bizshuk/gosdk convention of
// ~/.config/<app_name>/data/.
package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Scope is either "project" or "global" — where skills were installed.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// Entry is one row in the installs file. It records what was installed,
// where, and with which settings, so "skills update" can reproduce the
// exact same operation later.
type Entry struct {
	Source      string    `json:"source"`      // original "skills add [path]" argument
	ProjectPath string    `json:"projectPath"` // absolute CWD at install time; "" for global
	Agents      []string  `json:"agents"`      // agent type names
	Scope       Scope     `json:"scope"`       // "project" or "global"
	Depth       int       `json:"depth"`       // --depth value used
	Skills      []string  `json:"skills"`      // installed skill directory names
	Subagents   []string  `json:"subagents"`   // installed subagent file names
	UpdatedAt   time.Time `json:"updatedAt"`   // last install or update time
}

// Key returns the composite unique key for this entry: (scope, source, projectPath).
func (e Entry) Key() string {
	return fmt.Sprintf("%s|%s|%s", e.Scope, e.Source, e.ProjectPath)
}

// InstallsFile is the top-level JSON document persisted to disk.
type InstallsFile struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// storePath returns the absolute path to the installs metadata file.
// On macOS this is ~/.config/skills/data/installs.json.
func storePath() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("update: cannot resolve user config dir: %w", err)
	}
	return filepath.Join(cfgDir, "skills", "data", "installs.json"), nil
}

// Load reads and decodes the installs file. If the file does not exist it
// returns an empty InstallsFile without error — the first "skills add" on
// this machine will create it.
func Load() (*InstallsFile, error) {
	p, err := storePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &InstallsFile{Version: 1}, nil
		}
		return nil, fmt.Errorf("update: read installs: %w", err)
	}
	var f InstallsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("update: parse installs: %w", err)
	}
	if f.Version == 0 {
		f.Version = 1
	}
	return &f, nil
}

// Save writes the installs file to disk, creating parent directories as
// needed. The file is written atomically by first writing to a temp file
// and then renaming.
func Save(f *InstallsFile) error {
	p, err := storePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("update: create data dir: %w", err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("update: marshal installs: %w", err)
	}
	data = append(data, '\n')

	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("update: write installs: %w", err)
	}
	return os.Rename(tmp, p)
}

// Upsert inserts or replaces an entry in the file by its composite key
// (scope + source + projectPath). It returns the updated file which the
// caller should Save.
func Upsert(f *InstallsFile, e Entry) *InstallsFile {
	key := e.Key()
	e.UpdatedAt = time.Now()
	for i := range f.Entries {
		if f.Entries[i].Key() == key {
			f.Entries[i] = e
			return f
		}
	}
	f.Entries = append(f.Entries, e)
	return f
}

// Remove deletes one entry by its composite key. It returns true if an
// entry was removed.
func Remove(f *InstallsFile, e Entry) ([]Entry, bool) {
	key := e.Key()
	filtered := make([]Entry, 0, len(f.Entries))
	removed := false
	for i := range f.Entries {
		if f.Entries[i].Key() == key {
			removed = true
			continue
		}
		filtered = append(filtered, f.Entries[i])
	}
	f.Entries = filtered
	return f.Entries, removed
}
