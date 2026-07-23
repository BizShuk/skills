package token

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bizshuk/skills/svc/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetHTTPClient restores the package-level httpClient to a fresh
// default. Tests use t.Cleanup(resetHTTPClient) so a failure mid-test
// doesn't leak the httptest client into other tests.
func resetHTTPClient() {
	SetHTTPClient(&http.Client{Timeout: 30_000_000_000})
}

// ---------------- localCount ----------------

func TestLocalCountReturnsCeilRunesOverFour(t *testing.T) {
	// 5 runes → ceil(5/4) = 2.
	assert.Equal(t, 2, localCount("hello"))
	assert.Equal(t, 0, localCount(""))
}

func TestLocalCountHandlesMultibyteUTF8(t *testing.T) {
	// 16 Chinese runes → 16/4 = 4 exactly.
	prompt := "你好世界你好世界你好世界你好世界"
	assert.Equal(t, 16, len([]rune(prompt)))
	assert.Equal(t, 4, localCount(prompt))
}

func TestLocalCountReturnsAtLeastOne(t *testing.T) {
	assert.Equal(t, 1, localCount("a"))
	assert.Equal(t, 1, localCount("  ")) // 2 whitespace runes → ceil(2/4) = 1
}

// ---------------- Count dispatch ----------------

func TestCountDispatchesEmptyProviderToLocal(t *testing.T) {
	got, err := Count(context.Background(), "", "hello world")
	require.NoError(t, err)
	assert.Equal(t, localCount("hello world"), got)
}

func TestCountRejectsEmptyPrompt(t *testing.T) {
	_, err := Count(context.Background(), "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty prompt")
}

func TestCountErrorsOnUnknownProvider(t *testing.T) {
	_, err := Count(context.Background(), "no-such-provider", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
	assert.Contains(t, err.Error(), "claude-code")
	assert.Contains(t, err.Error(), "grok")
}

// ---------------- tiktoken path ----------------

func TestCountSucceedsViaTiktokenO200k(t *testing.T) {
	providers := []string{"codex", "grok", "opencode", "hermes-agent", "pi"}
	for _, p := range providers {
		t.Run(p, func(t *testing.T) {
			got, err := Count(context.Background(), p, "hello world")
			require.NoError(t, err)
			assert.Positive(t, got, "tiktoken should produce a positive count for non-empty prompt")
		})
	}
}

// ---------------- dispatch coverage ----------------

func TestDispatchCoversAllProviders(t *testing.T) {
	known := agent.LoadAll()
	for _, p := range known {
		_, ok := dispatch[p.Type]
		assert.Truef(t, ok,
			"provider %q in agent.LoadAll() is missing from token.dispatch; add it in init() or it will silently fall back to local heuristic",
			p.Type)
	}
}

func TestSupportedProvidersListMatchesLoadAll(t *testing.T) {
	got := supportedProviders()
	for _, p := range agent.LoadAll() {
		assert.Containsf(t, got, string(p.Type), "supportedProviders() missing %q", p.Type)
	}
	// Comma-joined sanity check.
	assert.Contains(t, got, ", ")
}

// ---------------- Anthropic API ----------------

func TestAnthropicCountSuccess(t *testing.T) {
	var gotURL, gotAuth, gotVersion string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		gotAuth = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":42}`))
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	SetHTTPClient(srv.Client())
	t.Cleanup(resetHTTPClient)

	n, err := Count(context.Background(), "claude-code", "hi")
	require.NoError(t, err)
	assert.Equal(t, 42, n)
	assert.Equal(t, "/v1/messages/count_tokens", gotURL)
	assert.Equal(t, "sk-test", gotAuth)
	assert.Equal(t, "2023-06-01", gotVersion)
	require.NotNil(t, gotBody["model"])
	assert.Equal(t, "claude-sonnet-4-5", gotBody["model"])
	msgs, ok := gotBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, msgs, 1)
	msg := msgs[0].(map[string]any)
	assert.Equal(t, "user", msg["role"])
	assert.Equal(t, "hi", msg["content"])
}

func TestAnthropicCountMissingAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := Count(context.Background(), "claude-code", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY")
}

func TestAnthropicCount401SurfacesHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	SetHTTPClient(srv.Client())
	t.Cleanup(resetHTTPClient)

	_, err := Count(context.Background(), "claude-code", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check ANTHROPIC_API_KEY")
}

func TestAnthropicCount403SurfacesHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	SetHTTPClient(srv.Client())
	t.Cleanup(resetHTTPClient)

	_, err := Count(context.Background(), "claude-code", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check ANTHROPIC_API_KEY")
}

