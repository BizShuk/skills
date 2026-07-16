package utils

import (
	"bytes"
	"strings"
	"testing"
)

func TestTable_Draw(t *testing.T) {
	headers := []string{"Key", "Value"}
	align := []int{0, 0}
	
	// Row containing []string cell
	rows := [][]Cell{
		{"Single Line", "Simple text"},
		{"Multi Line", []string{"Line 1", "Line 2"}},
	}
	
	tbl := Table{
		Headers: headers,
		Align:   align,
		Rows:    rows,
	}
	
	var buf bytes.Buffer
	tbl.Draw(&buf, false, false)
	
	output := buf.String()
	
	// Verify lines are present and aligned
	if !strings.Contains(output, "┌─────────────┬─────────────┐") {
		t.Errorf("missing top border, got:\n%s", output)
	}
	if !strings.Contains(output, "│ Single Line │ Simple text │") {
		t.Errorf("missing single line row, got:\n%s", output)
	}
	if !strings.Contains(output, "│ Multi Line  │ Line 1      │") {
		t.Errorf("missing multi line first row, got:\n%s", output)
	}
	if !strings.Contains(output, "│             │ Line 2      │") {
		t.Errorf("missing multi line second row, got:\n%s", output)
	}
}
