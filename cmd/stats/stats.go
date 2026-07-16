package stats

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// agentModelKey 是複合鍵 (Composite Key)，把 Agent + Model 兩個維度綁定在一起。
type agentModelKey struct {
	Agent string
	Model string
}

// AggregatedBucket 存放單一時間區段的統計資料。
// 使用複合鍵 (Composite Key) 搭配 *UsageStats，一層 map 取代三層嵌套。
type AggregatedBucket struct {
	Start time.Time
	End   time.Time
	Data  map[agentModelKey]*UsageStats
}

func NewAggregatedBucket(start, end time.Time) *AggregatedBucket {
	return &AggregatedBucket{
		Start: start,
		End:   end,
		Data:  make(map[agentModelKey]*UsageStats),
	}
}

// getOrCreate 取得指定 agent+model 的 UsageStats，不存在則自動建立。
func (b *AggregatedBucket) getOrCreate(agent, model string) *UsageStats {
	key := agentModelKey{Agent: agent, Model: model}
	if b.Data[key] == nil {
		b.Data[key] = &UsageStats{}
	}
	return b.Data[key]
}

// StatsCmd returns the stats command.
func StatsCmd() *cobra.Command {
	var bucketDuration string
	var period string

	// Set viper defaults for stats source paths.
	viper.SetDefault("sources.claude.projects_dir", "~/.claude/projects")
	viper.SetDefault("sources.codex.sessions_dir", "~/.codex/sessions")
	viper.SetDefault("sources.codex.archived_dir", "~/.codex/archived_sessions")
	viper.SetDefault("sources.antigravity.brain_dir", "~/.gemini/antigravity-ide/brain")

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show usage statistics for Claude Code, Codex, and Antigravity",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse inputs
			days, err := parsePeriod(period)
			if err != nil {
				return err
			}
			bucketHours, err := parseBucketDuration(bucketDuration)
			if err != nil {
				return err
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


			// Output Formatting (Traditional Chinese with English in brackets)
			printOverallSummary(totalInput, totalOutput, len(globalSkillCount), len(globalToolCount))
			printAgentModelDetails(globalData)
			printRanking("技能使用排行榜 (Skills Usage Ranking)", globalSkillCount)
			printRanking("工具呼叫排行榜 (Tools Usage Ranking)", globalToolCount)
			printTimeBucketBreakdown(sortedBucketUnix, bucketMap)

			return nil
		},
	}

	cmd.Flags().StringVarP(&bucketDuration, "bucket-duration", "b", "1h", "Bucket duration size (e.g. 1h, 24h, 1d)")
	cmd.Flags().StringVarP(&period, "period", "p", "7d", "Calculation period (e.g. 7d, 30d)")

	return cmd
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

func calculateDayStatsConcurrently(dateStr string, loc *time.Location) *DayStats {
	ds := NewDayStats(dateStr)

	var wg sync.WaitGroup
	wg.Add(3)

	dsClaude := NewDayStats(dateStr)
	dsCodex := NewDayStats(dateStr)
	dsAntigravity := NewDayStats(dateStr)

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

	wg.Wait()

	ds.Merge(dsClaude)
	ds.Merge(dsCodex)
	ds.Merge(dsAntigravity)

	return ds
}

type rankingEntry struct {
	name  string
	count int64
}

func printOverallSummary(totalInput, totalOutput int64, skillCount, toolCount int) {
	fmt.Println("=== 總體統計摘要 (Overall Summary) ===")
	fmt.Printf("總 Token 消耗量 (Total Tokens): %d (輸入: %d, 輸出: %d)\n", totalInput+totalOutput, totalInput, totalOutput)
	fmt.Printf("總技能使用次數 (Total Skills Used): %d\n", skillCount)
	fmt.Printf("總工具呼叫次數 (Total Tools Executed): %d\n", toolCount)
	fmt.Println()
}