func TestAnthropicCountRetries5xxThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"input_tokens":7}`))
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	SetHTTPClient(srv.Client())
	t.Cleanup(resetHTTPClient)

	// Linear backoff 200+400=600ms — well within a 5s test budget.
	ctx, cancel := context.WithTimeout(context.Background(), 5*1_000_000_000)
	defer cancel()

	n, err := Count(ctx, "claude-code", "hi")
	require.NoError(t, err)
	assert.Equal(t, 7, n)
	assert.GreaterOrEqual(t, calls.Load(), int32(3))
}

func TestAnthropicCountGivesUpAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	SetHTTPClient(srv.Client())
	t.Cleanup(resetHTTPClient)

	// Backoff sum 200+400+800+1600=3000ms; cap ctx at 10s for slack.
	ctx, cancel := context.WithTimeout(context.Background(), 10*1_000_000_000)
	defer cancel()

	_, err := Count(ctx, "claude-code", "hi")
	require.Error(t, err)
	assert.Equal(t, int32(maxAttempts), calls.Load(), "should hit exactly maxAttempts before giving up")
}

func TestAnthropicCountRespectsContextCancel(t *testing.T) {
	// Server hangs until the request context is done.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)
	SetHTTPClient(srv.Client())
	t.Cleanup(resetHTTPClient)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	_, err := Count(ctx, "claude-code", "hi")
	require.Error(t, err)
}

// ---------------- Gemini API ----------------

func TestGeminiCountSuccess(t *testing.T) {
	var gotKey, gotPath string
	var gotBody geminiReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.URL.Query().Get("key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"totalTokens":17}`))
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "gkey")
	t.Setenv("GOOGLE_API_KEY", "")
	SetHTTPClient(srv.Client())
	t.Cleanup(resetHTTPClient)

	prev := geminiBaseURL
	geminiBaseURL = srv.URL
	t.Cleanup(func() { geminiBaseURL = prev })

	n, err := Count(context.Background(), "antigravity", "hi")
	require.NoError(t, err)
	assert.Equal(t, 17, n)
	assert.Equal(t, "gkey", gotKey)
	assert.Contains(t, gotPath, ":countTokens")
	require.Len(t, gotBody.Contents, 1)
	require.Len(t, gotBody.Contents[0].Parts, 1)
	assert.Equal(t, "hi", gotBody.Contents[0].Parts[0].Text)
}

func TestGeminiCountAcceptsGoogleAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"totalTokens":5}`))
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "google-key")
	SetHTTPClient(srv.Client())
	t.Cleanup(resetHTTPClient)

	prev := geminiBaseURL
	geminiBaseURL = srv.URL
	t.Cleanup(func() { geminiBaseURL = prev })

	n, err := Count(context.Background(), "antigravity", "hi")
	require.NoError(t, err)
	assert.Equal(t, 5, n)
}

func TestGeminiCountMissingAPIKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	_, err := Count(context.Background(), "antigravity", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GEMINI_API_KEY")
	assert.Contains(t, err.Error(), "GOOGLE_API_KEY")
}

func TestGeminiCount401SurfacesHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "k")
	SetHTTPClient(srv.Client())
	t.Cleanup(resetHTTPClient)

	prev := geminiBaseURL
	geminiBaseURL = srv.URL
	t.Cleanup(func() { geminiBaseURL = prev })

	_, err := Count(context.Background(), "antigravity", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GEMINI_API_KEY")
}

func TestGeminiCountRetries429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			http.Error(w, "rate", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"totalTokens":3}`))
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "k")
	SetHTTPClient(srv.Client())
	t.Cleanup(resetHTTPClient)

	prev := geminiBaseURL
	geminiBaseURL = srv.URL
	t.Cleanup(func() { geminiBaseURL = prev })

	ctx, cancel := context.WithTimeout(context.Background(), 5*1_000_000_000)
	defer cancel()

	n, err := Count(ctx, "antigravity", "hi")
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, int32(2), calls.Load())
}

func TestGeminiCountUsesAntigravityCliAlias(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"totalTokens":9}`))
	}))
	defer srv.Close()

	t.Setenv("GEMINI_API_KEY", "k")
	SetHTTPClient(srv.Client())
	t.Cleanup(resetHTTPClient)

	prev := geminiBaseURL
	geminiBaseURL = srv.URL
	t.Cleanup(func() { geminiBaseURL = prev })

	n, err := Count(context.Background(), "antigravity-cli", "hi")
	require.NoError(t, err)
	assert.Equal(t, 9, n)
}
