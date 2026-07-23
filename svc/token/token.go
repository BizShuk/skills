// Package token counts tokens in a prompt, either locally (heuristic or
// tiktoken) or via a provider's HTTP API. The package is imported by
// cmd/token.go and is the single source of truth for every counting
// strategy the CLI supports.
//
// Strategy dispatch lives in init() — see the dispatch map at the
// bottom of this file. Every new entry must be wired in init() AND
// covered by TestDispatchCoversAllProviders, otherwise a new provider
// JSON in svc/agent/providers/ would silently fall back to the local
// heuristic.
package token

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bizshuk/skills/svc/agent"
)

// httpDoer is the minimum surface of *http.Client the package uses.
// Declared first so httpClient can have this type — that way tests
// can swap it via SetHTTPClient without a type assertion at the
// call site.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// httpClient is the shared client used by every API-backed counter.
// Tests swap it with SetHTTPClient so they can stand up an
// httptest.NewServer without touching real network. Held as the
// interface type so the swap is assignment-compatible.
var httpClient httpDoer = &http.Client{Timeout: 30 * time.Second}

// SetHTTPClient swaps the package-level client (test seam).
func SetHTTPClient(c httpDoer) { httpClient = c }

// maxAttempts mirrors svc/plugin/fetch.go (CLAUDE.md convention:
// "max retry times is 5") and is applied to API-backed counts.
const maxAttempts = 5

// counter is the common shape used by every API-backed strategy and
// the local tiktoken encoder. Local heuristic uses localCount directly.
type counter func(ctx context.Context, prompt string) (int, error)

// dispatch maps agent.Type → counter. Populated in init() so each
// implementation file owns its registration (and tests can still reach
// the map for the all-providers-covered check).
var dispatch = map[agent.Type]counter{}

func init() {
	dispatch["claude-code"] = anthropicCount
	dispatch["antigravity"] = geminiCount
	dispatch["antigravity-cli"] = geminiCount
	dispatch["codex"] = tiktokenO200k
	dispatch["grok"] = tiktokenO200k
	dispatch["opencode"] = tiktokenO200k
	dispatch["hermes-agent"] = tiktokenO200k
	dispatch["pi"] = tiktokenO200k
}

// Count dispatches to the right counter based on provider. An empty
// provider runs the local heuristic. Unknown provider types produce an
// error listing every known type (sorted, comma-joined) so the user
// sees the full supported set in one place.
func Count(ctx context.Context, provider string, prompt string) (int, error) {
	if prompt == "" {
		return 0, errors.New("empty prompt")
	}
	if provider == "" {
		return localCount(prompt), nil
	}
	fn, ok := dispatch[agent.Type(provider)]
	if !ok {
		return 0, fmt.Errorf("unknown provider %q; supported: %s", provider, supportedProviders())
	}
	return fn(ctx, prompt)
}

// supportedProviders returns the comma-joined list of agent types
// known to agent.LoadAll(). Used only in error messages.
func supportedProviders() string {
	all := agent.LoadAll()
	names := make([]string, 0, len(all))
	for _, p := range all {
		names = append(names, string(p.Type))
	}
	// LoadAll already returns sorted output; left here as a defensive
	// no-op in case the upstream sort is ever removed.
	return strings.Join(names, ", ")
}

// withRetry invokes do up to maxAttempts times for transient errors
// (429, 5xx, network errors). Permanent 4xx returns immediately.
// Backoff: 200ms × 2^(attempt-1), capped at 5s. ctx cancellation
// during sleep returns ctx.Err() without further attempts.
func withRetry(ctx context.Context, do func(ctx context.Context) (int, []byte, error)) (int, []byte, error) {
	var lastStatus int
	var lastBody []byte
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status, body, err := do(ctx)
		lastStatus, lastBody, lastErr = status, body, err

		if err == nil && status >= 200 && status < 300 {
			return status, body, nil
		}
		if !isRetryable(status, err) {
			return status, body, err
		}
		if attempt < maxAttempts {
			delay := time.Duration(1<<uint(attempt-1)) * 200 * time.Millisecond
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
			select {
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return lastStatus, lastBody, lastErr
}

// isRetryable returns true for network errors, 429, and 5xx. Other
// 4xx responses are permanent and surface immediately.
func isRetryable(status int, err error) bool {
	if err != nil {
		return true
	}
	return status == http.StatusTooManyRequests || status >= 500
}

// trimForErr caps an upstream error body for inclusion in our error
// message so a chatty provider can't blow up our stderr.
func trimForErr(b []byte) string {
	const max = 256
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}
