package stat

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

// InitDefaults 設定 viper 預設值，供呼叫端初始化時使用。
func InitDefaults() {
	viper.SetDefault("sources.claude.projects_dir", "~/.claude/projects")
	viper.SetDefault("sources.codex.sessions_dir", "~/.codex/sessions")
	viper.SetDefault("sources.codex.archived_dir", "~/.codex/archived_sessions")
	viper.SetDefault("sources.antigravity.brain_dir", "~/.gemini/antigravity-ide/brain")
	viper.SetDefault("sources.antigravity_cli.brain_dir", "~/.gemini/antigravity-cli/brain")
}

// Run 執行完整的統計流程：解析 → 聚合 → 回傳 StatsResult。
func Run(period, bucketDuration string) (*StatsResult, error) {
	days, err := parsePeriod(period)
	if err != nil {
		return nil, err
	}
	bucketHours, err := parseBucketDuration(bucketDuration)
	if err != nil {
		return nil, err
	}

	loc := time.Local
	now := time.Now().In(loc)
	todayStr := now.Format("2006-01-02")

	// Determine date range: [today - days + 1, today]
	var dates []string
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		dates = append(dates, d)
	}

	// Load or parse stats for each day
	var dayStatsList []*DayStats
	for _, dateStr := range dates {
		var ds *DayStats
		if dateStr < todayStr {
			// Try load cache
			var cacheErr error
			ds, cacheErr = LoadCache(dateStr)
			if cacheErr != nil {
				// Traverse and calculate concurrently
				ds = calculateDayStatsConcurrently(dateStr, loc)
				_ = SaveCache(ds)
			}
		} else {
			// Today is always live parsed concurrently
			ds = calculateDayStatsConcurrently(dateStr, loc)
		}
		dayStatsList = append(dayStatsList, ds)
	}

	// Aggregate into time buckets
	bucketMap := make(map[int64]*AggregatedBucket)
	for _, ds := range dayStatsList {
		for hourStr, hs := range ds.Hourly {
			hourInt, _ := strconv.Atoi(hourStr)
			t, err := time.ParseInLocation("2006-01-02 15:04:05", fmt.Sprintf("%s %02d:00:00", ds.Date, hourInt), loc)
			if err != nil {
				continue
			}

			bucketSec := int64(bucketHours * 3600)
			bucketUnix := (t.Unix() / bucketSec) * bucketSec
			bucketStart := time.Unix(bucketUnix, 0).In(loc)
			bucketEnd := bucketStart.Add(time.Duration(bucketHours) * time.Hour)

			bucket, ok := bucketMap[bucketUnix]
			if !ok {
				bucket = NewAggregatedBucket(bucketStart, bucketEnd)
				bucketMap[bucketUnix] = bucket
			}

			// 直接用 Merge 合併，不再需要分別處理 tokens/skills/tools
			for agent, modelMap := range hs.Usage {
				for model, usage := range modelMap {
					bucket.getOrCreate(agent, model).Merge(usage)
				}
			}
		}
	}

	// Sort buckets chronologically
	var sortedBucketUnix []int64
	for u := range bucketMap {
		sortedBucketUnix = append(sortedBucketUnix, u)
	}
	sort.Slice(sortedBucketUnix, func(i, j int) bool {
		return sortedBucketUnix[i] < sortedBucketUnix[j]
	})

	// Calculate Overall Statistics
	var totalInput, totalOutput int64
	globalData := make(map[agentModelKey]*UsageStats)

	for _, b := range bucketMap {
		for key, usage := range b.Data {
			if globalData[key] == nil {
				globalData[key] = &UsageStats{}
			}
			globalData[key].Merge(usage)
			totalInput += usage.InputTokens
			totalOutput += usage.OutputTokens
		}
	}

	// Aggregate overall skills and tools dynamically from globalData
	globalSkillCount := make(map[string]int64)
	globalToolCount := make(map[string]int64)
	for _, usage := range globalData {
		for sk, count := range usage.Skills {
			globalSkillCount[sk] += count
		}
		for t, count := range usage.Tools {
			globalToolCount[t] += count
		}
	}

	return &StatsResult{
		TotalInput:       totalInput,
		TotalOutput:      totalOutput,
		GlobalData:       globalData,
		GlobalSkillCount: globalSkillCount,
		GlobalToolCount:  globalToolCount,
		SortedBucketUnix: sortedBucketUnix,
		BucketMap:        bucketMap,
	}, nil
}

func calculateDayStatsConcurrently(dateStr string, loc *time.Location) *DayStats {
	ds := NewDayStats(dateStr)

	var wg sync.WaitGroup
	wg.Add(4)

	dsClaude := NewDayStats(dateStr)
	dsCodex := NewDayStats(dateStr)
	dsAntigravity := NewDayStats(dateStr)
	dsAntigravityCli := NewDayStats(dateStr)

	go func() {
		defer wg.Done()
		_ = ParseClaudeLogs(dsClaude, dateStr, loc)
	}()

	go func() {
		defer wg.Done()
		_ = ParseCodexLogs(dsCodex, dateStr, loc)
	}()

	go func() {
		defer wg.Done()
		_ = ParseAntigravityLogs(dsAntigravity, dateStr, loc)
	}()

	go func() {
		defer wg.Done()
		_ = ParseAntigravityCliLogs(dsAntigravityCli, dateStr, loc)
	}()

	wg.Wait()

	ds.Merge(dsClaude)
	ds.Merge(dsCodex)
	ds.Merge(dsAntigravity)
	ds.Merge(dsAntigravityCli)

	return ds
}

func parsePeriod(p string) (int, error) {
	p = strings.ToLower(p)
	if strings.HasSuffix(p, "d") {
		daysStr := strings.TrimSuffix(p, "d")
		days, err := strconv.Atoi(daysStr)
		if err != nil {
			return 0, fmt.Errorf("invalid period format: %s", p)
		}
		return days, nil
	}
	// fallback if number only
	days, err := strconv.Atoi(p)
	if err == nil {
		return days, nil
	}
	return 7, nil
}

func parseBucketDuration(b string) (int, error) {
	b = strings.ToLower(b)
	if strings.HasSuffix(b, "h") {
		h, err := strconv.Atoi(strings.TrimSuffix(b, "h"))
		if err != nil {
			return 0, fmt.Errorf("invalid bucket duration: %s", b)
		}
		return h, nil
	}
	if strings.HasSuffix(b, "d") {
		d, err := strconv.Atoi(strings.TrimSuffix(b, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid bucket duration: %s", b)
		}
		return d * 24, nil
	}
	// fallback if number only
	h, err := strconv.Atoi(b)
	if err == nil {
		return h, nil
	}
	return 1, nil
}
