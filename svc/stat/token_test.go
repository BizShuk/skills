package stat

import (
	"testing"
)

func TestUsageStatsAddTokenUsageSeparatesCache(t *testing.T) {
	u := &UsageStats{}
	u.AddTokenUsage(100, 20, 10, 5)

	if u.InputTokens != 100 {
		t.Errorf("input tokens = %d, want 100", u.InputTokens)
	}
	if u.CacheReadTokens != 20 {
		t.Errorf("cache read tokens = %d, want 20", u.CacheReadTokens)
	}
	if u.CacheCreationTokens != 10 {
		t.Errorf("cache creation tokens = %d, want 10", u.CacheCreationTokens)
	}
	if u.OutputTokens != 5 {
		t.Errorf("output tokens = %d, want 5", u.OutputTokens)
	}
	if got := u.TotalTokens(); got != 135 {
		t.Errorf("total tokens = %d, want 135", got)
	}
}

func TestSelectClaudeTokenEntries(t *testing.T) {
	entries := []claudeTokenEntry{
		{requestID: "req-final", inputTokens: 10, stopReason: "", hasStopReason: true},
		{requestID: "req-final", inputTokens: 20, stopReason: "tool_use", hasStopReason: true},
		{requestID: "req-live", inputTokens: 30, stopReason: "", hasStopReason: true},
		{requestID: "req-live", inputTokens: 40, stopReason: "", hasStopReason: true},
		{inputTokens: 50},
	}

	selected := selectClaudeTokenEntries(entries)
	if len(selected) != 3 {
		t.Fatalf("selected %d entries, want 3", len(selected))
	}

	want := []int64{20, 40, 50}
	for i, entry := range selected {
		if entry.inputTokens != want[i] {
			t.Errorf("selected[%d].inputTokens = %d, want %d", i, entry.inputTokens, want[i])
		}
	}
}
