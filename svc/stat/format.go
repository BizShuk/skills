package stat

import (
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/bizshuk/gosdk/tui"
)

// FormatReport 將完整統計結果以格式化文字輸出至 w。
func FormatReport(w io.Writer, r *StatsResult) {
	printOverallSummary(w, r)

	// 取得所有 Agent 列表，用於 Table 2 & Table 3 的欄位
	agentsMap := make(map[string]bool)
	for key := range r.GlobalData {
		agentsMap[key.Agent] = true
	}
	var agents []string
	for agent := range agentsMap {
		agents = append(agents, agent)
	}
	sort.Strings(agents)

	printTable1(w, r)
	printTable2(w, r, agents)
	printTable3(w, r, agents)
}

func printOverallSummary(w io.Writer, r *StatsResult) {
	total := r.TotalInput + r.TotalOutput
	fmt.Fprintln(w, "=== 總體統計摘要 (Overall Summary) ===")

	tbl := tui.Table{
		Headers: []string{"Metric", "Value"},
		Align:   []int{0, 0},
		Rows: [][]tui.Cell{
			{"Total Tokens", fmt.Sprintf("%s (Input: %s, Output: %s)", formatToken(total), formatToken(r.TotalInput), formatToken(r.TotalOutput))},
			{"Total Skills Used", fmt.Sprintf("%d", len(r.GlobalSkillCount))},
			{"Total Tools Executed", fmt.Sprintf("%d", len(r.GlobalToolCount))},
		},
	}
	tbl.Draw(w, false, false)
	fmt.Fprintln(w)
}

type tokenUsage struct {
	Input  int64
	Output int64
}

