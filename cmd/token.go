package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/bizshuk/skills/svc/token"
	"github.com/spf13/cobra"
)

// tokenLong is the full `skills token --help` description. It must
// stay self-contained because cobra renders it verbatim — users
// discover the bash usage patterns (cat file into arg, pipe into
// stdin) only by reading this text.
const tokenLong = `Count tokens for a prompt.

Without --provider, returns a fast local estimate of ceil(rune_count / 4)
suitable for rough budgeting. With --provider, the named agent's API
(or its local tokenizer) is used for a precise count.

The prompt is read from the first positional argument. When the argument
is omitted, stdin is read instead. Errors are written to stderr; on
success the integer count is printed alone on stdout so the output can
be piped into shell arithmetic or a budget check.

Examples:

  skills token "hello world"

  skills token "$(cat README.md)"
  skills token "$(< SKILL.md)"

  cat prompt.txt | skills token
  cat prompt.txt | skills token --provider claude-code
  echo "summarize this" | skills token --provider codex

Supported --provider values match every entry in svc/agent/providers/:
claude-code, antigravity, antigravity-cli, codex, grok, opencode,
hermes-agent, pi.

API-backed providers read credentials from environment variables:
  ANTHROPIC_API_KEY      required for --provider claude-code
  GEMINI_API_KEY | GOOGLE_API_KEY
                         required for --provider antigravity,
                         antigravity-cli
  ANTHROPIC_BASE_URL     optional override for claude-code endpoint

The other providers (codex, grok, opencode, hermes-agent, pi) use a
bundled local tokenizer; no API key is required.`

func tokenCmd() *cobra.Command {
	var provider string

	cmd := &cobra.Command{
		Use:   "token [prompt]",
		Short: "Count tokens for a prompt (local estimate or provider API)",
		Long:  tokenLong,
		Args:  cobra.ArbitraryArgs, // 0 or 1; resolvePrompt validates manually
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			prompt, err := resolvePrompt(cmd, args)
			if err != nil {
				return err
			}

			count, err := token.Count(ctx, provider, prompt)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), count)
			return err
		},
	}

	cmd.Flags().StringVarP(&provider, "provider", "p", "",
		"count via named provider's API or local tokenizer "+
			"(claude-code, antigravity, antigravity-cli, codex, grok, opencode, hermes-agent, pi)")
	return cmd
}

// resolvePrompt returns the prompt text from args[0] when present,
// else by draining cmd.InOrStdin(). Empty input is rejected with an
// error so users get a clear message instead of a zero count.
//
// The 0-or-1 positional rule is checked here (not via cobra.Args)
// because we need to react differently to each branch — the zero
// branch must read stdin, which cobra.MaximumNArgs(1) would still
// allow but doesn't help describe.
func resolvePrompt(cmd *cobra.Command, args []string) (string, error) {
	var prompt string
	switch len(args) {
	case 0:
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		prompt = string(data)
	case 1:
		prompt = args[0]
	default:
		return "", fmt.Errorf("accepts at most one positional argument (got %d)", len(args))
	}
	prompt = strings.TrimRight(prompt, "\n")
	if prompt == "" {
		return "", fmt.Errorf("prompt is empty (pass a positional argument or pipe content on stdin)")
	}
	return prompt, nil
}
