package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const maxJSONLLineSize = 16 * 1024 * 1024

func walkJSONLFiles(root string, visit func(path string) error) error {
	normalizedRoot, err := normalizePath(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(normalizedRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(normalizedRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return visit(path)
	})
}

func scanJSONL(path string, visit func(map[string]any)) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLLineSize)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		visit(record)
	}
	return scanner.Err()
}

func scanJSONFile(path string, visit func(map[string]any)) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil
	}
	visitJSONObjects(value, visit)
	return nil
}

func visitJSONObjects(value any, visit func(map[string]any)) {
	switch value := value.(type) {
	case map[string]any:
		visit(value)
	case []any:
		for _, nested := range value {
			visitJSONObjects(nested, visit)
		}
	}
}
