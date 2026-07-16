package stats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UsageStats 統一存放 Token 消耗、技能使用、工具呼叫。
// 三條資料流合併為一個結構體，減少平行 map 的維護成本。
type UsageStats struct {
	InputTokens  int64            `json:"input_tokens"`
	OutputTokens int64            `json:"output_tokens"`
	Skills       map[string]int64 `json:"skills,omitempty"`
	Tools        map[string]int64 `json:"tools,omitempty"`
}

func (u *UsageStats) AddTokens(input, output int64) {
	u.InputTokens += input
	u.OutputTokens += output
}

func (u *UsageStats) AddSkill(skill string, count int64) {
	if u.Skills == nil {
		u.Skills = make(map[string]int64)
	}
	u.Skills[skill] += count
}

func (u *UsageStats) AddTool(tool string, count int64) {
	if u.Tools == nil {
		u.Tools = make(map[string]int64)
	}
	u.Tools[tool] += count
}

// Merge 將 other 的所有數值累加到自身。
func (u *UsageStats) Merge(other *UsageStats) {
	if other == nil {
		return
	}
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	for sk, count := range other.Skills {
		u.AddSkill(sk, count)
	}
	for t, count := range other.Tools {
		u.AddTool(t, count)
	}
}

// TotalTokens 回傳輸入+輸出 token 總和。
func (u *UsageStats) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens
}

// HourStats 存放某小時內各 agent+model 的統計。
// 只需一個二層 map：agent -> model -> *UsageStats。
type HourStats struct {
	Usage map[string]map[string]*UsageStats `json:"usage"` // agent -> model -> usage
}

// GetOrCreate 取得指定 agent+model 的 UsageStats，不存在則自動建立。
// 所有 nil 檢查集中在這裡，外部只管丟資料進來。
func (h *HourStats) GetOrCreate(agent, model string) *UsageStats {
	if h.Usage == nil {
		h.Usage = make(map[string]map[string]*UsageStats)
	}
	if h.Usage[agent] == nil {
		h.Usage[agent] = make(map[string]*UsageStats)
	}
	if h.Usage[agent][model] == nil {
		h.Usage[agent][model] = &UsageStats{}
	}
	return h.Usage[agent][model]
}

type DayStats struct {
	Date   string                `json:"date"`   // YYYY-MM-DD
	Hourly map[string]*HourStats `json:"hourly"` // "0" to "23"
}

func NewDayStats(date string) *DayStats {
	ds := &DayStats{
		Date:   date,
		Hourly: make(map[string]*HourStats),
	}
	for i := 0; i < 24; i++ {
		hourStr := fmt.Sprintf("%d", i)
		ds.Hourly[hourStr] = &HourStats{
			Usage: make(map[string]map[string]*UsageStats),
		}
	}
	return ds
}

// expandPath expands ~ to home directory.
func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

func GetCacheFilePath(date string) string {
	dataDir := expandPath("~/.config/cc-plugin/data")
	return filepath.Join(dataDir, fmt.Sprintf("stats_%s.json", date))
}

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

// Merge merges stats from another DayStats into this one.
func (ds *DayStats) Merge(other *DayStats) {
	if other == nil {
		return
	}
	for hourStr, otherHS := range other.Hourly {
		hs := ds.Hourly[hourStr]
		if hs == nil {
			hs = &HourStats{
				Usage: make(map[string]map[string]*UsageStats),
			}
			ds.Hourly[hourStr] = hs
		}
		for agent, modelMap := range otherHS.Usage {
			for model, usage := range modelMap {
				hs.GetOrCreate(agent, model).Merge(usage)
			}
		}
	}
}
