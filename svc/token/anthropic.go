package token

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// anthropicModel is the model identifier sent to the count_tokens
// endpoint. Pinned to the current generally available Sonnet.
const anthropicModel = "claude-sonnet-4-5"

type anthropicReq struct {
	Model    string             `json:"model"`
	Messages []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResp struct {
	InputTokens int `json:"input_tokens"`
}

// anthropicCount calls POST {baseURL}/v1/messages/count_tokens with
// the prompt wrapped in a single user message and returns input_tokens.
// baseURL is read from ANTHROPIC_BASE_URL when set (the official SDK
// treats this as the API root without a trailing path); otherwise
// the default https://api.anthropic.com is used.
func anthropicCount(ctx context.Context, prompt string) (int, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return 0, fmt.Errorf("ANTHROPIC_API_KEY is not set; export it or omit --provider")
	}

	base := os.Getenv("ANTHROPIC_BASE_URL")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	url := base + "/v1/messages/count_tokens"

	body, err := json.Marshal(anthropicReq{
		Model: anthropicModel,
		Messages: []anthropicMessage{{
			Role:    "user",
			Content: prompt,
		}},
	})
	if err != nil {
		return 0, fmt.Errorf("encode request: %w", err)
	}

	status, respBody, err := withRetry(ctx, func(ctx context.Context) (int, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("content-type", "application/json")
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		resp, err := httpClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return resp.StatusCode, b, err
	})
	if err != nil {
		return 0, fmt.Errorf("anthropic count_tokens: %w", err)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return 0, fmt.Errorf("anthropic count_tokens: http %d (check ANTHROPIC_API_KEY)", status)
	}
	if status < 200 || status >= 300 {
		return 0, fmt.Errorf("anthropic count_tokens: http %d: %s", status, trimForErr(respBody))
	}

	var parsed anthropicResp
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return 0, fmt.Errorf("decode anthropic response: %w", err)
	}
	if parsed.InputTokens < 0 {
		return 0, fmt.Errorf("anthropic returned negative token count: %d", parsed.InputTokens)
	}
	return parsed.InputTokens, nil
}
