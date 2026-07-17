package session

import (
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/bizshuk/skills/model"
)

func discoverGrok(root, cwd string) ([]model.AgentSession, error) {
	normalizedRoot, err := normalizePath(root)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(normalizedRoot); errors.Is(err, os.ErrNotExist) {
		return []model.AgentSession{}, nil
	} else if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(normalizedRoot)
	if err != nil {
		return nil, err
	}
	sessions := make([]model.AgentSession, 0)
	for _, project := range entries {
		if !project.IsDir() {
			continue
		}
		decodedProject, err := url.PathUnescape(project.Name())
		if err != nil || !samePath(decodedProject, cwd) {
			continue
		}
		projectPath := filepath.Join(normalizedRoot, project.Name())
		projectSessions, err := os.ReadDir(projectPath)
		if err != nil {
			return nil, err
		}
		for _, sessionDir := range projectSessions {
			if !sessionDir.IsDir() {
				continue
			}
			sessionPath := filepath.Join(projectPath, sessionDir.Name())
			metadata := sessionMetadata{
				ID:         sessionDir.Name(),
				MatchesCWD: true,
			}
			if err := walkMetadataFiles(sessionPath, func(path string) error {
				if err := scanJSONFile(path, func(record map[string]any) {
					metadata.addWorkingDirectories(workingDirectories(record), cwd)
					for _, value := range timestamps(record) {
						metadata.addTimestamp(value)
					}
				}); err != nil {
					return nil
				}
				return nil
			}); err != nil {
				return nil, err
			}
			if session, ok := metadata.session("grok", sessionPath, sessionDir.Name()); ok {
				sessions = append(sessions, session)
			}
		}
	}
	return sessions, nil
}

func walkMetadataFiles(root string, visit func(path string) error) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
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
		return visit(path)
	})
}
