// Package rule downloads and installs global rule files for agent runtimes.
package rule

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	gohttp "github.com/bizshuk/gosdk/http"
)

const (
	// DefaultURL is the global rule source used when the install command
	// receives no positional URL.
	DefaultURL = "https://raw.githubusercontent.com/BizShuk/cc-plugin/refs/heads/master/config/CLAUDE.global.md"

	maxFetchAttempts = gohttp.DEFAULT_MAX_ATTEMPTS
)

// Fetcher downloads a global rule document. RetryDelay is applied between
// transient attempts; a zero value retries immediately.
type Fetcher struct {
	Client     *http.Client
	RetryDelay time.Duration
}

// NewFetcher returns the production HTTP fetcher.
func NewFetcher() Fetcher {
	return Fetcher{
		Client:     &http.Client{Timeout: 30 * time.Second},
		RetryDelay: 200 * time.Millisecond,
	}
}

// Fetch downloads sourceURL and returns its response bytes unchanged.
func (f Fetcher) Fetch(ctx context.Context, sourceURL string) ([]byte, error) {
	parsed, err := url.ParseRequestURI(sourceURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("fetch global rule: invalid http URL %q", sourceURL)
	}

	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}

	body, err := gohttp.Retry(
		ctx,
		gohttp.ConstantRetryPolicy(maxFetchAttempts, f.RetryDelay),
		func(ctx context.Context) ([]byte, error) {
			return fetchOnce(ctx, client, sourceURL)
		},
	)
	if err == nil {
		return body, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("fetch global rule: %w", ctxErr)
	}
	if gohttp.IsRetryable(err) {
		return nil, fmt.Errorf("fetch global rule after %d attempts: %w", maxFetchAttempts, err)
	}
	return nil, fmt.Errorf("fetch global rule: %w", err)
}

// fetchOnce performs a single GET. Errors that may clear on their own —
// network failures, 429, 5xx — come back tagged with utils.Retryable;
// everything else is permanent and stops the loop on the first attempt.
func fetchOnce(ctx context.Context, client *http.Client, sourceURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, gohttp.Retryable(fmt.Errorf("request: %w", err))
	}

	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, gohttp.Retryable(fmt.Errorf("read response: %w", readErr))
	}
	if closeErr != nil {
		return nil, gohttp.Retryable(fmt.Errorf("close response: %w", closeErr))
	}

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return body, nil
	}

	statusErr := fmt.Errorf("http %d (%s)", resp.StatusCode, resp.Status)
	if gohttp.IsRetryableStatus(resp.StatusCode) {
		return nil, gohttp.Retryable(statusErr)
	}
	return nil, statusErr
}
