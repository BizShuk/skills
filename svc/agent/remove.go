package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// RemoveSelection is the input to Remove: which items the user picked in
// the TUI. Each item carries its full Locations list so deletion knows
// where each copy lives on disk.
type RemoveSelection struct {
	Items []InstalledItem
}

// RemovedNames breaks the names of successfully-deleted items into per-scope
// buckets. The CLI uses this to call update.DropNamesByScope, which keeps
// installs.json's per-scope entries independent: removing a project-scope
// skill won't touch global entries (and vice versa).
type RemovedNames struct {
	ProjectSkills    []string
	ProjectSubagents []string
	GlobalSkills     []string
	GlobalSubagents  []string
}

// Remove deletes every location of every picked item from disk and reports
// which names were touched (so the caller can sync installs.json).
//
// Skill directories are removed recursively (os.RemoveAll). Subagent .md
// files are removed as a single file (os.Remove). A missing target is not
// an error — it just means the row's location was already gone (e.g. the
// user manually deleted one copy earlier).
//
// Per-path failures are aggregated. If ANY deletion fails for a real reason
// (permission denied, parent missing and non-empty, etc.) the returned
// error is non-nil and the partial progress is visible to the caller. The
// caller should still treat the metadata sync as "best effort" — the names
// ARE gone from some locations even when others failed.
func Remove(sel RemoveSelection) (RemovedNames, error) {
	var out RemovedNames
	var firstErr error
	recordErr := func(e error) {
		if firstErr == nil {
			firstErr = e
		}
	}

	for _, it := range sel.Items {
		var removed bool
		for _, loc := range it.Locations {
			if removeErr := removeLocation(it.Kind, loc.Path); removeErr != nil {
				recordErr(fmt.Errorf("remove %s: %s: %w", it.Kind, loc.Path, removeErr))
				continue
			}
			removed = true
		}
		if !removed {
			// No disk change for this item — don't add to the removed-names
			// list, since the file was already gone.
			continue
		}
		switch it.Scope {
		case ScopeProject:
			if it.Kind == InstalledSkill {
				out.ProjectSkills = append(out.ProjectSkills, it.Name)
			} else {
				out.ProjectSubagents = append(out.ProjectSubagents, it.Name)
			}
		case ScopeGlobal:
			if it.Kind == InstalledSkill {
				out.GlobalSkills = append(out.GlobalSkills, it.Name)
			} else {
				out.GlobalSubagents = append(out.GlobalSubagents, it.Name)
			}
		}
	}

	return out, firstErr
}

// removeLocation deletes a single (kind, path) pair. A NotExist error is
// treated as success; other errors bubble up.
func removeLocation(kind InstalledKind, path string) error {
	var removeErr error
	switch kind {
	case InstalledSkill:
		removeErr = os.RemoveAll(path)
	case InstalledSubagent:
		removeErr = os.Remove(path)
	default:
		return fmt.Errorf("unknown kind %q", kind)
	}
	if removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
		return removeErr
	}
	return nil
}