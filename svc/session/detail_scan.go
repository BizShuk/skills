package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// scanDetailFile visits each valid JSON object in a JSON or JSONL detail file.
// Malformed records are ignored so one damaged line does not hide later events.
func scanDetailFile(path string, visit func(record map[string]any, raw string) error) error {
	if filepath.Ext(path) == ".jsonl" {
		return scanDetailJSONL(path, visit)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read detail file %s: %w", path, err)
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil
	}
	return visitDetailJSONObjects(value, visit)
}

func scanDetailJSONL(path string, visit func(record map[string]any, raw string) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open detail file %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLLineSize)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var value any
		if err := json.Unmarshal(line, &value); err != nil {
			continue
		}
		record, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if err := visitDetailRecord(record, visit); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan detail file %s: %w", path, err)
	}
	return nil
}

func visitDetailJSONObjects(value any, visit func(record map[string]any, raw string) error) error {
	switch value := value.(type) {
	case map[string]any:
		return visitDetailRecord(value, visit)
	case []any:
		for _, nested := range value {
			if err := visitDetailJSONObjects(nested, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func visitDetailRecord(record map[string]any, visit func(record map[string]any, raw string) error) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil
	}
	if err := visit(record, string(raw)); err != nil {
		return fmt.Errorf("visit detail record: %w", err)
	}
	return nil
}
