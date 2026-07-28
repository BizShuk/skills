package rule

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Target describes one global rule path and the agents that consume it.
type Target struct {
	Path   string
	Agents []string
}

// Installed describes one successfully written unique target path.
type Installed struct {
	Path   string
	Agents []string
}

// Apply writes content to every unique target path and returns all successful
// writes. Failures are joined after every target has been attempted.
func Apply(content []byte, targets []Target) ([]Installed, error) {
	grouped, groupErrs := groupTargets(targets)
	errs := append([]error(nil), groupErrs...)
	installed := make([]Installed, 0, len(grouped))

	for _, target := range grouped {
		if err := writeAtomic(target.Path, content); err != nil {
			errs = append(errs, fmt.Errorf("install global rule %s: %w", target.Path, err))
			continue
		}
		installed = append(installed, Installed(target))
	}

	return installed, errors.Join(errs...)
}

func groupTargets(targets []Target) ([]Target, []error) {
	agentsByPath := make(map[string]map[string]struct{}, len(targets))
	var errs []error

	for _, target := range targets {
		if target.Path == "" {
			errs = append(errs, fmt.Errorf("install global rule: empty target path"))
			continue
		}
		if agentsByPath[target.Path] == nil {
			agentsByPath[target.Path] = make(map[string]struct{})
		}
		for _, agentName := range target.Agents {
			if agentName != "" {
				agentsByPath[target.Path][agentName] = struct{}{}
			}
		}
	}

	paths := make([]string, 0, len(agentsByPath))
	for path := range agentsByPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	grouped := make([]Target, 0, len(paths))
	for _, path := range paths {
		agents := make([]string, 0, len(agentsByPath[path]))
		for agentName := range agentsByPath[path] {
			agents = append(agents, agentName)
		}
		sort.Strings(agents)
		grouped = append(grouped, Target{Path: path, Agents: agents})
	}
	return grouped, errs
}

func writeAtomic(path string, content []byte) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create parent: %w", err)
	}

	file, err := os.CreateTemp(parent, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tempPath := file.Name()
	closed := false
	defer func() {
		if !closed {
			// Cleanup is best-effort; the primary write error is more useful.
			_ = file.Close()
		}
		// Rename removes the temporary path on success; cleanup is best-effort.
		_ = os.Remove(tempPath)
	}()

	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := file.Chmod(0o644); err != nil {
		return fmt.Errorf("set mode: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	closed = true

	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace target: %w", err)
	}
	return nil
}
