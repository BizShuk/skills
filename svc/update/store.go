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

	"github.com/bizshuk/gosdk/config"

	"github.com/bizshuk/skills/svc/agent"
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
// Under gosdk convention this resolves to ~/.config/skills/data/installs.json.
func storePath() (string, error) {
	cfgDir := config.GetAppConfigDir()
	return filepath.Join(cfgDir, "data", "installs.json"), nil
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

// DropNames removes the given skill and subagent names from every entry's
// Skills / Subagents lists. Entries that end up with both lists empty are
// removed outright, so a future `skills update` run won't re-fetch a source
// that no longer has anything to install. The returned slice is the entries
// that were dropped, in their original form (before the drop), so callers
// can log them.
//
// Equality is exact-string. An entry that tracked "writer" is not matched
// by "writer-2". Sets rather than lists: passing the same name twice is
// harmless.
//
// DropNames mutates f.Entries in place; the caller is expected to Save.
func DropNames(f *InstallsFile, removedSkills, removedSubagents []string) []Entry {
	skillSet := make(map[string]struct{}, len(removedSkills))
	for _, n := range removedSkills {
		skillSet[n] = struct{}{}
	}
	saSet := make(map[string]struct{}, len(removedSubagents))
	for _, n := range removedSubagents {
		saSet[n] = struct{}{}
	}

	var dropped []Entry
	kept := make([]Entry, 0, len(f.Entries))
	for _, e := range f.Entries {
		newSkills := filterOut(e.Skills, skillSet)
		newSubagents := filterOut(e.Subagents, saSet)
		if len(newSkills) == 0 && len(newSubagents) == 0 {
			dropped = append(dropped, e)
			continue
		}
		e.Skills = newSkills
		e.Subagents = newSubagents
		kept = append(kept, e)
	}
	f.Entries = kept
	return dropped
}

// DropNamesByScope is the section-aware counterpart of DropNames used by
// `skills remove`. It takes a removed-names record broken into four
// per-scope buckets (project skills, project subagents, global skills,
// global subagents) and drops each set from entries whose Scope matches:
// project names never touch global entries, and global names never touch
// project entries. This means a single `remove` invocation can take out a
// project-scope install without invalidating the global entry that
// tracks the same name at the user level.
//
// Entries that end up with both lists empty are removed outright. The
// returned slice is the entries that were dropped, in their original
// form, so callers can log them.
//
// DropNamesByScope mutates f.Entries in place; the caller is expected to
// Save.
func DropNamesByScope(f *InstallsFile, names agent.RemovedNames) []Entry {
	projSkillSet := make(map[string]struct{}, len(names.ProjectSkills))
	for _, n := range names.ProjectSkills {
		projSkillSet[n] = struct{}{}
	}
	projSASet := make(map[string]struct{}, len(names.ProjectSubagents))
	for _, n := range names.ProjectSubagents {
		projSASet[n] = struct{}{}
	}
	globSkillSet := make(map[string]struct{}, len(names.GlobalSkills))
	for _, n := range names.GlobalSkills {
		globSkillSet[n] = struct{}{}
	}
	globSASet := make(map[string]struct{}, len(names.GlobalSubagents))
	for _, n := range names.GlobalSubagents {
		globSASet[n] = struct{}{}
	}

	var dropped []Entry
	kept := make([]Entry, 0, len(f.Entries))
	for _, e := range f.Entries {
		var skillSet, saSet map[string]struct{}
		switch e.Scope {
		case ScopeProject:
			skillSet, saSet = projSkillSet, projSASet
		case ScopeGlobal:
			skillSet, saSet = globSkillSet, globSASet
		default:
			// Unknown scope — leave the entry alone rather than risk
			// silently corrupting it.
			kept = append(kept, e)
			continue
		}
		newSkills := filterOut(e.Skills, skillSet)
		newSubagents := filterOut(e.Subagents, saSet)
		if len(newSkills) == 0 && len(newSubagents) == 0 {
			dropped = append(dropped, e)
			continue
		}
		e.Skills = newSkills
		e.Subagents = newSubagents
		kept = append(kept, e)
	}
	f.Entries = kept
	return dropped
}

// filterOut returns a copy of names with every element in skipSet removed.
// The input slice is never mutated.
func filterOut(names []string, skipSet map[string]struct{}) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if _, drop := skipSet[n]; drop {
			continue
		}
		out = append(out, n)
	}
	return out
}
