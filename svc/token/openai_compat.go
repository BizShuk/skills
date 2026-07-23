package token

import (
	"context"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

// tiktokenCache memoizes encodings so each token command pays the
// upfront BPE-table cost exactly once per process. tiktoken-go loads
// the BPE tables from embedded assets on first use (~1s cold start).
var (
	o200kOnce sync.Once
	o200kEnc  *tiktoken.Tiktoken
	o200kErr  error

	cl100kOnce sync.Once
	cl100kEnc  *tiktoken.Tiktoken
	cl100kErr  error
)

// tiktokenO200k counts tokens using OpenAI's o200k_base encoding
// (GPT-4o / GPT-4.1 / Grok-2+). Falls back to cl100k_base if the
// o200k tables aren't reachable (offline build env or stripped
// assets). No HTTP call, no API key.
func tiktokenO200k(ctx context.Context, prompt string) (int, error) {
	enc, err := getO200k()
	if err != nil {
		enc, err = getCl100k()
		if err != nil {
			return 0, err
		}
	}
	return len(enc.Encode(prompt, nil, nil)), nil
}

func getO200k() (*tiktoken.Tiktoken, error) {
	o200kOnce.Do(func() {
		o200kEnc, o200kErr = tiktoken.GetEncoding("o200k_base")
	})
	return o200kEnc, o200kErr
}

func getCl100k() (*tiktoken.Tiktoken, error) {
	cl100kOnce.Do(func() {
		cl100kEnc, cl100kErr = tiktoken.GetEncoding("cl100k_base")
	})
	return cl100kEnc, cl100kErr
}
