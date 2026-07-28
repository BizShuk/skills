package rule

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetcherReturnsExactResponseBytes(t *testing.T) {
	want := []byte("# Global Rule\n\n- 保留 bytes\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		_, err := w.Write(want)
		assert.NoError(t, err)
	}))
	defer server.Close()

	got, err := (Fetcher{Client: server.Client()}).Fetch(context.Background(), server.URL)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestFetcherDoesNotRetryNonTransientHTTPStatus(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := (Fetcher{Client: server.Client()}).Fetch(context.Background(), server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http 400")
	assert.Equal(t, int32(1), attempts.Load())
}

func TestFetcherRetriesTransientHTTPStatus(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := attempts.Add(1)
		if attempt < 3 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		_, err := w.Write([]byte("ready"))
		assert.NoError(t, err)
	}))
	defer server.Close()

	got, err := (Fetcher{Client: server.Client()}).Fetch(context.Background(), server.URL)
	require.NoError(t, err)
	assert.Equal(t, []byte("ready"), got)
	assert.Equal(t, int32(3), attempts.Load())
}

func TestFetcherStopsAfterFiveAttempts(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := (Fetcher{Client: server.Client()}).Fetch(context.Background(), server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after 5 attempts")
	assert.Equal(t, int32(5), attempts.Load())
}

func TestFetcherHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (Fetcher{Client: http.DefaultClient}).Fetch(ctx, "https://example.invalid/rules.md")
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestDefaultURL(t *testing.T) {
	assert.Equal(t,
		"https://raw.githubusercontent.com/BizShuk/cc-plugin/refs/heads/master/config/CLAUDE.global.md",
		DefaultURL,
	)
}
