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

	"github.com/bizshuk/skills/svc/agent"
	"github.com/bizshuk/skills/utils"
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

// APP_NAME is this binary's gosdk application name; it decides which
// ~/.config/<app> tree the installs metadata lands in.
const APP_NAME = "skills"

// storePath returns the absolute path to the installs metadata file.
// Under gosdk convention this resolves to ~/.config/skills/data/installs.json.
func storePath() (string, error) {
	return filepath.Join(utils.AppDataDir(APP_NAME), "installs.json"), nil
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

// Upsert merges e into the entry with the same composite key (scope +
// source + projectPath): Agents, Skills and Subagents become the union of
// both, Depth takes the new value. A new key is appended. It returns the
// updated file which the caller should Save.
func Upsert(f *InstallsFile, e Entry) *InstallsFile {
	key := e.Key()
	e.UpdatedAt = time.Now()
	for i := range f.Entries {
		if f.Entries[i].Key() == key {
			old := f.Entries[i]
			e.Agents = union(old.Agents, e.Agents)
			e.Skills = union(old.Skills, e.Skills)
			e.Subagents = union(old.Subagents, e.Subagents)
			f.Entries[i] = e
			return f
		}
	}
	f.Entries = append(f.Entries, e)
	return f
}

// union returns a followed by the elements of b not already in a.
func union(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, n := range list {
			if _, dup := seen[n]; dup {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	return out
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

// DropNamesByScope is used by `skills remove`. It takes a removed-names record broken into four
// per-scope buckets (project skills, project subagents, global skills,
// global subagents) and drops each set from entries whose Scope matches:
// project names never touch global entries, and global names never touch
// project entries. This means a single `remove` invocation can take out a
// project-scope install without invalidating the global entry that
// tracks the same name at the user level. Project names only touch
// project entries whose ProjectPath equals projectPath, so removing from
// one project leaves other projects' entries intact.
//
// Entries that end up with both lists empty are removed outright. The
// returned slice is the entries that were dropped, in their original
// form, so callers can log them.
//
// DropNamesByScope mutates f.Entries in place; the caller is expected to
// Save.
func DropNamesByScope(f *InstallsFile, names agent.RemovedNames, projectPath string) []Entry {
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
			if e.ProjectPath != projectPath {
				kept = append(kept, e)
				continue
			}
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
