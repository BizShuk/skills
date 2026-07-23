package token

import "unicode/utf8"

// localCount returns ceil(rune_count / 4). Anthropic Claude averages
// roughly 3.5–4 characters per token; this matches the 4-char estimate
// used by the heuristic in svc/stat/parse.go (which divides by 3 for
// its Antigravity-specific case). Always returns >= 1 for non-empty
// input so callers don't have to special-case zero-token prompts.
func localCount(prompt string) int {
	n := utf8.RuneCountInString(prompt)
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}