// Table 1: Date-based Token Usage
func printTable1(w io.Writer, r *StatsResult) {
	fmt.Fprintln(w, "=== 表格 1: 每日 Token 消耗明細 (Table 1: Token Usage by Date) ===")

	// 依 Date -> Agent -> Model 聚合 Token
	dateGroup := make(map[string]map[string]map[string]*tokenUsage)
	var totalIn, totalOut int64

	for _, b := range r.BucketMap {
		dateStr := b.Start.Format("2006-01-02")
		if dateGroup[dateStr] == nil {
			dateGroup[dateStr] = make(map[string]map[string]*tokenUsage)
		}
		for key, usage := range b.Data {
			if dateGroup[dateStr][key.Agent] == nil {
				dateGroup[dateStr][key.Agent] = make(map[string]*tokenUsage)
			}
			if dateGroup[dateStr][key.Agent][key.Model] == nil {
				dateGroup[dateStr][key.Agent][key.Model] = &tokenUsage{}
			}
			dateGroup[dateStr][key.Agent][key.Model].Input += usage.InputTokens
			dateGroup[dateStr][key.Agent][key.Model].Output += usage.OutputTokens
			totalIn += usage.InputTokens
			totalOut += usage.OutputTokens
		}
	}

	var dates []string
	for d := range dateGroup {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	var rows [][]tui.Cell
	var separators []bool

	for dateIndex, dateStr := range dates {
		var dateAgents []string
		for a := range dateGroup[dateStr] {
			dateAgents = append(dateAgents, a)
		}
		sort.Strings(dateAgents)

		var dayRows [][]tui.Cell
		for _, agent := range dateAgents {
			var dateModels []string
			for m := range dateGroup[dateStr][agent] {
				dateModels = append(dateModels, m)
			}
			sort.Strings(dateModels)

			for _, model := range dateModels {
				usage := dateGroup[dateStr][agent][model]
				dayRows = append(dayRows, []tui.Cell{
					"", // 先留空，第一行會被日期覆蓋
					agent,
					model,
					formatToken(usage.Input),
					formatToken(usage.Output),
				})
			}
		}

		if len(dayRows) > 0 {
			dayRows[0][0] = dateStr
		}

		for i, r := range dayRows {
			rows = append(rows, r)
			// 如果這是這天的最後一行，且這一天不是最後一筆日期，就加上分日期的水平分隔線
			isLastRowOfDay := i == len(dayRows)-1
			isLastDate := dateIndex == len(dates)-1
			if isLastRowOfDay && !isLastDate {
				separators = append(separators, true)
			} else {
				separators = append(separators, false)
			}
		}
	}

	// 加上 Total 列
	rows = append(rows, []tui.Cell{
		"Total",
		"",
		"",
		formatToken(totalIn),
		formatToken(totalOut),
	})
	separators = append(separators, false)

	tbl := tui.Table{
		Headers:    []string{"Date", "Agent", "Model", "Token Input", "Token Output"},
		Align:      []int{0, 0, 0, 1, 1}, // Token 數量靠右對齊
		Rows:       rows,
		Separators: separators,
	}
	tbl.Draw(w, true, false)
	fmt.Fprintln(w)
}

// Table 2: Skills Pivot Table
func printTable2(w io.Writer, r *StatsResult, agents []string) {
	fmt.Fprintln(w, "=== 表格 2: 技能使用統計 (Table 2: Skill Usage Pivot) ===")

	// 統計每個 skill 在各 agent 上的使用次數: skill -> agent -> count
	skillPivot := make(map[string]map[string]int64)
	allSkillsMap := make(map[string]bool)

	for key, usage := range r.GlobalData {
		for sk, count := range usage.Skills {
			if skillPivot[sk] == nil {
				skillPivot[sk] = make(map[string]int64)
			}
			skillPivot[sk][key.Agent] += count
			allSkillsMap[sk] = true
		}
	}

	var allSkills []string
	for sk := range allSkillsMap {
		allSkills = append(allSkills, sk)
	}
	sort.Strings(allSkills)

	// 計算各 agent 的總計與總合
	agentTotals := make(map[string]int64)
	var grandTotal int64

	var rows [][]tui.Cell
	for _, sk := range allSkills {
		row := []tui.Cell{sk}
		var total int64
		for _, agent := range agents {
			count := skillPivot[sk][agent]
			row = append(row, fmt.Sprintf("%d", count))
			agentTotals[agent] += count
			total += count
		}
		row = append(row, fmt.Sprintf("%d", total))
		grandTotal += total
		rows = append(rows, row)
	}

	// 加上 Total 列
	totalRow := []tui.Cell{"Total"}
	for _, agent := range agents {
		totalRow = append(totalRow, fmt.Sprintf("%d", agentTotals[agent]))
	}
	totalRow = append(totalRow, fmt.Sprintf("%d", grandTotal))
	rows = append(rows, totalRow)

	// 對齊設定: 第一欄靠左，其他 (各 agent 數量及 Sum) 靠右
	aligns := make([]int, len(agents)+2)
	for i := 1; i < len(aligns); i++ {
		aligns[i] = 1
	}

	headers := append([]string{"Skill Name"}, agents...)
	headers = append(headers, "Sum")

	tbl := tui.Table{
		Headers: headers,
		Align:   aligns,
		Rows:    rows,
	}
	tbl.Draw(w, true, true)
	fmt.Fprintln(w)
}

// Table 3: Tools Pivot Table
func printTable3(w io.Writer, r *StatsResult, agents []string) {
	fmt.Fprintln(w, "=== 表格 3: 工具呼叫統計 (Table 3: Tool Usage Pivot) ===")

	// 統計每個 tool 在各 agent 上的使用次數: tool -> agent -> count
	toolPivot := make(map[string]map[string]int64)
	allToolsMap := make(map[string]bool)

	for key, usage := range r.GlobalData {
		for t, count := range usage.Tools {
			if toolPivot[t] == nil {
				toolPivot[t] = make(map[string]int64)
			}
			toolPivot[t][key.Agent] += count
			allToolsMap[t] = true
		}
	}

	var allTools []string
	for t := range allToolsMap {
		allTools = append(allTools, t)
	}
	sort.Strings(allTools)

	// 計算各 agent 的總計與總合
	agentTotals := make(map[string]int64)
	var grandTotal int64

	var rows [][]tui.Cell
	for _, t := range allTools {
		row := []tui.Cell{t}
		var total int64
		for _, agent := range agents {
			count := toolPivot[t][agent]
			row = append(row, fmt.Sprintf("%d", count))
			agentTotals[agent] += count
			total += count
		}
		row = append(row, fmt.Sprintf("%d", total))
		grandTotal += total
		rows = append(rows, row)
	}

	// 加上 Total 列
	totalRow := []tui.Cell{"Total"}
	for _, agent := range agents {
		totalRow = append(totalRow, fmt.Sprintf("%d", agentTotals[agent]))
	}
	totalRow = append(totalRow, fmt.Sprintf("%d", grandTotal))
	rows = append(rows, totalRow)

	// 對齊設定
	aligns := make([]int, len(agents)+2)
	for i := 1; i < len(aligns); i++ {
		aligns[i] = 1
	}

	headers := append([]string{"Tool Name"}, agents...)
	headers = append(headers, "Sum")

	tbl := tui.Table{
		Headers: headers,
		Align:   aligns,
		Rows:    rows,
	}
	tbl.Draw(w, true, true)
	fmt.Fprintln(w)
}

// formatToken 將 Token 數以 B/M/K 單位格式化，小於單位向上取整 (Ceil)
func formatToken(n int64) string {
	if n == 0 {
		return "0"
	}
	f := float64(n)
	if f >= 1e9 {
		val := int64(math.Ceil(f / 1e9))
		return fmt.Sprintf("%dB", val)
	}
	if f >= 1e6 {
		val := int64(math.Ceil(f / 1e6))
		return fmt.Sprintf("%dM", val)
	}
	if f >= 1e3 {
		val := int64(math.Ceil(f / 1e3))
		return fmt.Sprintf("%dK", val)
	}
	// 小於 1K 則直接向上取整為 1K (除了 0 之外)
	return "1K"
}
