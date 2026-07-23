package token

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

const geminiModel = "gemini-2.0-flash"

// geminiBaseURL is the API root for Gemini's countTokens endpoint.
// Overridable for tests so httptest.NewServer can stand in for the
// real network. Default points at Google's public endpoint.
var geminiBaseURL = "https://generativelanguage.googleapis.com"

type geminiReq struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResp struct {
	TotalTokens int `json:"totalTokens"`
}

// geminiCount calls POST {base}/v1beta/models/{model}:countTokens?key=...
// for both antigravity and antigravity-cli. API key may be set via
// GEMINI_API_KEY (project convention) or GOOGLE_API_KEY (Google's
// standard); GEMINI_API_KEY wins when both are set.
func geminiCount(ctx context.Context, prompt string) (int, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		return 0, fmt.Errorf("GEMINI_API_KEY (or GOOGLE_API_KEY) is not set; export it or omit --provider")
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:countTokens", geminiBaseURL, geminiModel)
	u, err := url.Parse(endpoint)
	if err != nil {
		return 0, fmt.Errorf("parse gemini endpoint: %w", err)
	}
	q := u.Query()
	q.Set("key", apiKey)
	u.RawQuery = q.Encode()

	body, err := json.Marshal(geminiReq{
		Contents: []geminiContent{{
			Parts: []geminiPart{{Text: prompt}},
		}},
	})
	if err != nil {
		return 0, fmt.Errorf("encode request: %w", err)
	}

	status, respBody, err := withRetry(ctx, func(ctx context.Context) (int, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return resp.StatusCode, b, err
	})
	if err != nil {
		return 0, fmt.Errorf("gemini countTokens: %w", err)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return 0, fmt.Errorf("gemini countTokens: http %d (check GEMINI_API_KEY / GOOGLE_API_KEY)", status)
	}
	if status < 200 || status >= 300 {
		return 0, fmt.Errorf("gemini countTokens: http %d: %s", status, trimForErr(respBody))
	}

	var parsed geminiResp
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return 0, fmt.Errorf("decode gemini response: %w", err)
	}
	if parsed.TotalTokens < 0 {
		return 0, fmt.Errorf("gemini returned negative token count: %d", parsed.TotalTokens)
	}
	return parsed.TotalTokens, nil
}
