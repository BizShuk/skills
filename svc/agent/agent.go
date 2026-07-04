// Package agent declares supported agent (skills/agents runtime) targets.
//
// Provider definitions live as one JSON file per agent under ./providers/
// and are embedded into the binary at build time via go:embed. The install
// package consumes Provider values via LoadAll and expands "~/" prefixes
// to the user's home directory on demand (see ExpandHome).
//
// Why JSON + embed instead of Go literals?
//   - Adding a new agent target is a one-file change with no recompile
//     beyond the package itself.
//   - The file is the source of truth — it can be diffed, reviewed,
//     and (if a future --config flag lands) overridden at runtime.
package agent

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed providers
var providers embed.FS

// Type is the unique machine identifier of an agent runtime
// (matches the `type` field in each provider JSON file).
type Type string

// Provider is one row of the embedded agent table. Path fields that begin
// with "~/" are user-relative and must be run through ExpandHome before use.
type Provider struct {
	Type             Type   `json:"type"`
	DisplayName      string `json:"displayName"`
	ProjectSkillsDir string `json:"projectSkillsDir"`
	UserSkillsDir    string `json:"userSkillsDir"`
	ProjectAgentsDir string `json:"projectAgentsDir"`
	UserAgentsDir    string `json:"userAgentsDir"`
	DetectDir        string `json:"detectDir"`
}

// LoadAll reads every JSON file under providers/ and returns the parsed
// Providers sorted by Type for deterministic output. A parse error for
// any file is returned immediately — bad config should fail loud at boot.
func LoadAll() []Provider {
	entries, err := providers.ReadDir("providers")
	if err != nil {
		panic(fmt.Errorf("agent: embedded providers/ unreadable: %w", err))
	}

	var out []Provider
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		f, err := providers.Open("providers/" + e.Name())
		if err != nil {
			panic(fmt.Errorf("agent: open %s: %w", e.Name(), err))
		}
		p, err := parse(f)
		f.Close()
		if err != nil {
			panic(fmt.Errorf("agent: parse %s: %w", e.Name(), err))
		}
		out = append(out, p)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// Find returns the Provider matching t, or false if not present.
func Find(t Type) (Provider, bool) {
	for _, p := range LoadAll() {
		if p.Type == t {
			return p, true
		}
	}
	return Provider{}, false
}

// parse decodes a single Provider JSON document. It rejects documents
// missing the `type` field — a Provider without an identity cannot
// participate in the install selection logic.
func parse(r io.Reader) (Provider, error) {
	var p Provider
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return Provider{}, err
	}
	if p.Type == "" {
		return Provider{}, fmt.Errorf("provider missing required field: type")
	}
	return p, nil
}

// ExpandHome replaces a leading "~/" with the current user's home directory.
// Other paths are returned unchanged. The HOME environment variable is read
// directly (not via go-homedir) to avoid the package-level cache that
// defeats t.Setenv in tests.
func ExpandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Fall back to env so a misconfigured system still produces
		// a deterministic, non-empty result.
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, path[2:])
}