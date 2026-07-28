// Package rule downloads and installs global rule files for agent runtimes.
package rule

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// DefaultURL is the global rule source used when the install command
	// receives no positional URL.
	DefaultURL = "https://raw.githubusercontent.com/BizShuk/cc-plugin/refs/heads/master/config/CLAUDE.global.md"

	maxFetchAttempts = 5
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

	var lastErr error
	for attempt := 1; attempt <= maxFetchAttempts; attempt++ {
		body, retry, err := fetchOnce(ctx, client, sourceURL)
		if err == nil {
			return body, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("fetch global rule: %w", ctxErr)
		}
		if !retry {
			return nil, fmt.Errorf("fetch global rule: %w", err)
		}

		lastErr = err
		if attempt == maxFetchAttempts {
			break
		}
		if err := waitForRetry(ctx, f.RetryDelay); err != nil {
			return nil, fmt.Errorf("fetch global rule: %w", err)
		}
	}

	return nil, fmt.Errorf("fetch global rule after %d attempts: %w", maxFetchAttempts, lastErr)
}

func fetchOnce(ctx context.Context, client *http.Client, sourceURL string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("request: %w", err)
	}

	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, true, fmt.Errorf("read response: %w", readErr)
	}
	if closeErr != nil {
		return nil, true, fmt.Errorf("close response: %w", closeErr)
	}

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return body, false, nil
	}

	statusErr := fmt.Errorf("http %d (%s)", resp.StatusCode, resp.Status)
	retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError
	return nil, retry, statusErr
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
