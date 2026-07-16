package stat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
)

// GetCacheFilePath returns the cache file path for a given date.
func GetCacheFilePath(date string) string {
	homedir.DisableCache = true
	dataDir, err := homedir.Expand("~/.config/cc-plugin/data")
	if err != nil {
		dataDir = "~/.config/cc-plugin/data"
	}
	return filepath.Join(dataDir, fmt.Sprintf("stats_%s.json", date))
}

// LoadCache loads cached DayStats from disk.
func LoadCache(date string) (*DayStats, error) {
	path := GetCacheFilePath(date)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ds DayStats
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, err
	}
	return &ds, nil
}

// SaveCache persists DayStats to disk.
func SaveCache(ds *DayStats) error {
	path := GetCacheFilePath(ds.Date)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}
	data, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal DayStats: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}
	return nil
}