func printAgentModelDetails(globalData map[agentModelKey]*UsageStats) {
	fmt.Println("=== 代理與模型明細 (Agent & Model Details) ===")
	// To make printing deterministic, sort keys first
	var keys []agentModelKey
	for k := range globalData {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Agent != keys[j].Agent {
			return keys[i].Agent < keys[j].Agent
		}
		return keys[i].Model < keys[j].Model
	})

	for _, key := range keys {
		usage := globalData[key]
		fmt.Printf("- 代理 (Agent): %s\n", key.Agent)
		fmt.Printf("  * 模型 (Model): %s\n", key.Model)
		fmt.Printf("    - Token 消耗: %d (輸入: %d, 輸出: %d)\n", usage.TotalTokens(), usage.InputTokens, usage.OutputTokens)

		// Skills — 直接從同一個 UsageStats 取得
		if len(usage.Skills) > 0 {
			var skList []string
			var sks []string
			for sk := range usage.Skills {
				sks = append(sks, sk)
			}
			sort.Strings(sks)
			for _, sk := range sks {
				skList = append(skList, fmt.Sprintf("%s (%d次)", sk, usage.Skills[sk]))
			}
			fmt.Printf("    - 使用技能 (Skills): %s\n", strings.Join(skList, ", "))
		} else {
			fmt.Println("    - 使用技能 (Skills): 無 (None)")
		}

		// Tools — 直接從同一個 UsageStats 取得
		if len(usage.Tools) > 0 {
			var tList []string
			var ts []string
			for t := range usage.Tools {
				ts = append(ts, t)
			}
			sort.Strings(ts)
			for _, t := range ts {
				tList = append(tList, fmt.Sprintf("%s (%d次)", t, usage.Tools[t]))
			}
			fmt.Printf("    - 使用工具 (Tools): %s\n", strings.Join(tList, ", "))
		} else {
			fmt.Println("    - 使用工具 (Tools): 無 (None)")
		}
	}
	fmt.Println()
}

func printRanking(title string, countMap map[string]int64) {
	fmt.Printf("=== %s ===\n", title)
	var rank []rankingEntry
	for name, count := range countMap {
		rank = append(rank, rankingEntry{name: name, count: count})
	}
	sort.Slice(rank, func(i, j int) bool {
		if rank[i].count != rank[j].count {
			return rank[i].count > rank[j].count
		}
		return rank[i].name < rank[j].name
	})
	for idx, item := range rank {
		fmt.Printf("%d. %s: %d次\n", idx+1, item.name, item.count)
	}
	if len(rank) == 0 {
		fmt.Println("(無資料)")
	}
	fmt.Println()
}

func printTimeBucketBreakdown(sortedBucketUnix []int64, bucketMap map[int64]*AggregatedBucket) {
	fmt.Println("=== 時間區段明細 (Time Bucket Breakdown) ===")
	hasActiveBuckets := false
	for _, unix := range sortedBucketUnix {
		b := bucketMap[unix]

		// Calculate bucket total tokens
		var bIn, bOut int64
		hasSkills := false
		hasTools := false
		for _, usage := range b.Data {
			bIn += usage.InputTokens
			bOut += usage.OutputTokens
			if len(usage.Skills) > 0 {
				hasSkills = true
			}
			if len(usage.Tools) > 0 {
				hasTools = true
			}
		}
		if bIn == 0 && bOut == 0 && !hasSkills && !hasTools {
			continue // skip inactive buckets
		}
		hasActiveBuckets = true

		fmt.Printf("- [%s - %s]\n", b.Start.Format("2006-01-02 15:04"), b.End.Format("15:04"))
		fmt.Printf("  * Token 消耗: %d (輸入: %d, 輸出: %d)\n", bIn+bOut, bIn, bOut)

		// Print agent breakdown
		// Sort keys to be deterministic
		var keys []agentModelKey
		for k := range b.Data {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].Agent != keys[j].Agent {
				return keys[i].Agent < keys[j].Agent
			}
			return keys[i].Model < keys[j].Model
		})

		for _, key := range keys {
			usage := b.Data[key]
			fmt.Printf("    %s (%s): %d tokens\n", key.Agent, key.Model, usage.TotalTokens())
		}

		// Print skills breakdown
		var skillsUsed []string
		for _, key := range keys {
			usage := b.Data[key]
			var sks []string
			for sk := range usage.Skills {
				sks = append(sks, sk)
			}
			sort.Strings(sks)
			skillsUsed = append(skillsUsed, sks...)
		}
		if len(skillsUsed) > 0 {
			fmt.Printf("  * 技能 (Skills): %s\n", strings.Join(skillsUsed, ", "))
		}

		// Print tools breakdown
		var toolsUsed []string
		for _, key := range keys {
			usage := b.Data[key]
			var ts []string
			for t := range usage.Tools {
				ts = append(ts, t)
			}
			sort.Strings(ts)
			for _, t := range ts {
				toolsUsed = append(toolsUsed, fmt.Sprintf("%s (%d)", t, usage.Tools[t]))
			}
		}
		if len(toolsUsed) > 0 {
			fmt.Printf("  * 工具 (Tools): %s\n", strings.Join(toolsUsed, ", "))
		}
	}
	if !hasActiveBuckets {
		fmt.Println("(無活躍時間區段)")
	}
}

