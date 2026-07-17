package session

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bizshuk/skills/model"
)

type structuredSessionGroup struct {
	path     string
	metadata sessionMetadata
}

func discoverStructured(root, cwd, agentName string) ([]model.AgentSession, error) {
	normalizedRoot, err := normalizePath(root)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(normalizedRoot); errors.Is(err, os.ErrNotExist) {
		return []model.AgentSession{}, nil
	} else if err != nil {
		return nil, err
	}

	groups := make(map[string]*structuredSessionGroup)
	err = filepath.WalkDir(normalizedRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		extension := filepath.Ext(entry.Name())
		if extension != ".json" && extension != ".jsonl" {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}

		groupPath := structuredGroupPath(normalizedRoot, path)
		group := groups[groupPath]
		if group == nil {
			group = &structuredSessionGroup{path: groupPath}
			groups[groupPath] = group
		}
		if err := scanStructuredFile(path, func(record map[string]any) {
			group.metadata.addWorkingDirectories(workingDirectories(record), cwd)
			for _, value := range timestamps(record) {
				group.metadata.addTimestamp(value)
			}
		}); err != nil {
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sessions := make([]model.AgentSession, 0, len(groups))
	for _, group := range groups {
		fallbackID := structuredGroupID(group.path)
		if session, ok := group.metadata.session(agentName, group.path, fallbackID); ok {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func scanStructuredFile(path string, visit func(map[string]any)) error {
	if filepath.Ext(path) == ".jsonl" {
		return scanJSONL(path, visit)
	}
	return scanJSONFile(path, visit)
}

func structuredGroupPath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err == nil {
		parts := strings.Split(relative, string(filepath.Separator))
		for index, part := range parts {
			if part != ".system_generated" || index == 0 {
				continue
			}
			return filepath.Join(append([]string{root}, parts[:index]...)...)
		}
	}
	return path
}

func structuredGroupID(path string) string {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return filepath.Base(path)
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

// loadStructuredDetail reads JSON and JSONL files from a structured session.
func loadStructuredDetail(item model.AgentSession) (model.AgentSessionDetail, error) {
	info, err := os.Stat(item.Path)
	if err != nil {
		return model.AgentSessionDetail{}, fmt.Errorf("stat structured session %s: %w", item.Path, err)
	}

	detail := model.AgentSessionDetail{
		Session: item,
		Events:  make([]model.SessionEvent, 0),
	}
	visit := func(path string) error {
		return scanDetailFile(path, func(record map[string]any, raw string) error {
			if detail.CWD == "" {
				detail.CWD = firstDetailWorkingDirectory(record)
			}
			event, ok := normalizeGenericRecord(record, raw)
			if ok {
				detail.Events = append(detail.Events, event)
			}
			return nil
		})
	}

	if info.IsDir() {
		err = filepath.WalkDir(item.Path, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !entry.Type().IsRegular() || !isStructuredDetailFile(path) {
				return nil
			}
			return visit(path)
		})
	} else if info.Mode().IsRegular() && isStructuredDetailFile(item.Path) {
		err = visit(item.Path)
	}
	if err != nil {
		return model.AgentSessionDetail{}, fmt.Errorf("read structured session %s: %w", item.Path, err)
	}

	sortStructuredDetailEvents(detail.Events)
	return detail, nil
}

func isStructuredDetailFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return extension == ".json" || extension == ".jsonl"
}

func sortStructuredDetailEvents(events []model.SessionEvent) {
	sort.SliceStable(events, func(left, right int) bool {
		leftTimestamp := events[left].Timestamp
		rightTimestamp := events[right].Timestamp
		if leftTimestamp.IsZero() {
			return false
		}
		if rightTimestamp.IsZero() {
			return true
		}
		return leftTimestamp.Before(rightTimestamp)
	})
}
