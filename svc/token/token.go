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

	gohttp "github.com/bizshuk/gosdk/http"
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

// maxAttempts is the shared project retry budget, re-exported locally
// so the API-backed counters and their tests name it once.
const maxAttempts = gohttp.DEFAULT_MAX_ATTEMPTS

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

// httpOutcome is the value utils.Retry carries between attempts. Both
// fields survive a failed loop so callers can still build an error
// message from the last status and body.
type httpOutcome struct {
	status int
	body   []byte
}

// statusError marks a non-2xx response. It exists so withRetry can tell
// "the request itself failed" apart from "the endpoint answered, just not
// with 2xx" and preserve the latter's (status, body, nil) contract.
type statusError struct {
	status int
}

func (e *statusError) Error() string { return fmt.Sprintf("http %d", e.status) }

// withRetry invokes do up to maxAttempts times for transient errors
// (429, 5xx, network errors). Permanent 4xx returns immediately.
// Backoff: 200ms × 2^(attempt-1), capped at 5s. ctx cancellation
// during sleep returns ctx.Err() without further attempts.
//
// A non-2xx response is not an error to this function: it returns the
// status and body with a nil error and lets the caller decide how to
// phrase the failure.
func withRetry(ctx context.Context, do func(ctx context.Context) (int, []byte, error)) (int, []byte, error) {
	outcome, err := gohttp.Retry(ctx, gohttp.DefaultRetryPolicy(), func(ctx context.Context) (httpOutcome, error) {
		status, body, err := do(ctx)
		out := httpOutcome{status: status, body: body}
		switch {
		case err != nil:
			// Network-level failure (DNS, dial, TLS, timeout, body read).
			return out, gohttp.Retryable(err)
		case status >= 200 && status < 300:
			return out, nil
		case gohttp.IsRetryableStatus(status):
			return out, gohttp.Retryable(&statusError{status: status})
		default:
			return out, &statusError{status: status}
		}
	})

	var status *statusError
	if errors.As(err, &status) {
		return outcome.status, outcome.body, nil
	}
	return outcome.status, outcome.body, err
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
