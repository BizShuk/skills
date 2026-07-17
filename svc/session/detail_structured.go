package session

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/bizshuk/skills/model"
)

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
	extension := filepath.Ext(path)
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
