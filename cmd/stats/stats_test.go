package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseTime(t *testing.T) {
	tests := []struct {
		input    any
		expected time.Time
		ok       bool
	}{
		{"2026-07-15T08:16:33.604Z", time.Date(2026, 7, 15, 8, 16, 33, 604000000, time.UTC), true},
		{float64(1784103393000), time.Date(2026, 7, 15, 8, 16, 33, 0, time.UTC), true},
		{nil, time.Time{}, false},
	}
	for _, tc := range tests {
		res, ok := parseTime(tc.input)
		if ok != tc.ok {
			t.Errorf("expected ok=%v, got=%v", tc.ok, ok)
		}
		if ok && !res.UTC().Equal(tc.expected.UTC()) {
			t.Errorf("expected %v, got %v", tc.expected, res)
		}
	}
}

func TestExtractSkillName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/Users/shuk/.gemini/skills/security-scanner/SKILL.md", "security-scanner"},
		{"/Users/shuk/.claude/skills/code-review.md", "code-review"},
		{"/Users/shuk/projects/cc-plugin/cmd/root.go", ""},
	}
	for _, tc := range tests {
		res := extractSkillName(tc.input)
		if res != tc.expected {
			t.Errorf("expected %q, got %q for %q", tc.expected, res, tc.input)
		}
	}
}

func TestCacheLoadingSaving(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "stats-cache-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	ds := NewDayStats("2026-07-15")
	u := ds.Hourly["12"].GetOrCreate("claude-code", "claude-3-5-sonnet")
	u.AddTokens(1000, 500)
	u.AddSkill("security-scanner", 1)
	u.AddTool("Bash", 2)

	data, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	testFile := filepath.Join(tempDir, "stats_2026-07-15.json")
	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Read it back
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	var loaded DayStats
	if err := json.Unmarshal(content, &loaded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if loaded.Date != "2026-07-15" {
		t.Errorf("expected date 2026-07-15, got %s", loaded.Date)
	}
	usage := loaded.Hourly["12"].Usage["claude-code"]["claude-3-5-sonnet"]
	if usage.InputTokens != 1000 || usage.OutputTokens != 500 {
		t.Errorf("tokens mismatch: %+v", usage)
	}
	if usage.Skills["security-scanner"] != 1 {
		t.Errorf("skills mismatch: %v", usage.Skills)
	}
	if usage.Tools["Bash"] != 2 {
		t.Errorf("tools mismatch: %v", usage.Tools)
	}
}

func TestDayStatsMerge(t *testing.T) {
	ds1 := NewDayStats("2026-07-15")
	u1 := ds1.Hourly["10"].GetOrCreate("claude-code", "model-a")
	u1.AddTokens(100, 50)
	u1.AddSkill("skill-x", 1)
	u1.AddTool("tool-y", 2)

	ds2 := NewDayStats("2026-07-15")
	u2 := ds2.Hourly["10"].GetOrCreate("claude-code", "model-a")
	u2.AddTokens(200, 150)
	u2.AddSkill("skill-x", 3)
	u2.AddTool("tool-z", 1)

	ds1.Merge(ds2)

	merged := ds1.Hourly["10"].Usage["claude-code"]["model-a"]
	if merged.InputTokens != 300 || merged.OutputTokens != 200 {
		t.Errorf("tokens merge failed: %+v", merged)
	}
	if merged.Skills["skill-x"] != 4 {
		t.Errorf("skills merge failed: %d", merged.Skills["skill-x"])
	}
	if merged.Tools["tool-y"] != 2 || merged.Tools["tool-z"] != 1 {
		t.Errorf("tools merge failed: y=%d, z=%d", merged.Tools["tool-y"], merged.Tools["tool-z"])
	}
}

func TestUsageStatsMerge(t *testing.T) {
	a := &UsageStats{InputTokens: 100, OutputTokens: 50}
	a.AddSkill("sk1", 2)
	a.AddTool("t1", 3)

	b := &UsageStats{InputTokens: 200, OutputTokens: 100}
	b.AddSkill("sk1", 1)
	b.AddSkill("sk2", 5)
	b.AddTool("t1", 1)

	a.Merge(b)

	if a.InputTokens != 300 || a.OutputTokens != 150 {
		t.Errorf("token merge failed: %+v", a)
	}
	if a.Skills["sk1"] != 3 || a.Skills["sk2"] != 5 {
		t.Errorf("skill merge failed: %v", a.Skills)
	}
	if a.Tools["t1"] != 4 {
		t.Errorf("tool merge failed: %v", a.Tools)
	}
	if a.TotalTokens() != 450 {
		t.Errorf("TotalTokens failed: %d", a.TotalTokens())
	}
}
