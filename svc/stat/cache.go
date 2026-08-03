package stat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bizshuk/skills/utils"
)

// APP_NAME is this binary's gosdk application name; it decides which
// ~/.config/<app> tree the stats cache lands in.
const APP_NAME = "skills"

// GetCacheFilePath returns the cache file path for a given date. It lands
// under this app's own data dir (~/.config/skills/data), the same directory
// svc/update writes installs.json to — previously this pointed at
// ~/.config/cc-plugin, a different application's namespace.
func GetCacheFilePath(date string) string {
	return filepath.Join(utils.AppDataDir(APP_NAME), fmt.Sprintf("stats_%s.json", date))
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
	if ds.Version != dayStatsVersion {
		return nil, fmt.Errorf("unsupported stats cache version %d", ds.Version)
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
