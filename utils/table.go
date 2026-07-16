package utils

import (
	"fmt"
	"io"
	"strings"
)

// Cell 欄位單元格類型，可以是 string 或 []string
type Cell any

// Table 負責繪製帶有 Unicode 框線與 ANSI 顏色的通用表格
type Table struct {
	Headers    []string
	Rows       [][]Cell
	Align      []int  // 0: Left, 1: Right
	Separators []bool // 標記哪些 row 後面要印水平分隔線 ├─────┼─────┤
}

// Draw 繪製表格
// w: 輸出目標
// hasTotalRow: 是否最後一列是 Total 列 (會高亮為黃色+粗體)
// highlightLastCol: 是否最右邊一欄是 Sum/Total (會高亮為黃色)
func (t *Table) Draw(w io.Writer, hasTotalRow bool, highlightLastCol bool) {
	if len(t.Headers) == 0 {
		return
	}

	toLines := func(c Cell) []string {
		switch v := c.(type) {
		case string:
			return []string{v}
		case []string:
			return v
		default:
			if c == nil {
				return []string{""}
			}
			return []string{fmt.Sprintf("%v", c)}
		}
	}

	// 1. 計算每一欄的最大寬度 (需考慮多行 Cell 中的每一行)
	colWidths := make([]int, len(t.Headers))
	for i, h := range t.Headers {
		colWidths[i] = len(h)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(colWidths) {
				lines := toLines(cell)
				for _, line := range lines {
					if len(line) > colWidths[i] {
						colWidths[i] = len(line)
					}
				}
			}
		}
	}

	// 2. 畫頂部邊框: ┌───────────┬────────────┐
	fmt.Fprint(w, "┌")
	for i, width := range colWidths {
		fmt.Fprint(w, strings.Repeat("─", width+2))
		if i < len(colWidths)-1 {
			fmt.Fprint(w, "┬")
		}
	}
	fmt.Fprintln(w, "┐")

	// 3. 畫 Header 列 (藍色高亮 + 粗體)
	fmt.Fprint(w, "│")
	for i, h := range t.Headers {
		valStr := "\033[1;36m" + h + "\033[0m"
		
		padLen := colWidths[i] - len(h)
		leftPad := 1
		rightPad := padLen + 1
		if t.Align[i] == 1 { // Right align
			leftPad = padLen + 1
			rightPad = 1
		}
		
		fmt.Fprint(w, strings.Repeat(" ", leftPad)+valStr+strings.Repeat(" ", rightPad))
		fmt.Fprint(w, "│")
	}
	fmt.Fprintln(w)

	// 4. 畫 Header 下方的分隔線: ├───────────┼────────────┤
	fmt.Fprint(w, "├")
	for i, width := range colWidths {
		fmt.Fprint(w, strings.Repeat("─", width+2))
		if i < len(colWidths)-1 {
			fmt.Fprint(w, "┼")
		}
	}
	fmt.Fprintln(w, "┤")

	// 5. 畫資料列
	for rowIndex, row := range t.Rows {
		isTotalRow := hasTotalRow && rowIndex == len(t.Rows)-1

		// 將此列的每一個 Cell 轉成 lines，並算出最大行數
		var cellLines [][]string
		maxLines := 0
		for _, cell := range row {
			lines := toLines(cell)
			if len(lines) > maxLines {
				maxLines = len(lines)
			}
			cellLines = append(cellLines, lines)
		}

		// 渲染多行內容
		for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
			fmt.Fprint(w, "│")
			for i := 0; i < len(t.Headers); i++ {
				val := ""
				if i < len(cellLines) && lineIdx < len(cellLines[i]) {
					val = cellLines[i][lineIdx]
				}

				colorStart := ""
				colorEnd := ""
				if isTotalRow {
					// Total 列使用黃色+粗體
					colorStart = "\033[1;33m"
					colorEnd = "\033[0m"
				} else if highlightLastCol && i == len(t.Headers)-1 {
					// 最後一欄 (Sum) 使用黃色
					colorStart = "\033[33m"
					colorEnd = "\033[0m"
				}

				valStr := colorStart + val + colorEnd

				padLen := colWidths[i] - len(val)
				leftPad := 1
				rightPad := padLen + 1
				if t.Align[i] == 1 { // Right align
					leftPad = padLen + 1
					rightPad = 1
				}

				fmt.Fprint(w, strings.Repeat(" ", leftPad)+valStr+strings.Repeat(" ", rightPad))
				fmt.Fprint(w, "│")
			}
			fmt.Fprintln(w)
		}

		// 如果不是最後一列，且下一列是 Total 列，或者當前列後面有 separator，要加上分隔線 ├───────────┼────────────┤
		if rowIndex < len(t.Rows)-1 {
			drawSep := (hasTotalRow && rowIndex == len(t.Rows)-2) || (rowIndex < len(t.Separators) && t.Separators[rowIndex])
			if drawSep {
				fmt.Fprint(w, "├")
				for i, width := range colWidths {
					fmt.Fprint(w, strings.Repeat("─", width+2))
					if i < len(colWidths)-1 {
						fmt.Fprint(w, "┼")
					}
				}
				fmt.Fprintln(w, "┤")
			}
		}
	}

	// 6. 畫底部邊框: └───────────┴────────────┘
	fmt.Fprint(w, "└")
	for i, width := range colWidths {
		fmt.Fprint(w, strings.Repeat("─", width+2))
		if i < len(colWidths)-1 {
			fmt.Fprint(w, "┴")
		}
	}
	fmt.Fprintln(w, "┘")
}
