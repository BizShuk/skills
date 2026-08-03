# `skills add` Go 版 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 以 Go 重寫 `skills add [path]`，遞迴（深度 3、並行）發現 marketplace/plugin 中的 skills，用 TUI 選取後安裝到目標 agents 的 project 或 user 目錄。

**Architecture:** cobra 單一命令，`svc/` 下六個單一職責 package（source→manifest→fetch→discover→install→tui），以介面解耦、逐一 TDD。走訪為 BFS，remote plugin 是唯一遞迴節點，errgroup 並行，失敗只標記不中斷。

**Tech Stack:** Go 1.22+、cobra、bubbletea/bubbles、golang.org/x/sync/errgroup、log/slog、testify。Module `github.com/bizshuk/skills`（repo 根目錄，`golang` branch）。

參考規格：`docs/superpowers/specs/2026-07-04-skills-add-golang-design.md`

---

## File Structure

```tree
/ (module root)
├── go.mod                       # module github.com/bizshuk/skills
├── cmd/skills/main.go           # cobra root + add 指令接線
└── svc/
    ├── source/source.go         # Parse(s) → ParsedSource
    ├── source/source_test.go
    ├── manifest/types.go        # ParsedSource 之外的 manifest 型別
    ├── manifest/manifest.go     # Marketplace/Plugin 解析、ResolvePluginSource、掃 skills
    ├── manifest/manifest_test.go
    ├── fetch/fetch.go           # Fetcher 介面 + local/github 實作
    ├── fetch/fetch_test.go
    ├── discover/discover.go     # Walk() BFS → Catalog
    ├── discover/discover_test.go
    ├── install/agents.go        # agents 安裝位置表 + 偵測
    ├── install/install.go       # Apply() 複製
    ├── install/install_test.go
    └── tui/tui.go               # bubbletea model + Run()
```

每個 package 一個責任；型別集中在各 package 的 `types.go` 或主檔頂部，跨 package 共用型別放在其擁有者 package（如 `ParsedSource` 屬 `source`）。

---

## Task 0: Module bootstrap

**Files:**
- Create: `go.mod`
- Create: `cmd/skills/main.go`

- [ ] **Step 1: 初始化 module 並加依賴**

Run:
```bash
cd /Users/shuk/projects/tmp/skills
go mod init github.com/bizshuk/skills
go get github.com/spf13/cobra@latest
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/bubbles@latest
go get golang.org/x/sync/errgroup@latest
go get github.com/stretchr/testify@latest
```
Expected: `go.mod` 生成、無錯誤。

- [ ] **Step 2: 寫最小 cobra root（add 指令暫時只印參數）**

Create `cmd/skills/main.go`:
```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{Use: "skills", SilenceUsage: true}
	var global bool
	var agents []string
	var depth int
	var yes bool

	add := &cobra.Command{
		Use:   "add [path]",
		Short: "Discover and install skills from a source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "add source=%q global=%v agents=%v depth=%d yes=%v\n",
				args[0], global, agents, depth, yes)
			return nil
		},
	}
	add.Flags().BoolVar(&global, "global", false, "install into user-level dirs")
	add.Flags().StringSliceVar(&agents, "agent", nil, "override detected target agents")
	add.Flags().IntVar(&depth, "depth", 3, "max recursion depth")
	add.Flags().BoolVar(&yes, "yes", false, "skip TUI, install all detected")
	root.AddCommand(add)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 3: 驗證 build 與執行**

Run: `go build ./... && go run ./cmd/skills add owner/repo`
Expected: 印出 `add source="owner/repo" global=false agents=[] depth=3 yes=false`

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum cmd/skills/main.go
git commit -m "feat: bootstrap go module and cobra add skeleton"
```

---

## Task 1: source.Parse — 來源解析

**Files:**
- Create: `svc/source/source.go`
- Test: `svc/source/source_test.go`

移植自 TS `src/source-parser.ts`。本任務只做 `Parse` 與其型別。

- [ ] **Step 1: 寫失敗測試**

Create `svc/source/source_test.go`:
```go
package source

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	abs, _ := filepath.Abs("./local/dir")

	tests := []struct {
		name string
		in   string
		want ParsedSource
	}{
		{"shorthand", "owner/repo", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git"}},
		{"shorthand-subpath", "owner/repo/skills/foo", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git", Subpath: "skills/foo"}},
		{"at-skill", "owner/repo@web-design", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git", SkillFilter: "web-design"}},
		{"github-tree", "https://github.com/owner/repo/tree/main/skills/x", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git", Ref: "main", Subpath: "skills/x"}},
		{"github-url", "https://github.com/owner/repo", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git"}},
		{"github-prefix", "github:owner/repo", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git"}},
		{"gitlab-url", "https://gitlab.com/group/sub/repo", ParsedSource{Type: GitLab, URL: "https://gitlab.com/group/sub/repo.git"}},
		{"fragment-ref", "owner/repo#v2", ParsedSource{Type: GitHub, URL: "https://github.com/owner/repo.git", Ref: "v2"}},
		{"wellknown", "https://example.com/team", ParsedSource{Type: WellKnown, URL: "https://example.com/team"}},
		{"local-rel", "./local/dir", ParsedSource{Type: Local, URL: abs, LocalPath: abs}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseSubpathTraversalRejected(t *testing.T) {
	_, err := Parse("owner/repo/../../etc")
	assert.Error(t, err)
}
```

- [ ] **Step 2: 執行確認失敗**

Run: `go test ./svc/source/`
Expected: FAIL（`Parse`、`ParsedSource` 未定義）

- [ ] **Step 3: 實作 source.go**

Create `svc/source/source.go`:
```go
// Package source parses a user-supplied source string (owner/repo,
// git URL, local path, well-known URL) into a normalized ParsedSource.
package source

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

type SourceType int

const (
	Local SourceType = iota
	GitHub
	GitLab
	Git
	WellKnown
)

type ParsedSource struct {
	Type        SourceType
	URL         string
	Ref         string
	Subpath     string
	SkillFilter string
	LocalPath   string
}

var (
	reGitHubTreePath = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/tree/([^/]+)/(.+)`)
	reGitHubTree     = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/tree/([^/]+)$`)
	reGitHubRepo     = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`)
	reGitLabRepo     = regexp.MustCompile(`gitlab\.com/(.+?)(?:\.git)?/?$`)
	reAtSkill        = regexp.MustCompile(`^([^/]+)/([^/@]+)@(.+)$`)
	reShorthand      = regexp.MustCompile(`^([^/]+)/([^/]+)(?:/(.+?))?/?$`)
)

// sanitizeSubpath rejects any ".." segment that could escape the repo root.
func sanitizeSubpath(sub string) (string, error) {
	norm := strings.ReplaceAll(sub, `\`, "/")
	for _, seg := range strings.Split(norm, "/") {
		if seg == ".." {
			return "", fmt.Errorf("unsafe subpath %q: contains ..", sub)
		}
	}
	return sub, nil
}

func isLocalPath(in string) bool {
	return filepath.IsAbs(in) ||
		strings.HasPrefix(in, "./") ||
		strings.HasPrefix(in, "../") ||
		in == "." || in == ".."
}

// splitFragment extracts a trailing "#ref" or "#ref@skill" from git-like inputs.
func splitFragment(in string) (base, ref, skill string) {
	i := strings.Index(in, "#")
	if i < 0 {
		return in, "", ""
	}
	base = in[:i]
	frag := in[i+1:]
	if j := strings.Index(frag, "@"); j >= 0 {
		return base, frag[:j], frag[j+1:]
	}
	return base, frag, ""
}

func Parse(in string) (ParsedSource, error) {
	if isLocalPath(in) {
		abs, err := filepath.Abs(in)
		if err != nil {
			return ParsedSource{}, err
		}
		return ParsedSource{Type: Local, URL: abs, LocalPath: abs}, nil
	}

	base, fragRef, fragSkill := splitFragment(in)

	if strings.HasPrefix(base, "github:") {
		return Parse(reattach(strings.TrimPrefix(base, "github:"), fragRef, fragSkill))
	}
	if strings.HasPrefix(base, "gitlab:") {
		return Parse(reattach("https://gitlab.com/"+strings.TrimPrefix(base, "gitlab:"), fragRef, fragSkill))
	}

	if m := reGitHubTreePath.FindStringSubmatch(base); m != nil {
		sub, err := sanitizeSubpath(m[4])
		if err != nil {
			return ParsedSource{}, err
		}
		return ParsedSource{Type: GitHub, URL: ghURL(m[1], m[2]), Ref: m[3], Subpath: sub}, nil
	}
	if m := reGitHubTree.FindStringSubmatch(base); m != nil {
		return ParsedSource{Type: GitHub, URL: ghURL(m[1], m[2]), Ref: m[3]}, nil
	}
	if m := reGitHubRepo.FindStringSubmatch(base); m != nil {
		return ParsedSource{Type: GitHub, URL: ghURL(m[1], strings.TrimSuffix(m[2], ".git")), Ref: fragRef}, nil
	}
	if m := reGitLabRepo.FindStringSubmatch(base); m != nil && strings.Contains(m[1], "/") {
		return ParsedSource{Type: GitLab, URL: "https://gitlab.com/" + m[1] + ".git", Ref: fragRef}, nil
	}

	if !strings.Contains(base, ":") && !strings.HasPrefix(base, ".") && !strings.HasPrefix(base, "/") {
		if m := reAtSkill.FindStringSubmatch(base); m != nil {
			skill := m[3]
			if fragSkill != "" {
				skill = fragSkill
			}
			return ParsedSource{Type: GitHub, URL: ghURL(m[1], m[2]), Ref: fragRef, SkillFilter: skill}, nil
		}
		if m := reShorthand.FindStringSubmatch(base); m != nil {
			var sub string
			if m[3] != "" {
				s, err := sanitizeSubpath(m[3])
				if err != nil {
					return ParsedSource{}, err
				}
				sub = s
			}
			return ParsedSource{Type: GitHub, URL: ghURL(m[1], m[2]), Ref: fragRef, Subpath: sub, SkillFilter: fragSkill}, nil
		}
	}

	if isWellKnownURL(base) {
		return ParsedSource{Type: WellKnown, URL: base}, nil
	}

	return ParsedSource{Type: Git, URL: base, Ref: fragRef}, nil
}

func ghURL(owner, repo string) string {
	return "https://github.com/" + owner + "/" + strings.TrimSuffix(repo, ".git") + ".git"
}

func reattach(base, ref, skill string) string {
	if ref == "" {
		return base
	}
	if skill != "" {
		return base + "#" + ref + "@" + skill
	}
	return base + "#" + ref
}

func isWellKnownURL(in string) bool {
	if !strings.HasPrefix(in, "http://") && !strings.HasPrefix(in, "https://") {
		return false
	}
	u, err := url.Parse(in)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "github.com", "gitlab.com", "raw.githubusercontent.com":
		return false
	}
	return !strings.HasSuffix(in, ".git")
}
```

- [ ] **Step 4: 執行確認通過**

Run: `go test ./svc/source/ -v`
Expected: PASS（全部子測試）

- [ ] **Step 5: Commit**

```bash
git add svc/source/
git commit -m "feat: source string parser"
```

---

## Task 2: manifest — plugin/marketplace 解析與 skill 掃描

**Files:**
- Create: `svc/manifest/types.go`
- Create: `svc/manifest/manifest.go`
- Test: `svc/manifest/manifest_test.go`

移植 TS `plugin-manifest.ts` 的 `resolvePluginSource` / `getPluginGroupings` 精華。

- [ ] **Step 1: 定義型別**

Create `svc/manifest/types.go`:
```go
package manifest

// Skill 是一個含 SKILL.md 的目錄。
type Skill struct {
	Name string
	Path string // 絕對路徑
}

// LocalPlugin：source 指向本 repo 內的 plugin（或 fallback 到 repo 本身）。
type LocalPlugin struct {
	Name   string
	Base   string // plugin 目錄絕對路徑
	Skills []Skill
}

// RemotePlugin：source 指向外部 repo，交由 discover 遞迴。
type RemotePlugin struct {
	Name      string
	OwnerRepo string
	URL       string
	Ref       string
	Subdir    string
}

// Parsed：一個 basePath 解析後的本地與遠端 plugin。
type Parsed struct {
	Locals  []LocalPlugin
	Remotes []RemotePlugin
}
```

- [ ] **Step 2: 寫失敗測試**

Create `svc/manifest/manifest_test.go`:
```go
package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestScanMarketplaceLocalAndRemote(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude-plugin/marketplace.json"), `{
      "metadata": {"pluginRoot": "./plugins"},
      "plugins": [
        {"name": "docs", "source": "./docs-plugin"},
        {"name": "remote-one", "source": {"source": "github", "repo": "acme/widgets"}}
      ]
    }`)
	// local plugin with one conventional skill
	writeFile(t, filepath.Join(root, "plugins/docs-plugin/skills/writer/SKILL.md"), "# writer")

	got, err := Scan(root)
	require.NoError(t, err)

	require.Len(t, got.Locals, 1)
	assert.Equal(t, "docs", got.Locals[0].Name)
	require.Len(t, got.Locals[0].Skills, 1)
	assert.Equal(t, "writer", got.Locals[0].Skills[0].Name)

	require.Len(t, got.Remotes, 1)
	assert.Equal(t, "remote-one", got.Remotes[0].Name)
	assert.Equal(t, "acme/widgets", got.Remotes[0].OwnerRepo)
}

func TestScanPluginJsonExtraSkillsAdditive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude-plugin/plugin.json"), `{
      "name": "solo", "skills": ["./extra/special"]
    }`)
	writeFile(t, filepath.Join(root, "skills/conventional/SKILL.md"), "# c")
	writeFile(t, filepath.Join(root, "extra/special/SKILL.md"), "# s")

	got, err := Scan(root)
	require.NoError(t, err)
	require.Len(t, got.Locals, 1)
	names := []string{}
	for _, s := range got.Locals[0].Skills {
		names = append(names, s.Name)
	}
	assert.ElementsMatch(t, []string{"conventional", "special"}, names)
}

func TestScanTraversalRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude-plugin/plugin.json"), `{
      "name": "bad", "skills": ["../../etc/passwd"]
    }`)
	got, err := Scan(root)
	require.NoError(t, err)
	require.Len(t, got.Locals, 1)
	assert.Empty(t, got.Locals[0].Skills)
}
```

- [ ] **Step 3: 執行確認失敗**

Run: `go test ./svc/manifest/`
Expected: FAIL（`Scan` 未定義）

- [ ] **Step 4: 實作 manifest.go**

Create `svc/manifest/manifest.go`:
```go
// Package manifest reads .claude-plugin/{marketplace,plugin}.json and scans
// each plugin's skills/ directory, classifying plugins as local or remote.
package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type marketplaceFile struct {
	Metadata struct {
		PluginRoot string `json:"pluginRoot"`
	} `json:"metadata"`
	Plugins []pluginEntry `json:"plugins"`
}

type pluginEntry struct {
	Name   string          `json:"name"`
	Source json.RawMessage `json:"source"`
	Skills []string        `json:"skills"`
}

type pluginFile struct {
	Name   string   `json:"name"`
	Skills []string `json:"skills"`
}

type objectSource struct {
	Source string `json:"source"`
	Repo   string `json:"repo"`
	URL    string `json:"url"`
	Path   string `json:"path"`
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
}

// isValidRel enforces the Claude Code convention that manifest paths begin "./".
func isValidRel(p string) bool { return strings.HasPrefix(p, "./") }

// contained reports whether target stays within base after resolving.
func contained(target, base string) bool {
	ab, _ := filepath.Abs(base)
	at, _ := filepath.Abs(target)
	return at == ab || strings.HasPrefix(at, ab+string(filepath.Separator))
}

func Scan(base string) (Parsed, error) {
	var out Parsed

	if data, err := os.ReadFile(filepath.Join(base, ".claude-plugin/marketplace.json")); err == nil {
		var mf marketplaceFile
		if json.Unmarshal(data, &mf) == nil {
			root := mf.Metadata.PluginRoot
			if root == "" || isValidRel(root) {
				for _, p := range mf.Plugins {
					scanEntry(base, root, p, &out)
				}
			}
		}
	}

	if data, err := os.ReadFile(filepath.Join(base, ".claude-plugin/plugin.json")); err == nil {
		var pf pluginFile
		if json.Unmarshal(data, &pf) == nil && pf.Name != "" {
			lp := LocalPlugin{Name: pf.Name, Base: base}
			lp.Skills = collectSkills(base, base, pf.Skills)
			out.Locals = append(out.Locals, lp)
		}
	}

	return out, nil
}

func scanEntry(base, root string, p pluginEntry, out *Parsed) {
	if p.Name == "" {
		return
	}
	// remote: object source
	if len(p.Source) > 0 && p.Source[0] == '{' {
		var os_ objectSource
		if json.Unmarshal(p.Source, &os_) != nil {
			return
		}
		if r := resolveRemote(os_); r != nil {
			r.Name = p.Name
			out.Remotes = append(out.Remotes, *r)
		}
		return
	}
	// local: string source or absent
	var srcStr string
	if len(p.Source) > 0 {
		_ = json.Unmarshal(p.Source, &srcStr)
		if srcStr != "" && !isValidRel(srcStr) {
			return
		}
	}
	pluginBase := filepath.Join(base, root, srcStr)
	if !contained(pluginBase, base) {
		return
	}
	lp := LocalPlugin{Name: p.Name, Base: pluginBase}
	lp.Skills = collectSkills(base, pluginBase, p.Skills)
	out.Locals = append(out.Locals, lp)
}

func resolveRemote(s objectSource) *RemotePlugin {
	switch s.Source {
	case "github", "":
		if !strings.Contains(s.Repo, "/") {
			return nil
		}
		return &RemotePlugin{OwnerRepo: strings.ToLower(s.Repo), Ref: s.Ref}
	case "url":
		or := ownerRepoFromURL(s.URL)
		if or == "" {
			return nil
		}
		return &RemotePlugin{OwnerRepo: or, URL: s.URL, Ref: s.Ref}
	case "git-subdir":
		or := ownerRepoFromURL(s.URL)
		if or == "" || s.Path == "" {
			return nil
		}
		return &RemotePlugin{OwnerRepo: or, URL: s.URL, Subdir: s.Path, Ref: s.Ref}
	}
	return nil
}

// ownerRepoFromURL extracts "owner/repo" from an http(s) or git URL.
func ownerRepoFromURL(raw string) string {
	raw = strings.TrimSuffix(raw, ".git")
	i := strings.Index(raw, "://")
	if i >= 0 {
		raw = raw[i+3:]
	}
	if at := strings.Index(raw, "@"); at >= 0 && !strings.Contains(raw[:at], "/") {
		raw = raw[at+1:]
	}
	raw = strings.ReplaceAll(raw, ":", "/")
	parts := strings.Split(raw, "/")
	if len(parts) < 3 {
		return ""
	}
	return strings.ToLower(parts[len(parts)-2] + "/" + parts[len(parts)-1])
}

// collectSkills scans pluginBase/skills/* plus additive manifest skill paths.
func collectSkills(base, pluginBase string, extra []string) []Skill {
	seen := map[string]bool{}
	var skills []Skill

	add := func(dir string) {
		if !contained(dir, base) {
			return
		}
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
			return
		}
		abs, _ := filepath.Abs(dir)
		if seen[abs] {
			return
		}
		seen[abs] = true
		skills = append(skills, Skill{Name: filepath.Base(dir), Path: abs})
	}

	// conventional skills/ directory
	entries, _ := os.ReadDir(filepath.Join(pluginBase, "skills"))
	for _, e := range entries {
		if e.IsDir() {
			add(filepath.Join(pluginBase, "skills", e.Name()))
		}
	}
	// additive explicit skill paths
	for _, sp := range extra {
		if !isValidRel(sp) {
			continue
		}
		add(filepath.Join(pluginBase, sp))
	}
	return skills
}
```

- [ ] **Step 5: 執行確認通過**

Run: `go test ./svc/manifest/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add svc/manifest/
git commit -m "feat: manifest scanning for local and remote plugins"
```

---

## Task 3: fetch — Fetcher 介面與實作

**Files:**
- Create: `svc/fetch/fetch.go`
- Test: `svc/fetch/fetch_test.go`

- [ ] **Step 1: 寫失敗測試**

Create `svc/fetch/fetch_test.go`:
```go
package fetch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bizshuk/skills/svc/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalFetcherReturnsPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# x"), 0o644))

	f := New()
	got, err := f.Materialize(context.Background(), source.ParsedSource{Type: source.Local, LocalPath: dir})
	require.NoError(t, err)
	assert.Equal(t, dir, got)
}

func TestLocalFetcherMissingPath(t *testing.T) {
	f := New()
	_, err := f.Materialize(context.Background(), source.ParsedSource{Type: source.Local, LocalPath: "/no/such/dir"})
	assert.Error(t, err)
}
```

- [ ] **Step 2: 執行確認失敗**

Run: `go test ./svc/fetch/`
Expected: FAIL（`New`、`Materialize` 未定義）

- [ ] **Step 3: 實作 fetch.go**

Create `svc/fetch/fetch.go`:
```go
// Package fetch turns a ParsedSource into a local directory on disk.
// Local sources return their path directly; remote sources download a
// GitHub tarball into a temp dir. Transient network errors retry up to 5x.
package fetch

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bizshuk/skills/svc/source"
)

const maxRetries = 5

type Fetcher interface {
	Materialize(ctx context.Context, s source.ParsedSource) (string, error)
}

type httpFetcher struct{ client *http.Client }

func New() Fetcher { return &httpFetcher{client: &http.Client{Timeout: 30 * time.Second}} }

func (f *httpFetcher) Materialize(ctx context.Context, s source.ParsedSource) (string, error) {
	if s.Type == source.Local {
		if _, err := os.Stat(s.LocalPath); err != nil {
			return "", fmt.Errorf("local path not found: %s", s.LocalPath)
		}
		return s.LocalPath, nil
	}
	return f.fetchGitHubTarball(ctx, s)
}

// fetchGitHubTarball downloads codeload tarball for a github.com/owner/repo URL.
func (f *httpFetcher) fetchGitHubTarball(ctx context.Context, s source.ParsedSource) (string, error) {
	owner, repo, ok := ownerRepo(s.URL)
	if !ok {
		return "", fmt.Errorf("unsupported remote source: %s", s.URL)
	}
	ref := s.Ref
	if ref == "" {
		ref = "HEAD"
	}
	url := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", owner, repo, ref)

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		dir, err := f.downloadAndExtract(ctx, url)
		if err == nil {
			return dir, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("unable to fetch %s/%s: %w", owner, repo, lastErr)
}

func ownerRepo(gitURL string) (string, string, bool) {
	raw := strings.TrimSuffix(gitURL, ".git")
	i := strings.Index(raw, "github.com/")
	if i < 0 {
		return "", "", false
	}
	parts := strings.Split(raw[i+len("github.com/"):], "/")
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (f *httpFetcher) downloadAndExtract(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}

	dest, err := os.MkdirTemp("", "skills-fetch-*")
	if err != nil {
		return "", err
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		// strip the leading "<repo>-<ref>/" component
		rel := stripFirstComponent(hdr.Name)
		if rel == "" {
			continue
		}
		target := filepath.Join(dest, rel)
		if !strings.HasPrefix(target, dest+string(filepath.Separator)) {
			continue // traversal guard
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			out, err := os.Create(target)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return "", err
			}
			out.Close()
		}
	}
	return dest, nil
}

func stripFirstComponent(name string) string {
	i := strings.Index(name, "/")
	if i < 0 {
		return ""
	}
	return name[i+1:]
}
```

- [ ] **Step 4: 執行確認通過**

Run: `go test ./svc/fetch/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add svc/fetch/
git commit -m "feat: fetcher for local paths and github tarballs"
```

---

## Task 4: discover — 遞迴 BFS walker

**Files:**
- Create: `svc/discover/discover.go`
- Test: `svc/discover/discover_test.go`

- [ ] **Step 1: 寫失敗測試（用 fake Fetcher）**

Create `svc/discover/discover_test.go`:
```go
package discover

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bizshuk/skills/svc/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFetcher maps ownerRepo → a prepared local dir; unknown → error.
type fakeFetcher struct{ repos map[string]string }

func (f fakeFetcher) Materialize(_ context.Context, s source.ParsedSource) (string, error) {
	if s.Type == source.Local {
		return s.LocalPath, nil
	}
	for or, dir := range f.repos {
		if strings.Contains(s.URL, or) {
			return dir, nil
		}
	}
	return "", os.ErrNotExist
}

func mkMarketplace(t *testing.T, root, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".claude-plugin/marketplace.json"), []byte(body), 0o644))
}
func mkSkill(t *testing.T, base, plugin, skill string) {
	t.Helper()
	dir := filepath.Join(base, plugin, "skills", skill)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+skill), 0o644))
}

func TestWalkLocalOnly(t *testing.T) {
	root := t.TempDir()
	mkMarketplace(t, root, `{"metadata":{"pluginRoot":"./p"},"plugins":[{"name":"docs","source":"./d"}]}`)
	mkSkill(t, filepath.Join(root, "p"), "d", "writer")

	cat, err := Walk(context.Background(), fakeFetcher{}, source.ParsedSource{Type: source.Local, LocalPath: root}, 3)
	require.NoError(t, err)
	require.Len(t, cat, 1)
	assert.Equal(t, "docs", cat[0].PluginName)
	assert.True(t, cat[0].FetchOK)
	require.Len(t, cat[0].Skills, 1)
}

func TestWalkRemoteUnreachableMarkedUnableToFetch(t *testing.T) {
	root := t.TempDir()
	mkMarketplace(t, root, `{"plugins":[{"name":"remote","source":{"source":"github","repo":"acme/missing"}}]}`)

	cat, err := Walk(context.Background(), fakeFetcher{repos: map[string]string{}}, source.ParsedSource{Type: source.Local, LocalPath: root}, 3)
	require.NoError(t, err)
	require.Len(t, cat, 1)
	assert.False(t, cat[0].FetchOK)
	assert.NotEmpty(t, cat[0].FetchErr)
}

func TestWalkDepthLimitStops(t *testing.T) {
	// remote repo whose marketplace points at another remote; depth=1 must not
	// descend past the first remote hop.
	inner := t.TempDir()
	mkMarketplace(t, inner, `{"plugins":[{"name":"deep","source":{"source":"github","repo":"acme/deeper"}}]}`)

	root := t.TempDir()
	mkMarketplace(t, root, `{"plugins":[{"name":"lvl1","source":{"source":"github","repo":"acme/inner"}}]}`)

	ff := fakeFetcher{repos: map[string]string{"acme/inner": inner}}
	cat, err := Walk(context.Background(), ff, source.ParsedSource{Type: source.Local, LocalPath: root}, 1)
	require.NoError(t, err)
	// lvl1 fetched (depth 1). Its child "deep" is at depth 2 > 1, so it is not walked;
	// deep appears as a not-yet-fetched remote category placeholder.
	names := map[string]bool{}
	for _, c := range cat {
		names[c.PluginName] = true
	}
	assert.True(t, names["lvl1"])
}
```

- [ ] **Step 2: 執行確認失敗**

Run: `go test ./svc/discover/`
Expected: FAIL（`Walk` 未定義）

- [ ] **Step 3: 實作 discover.go**

Create `svc/discover/discover.go`:
```go
// Package discover walks a source breadth-first, turning every plugin into a
// Category. Remote plugins are the only nodes that recurse; they are fetched
// in parallel per level, guarded by a visited set and a max depth.
package discover

import (
	"context"
	"fmt"
	"sync"

	"github.com/bizshuk/skills/svc/fetch"
	"github.com/bizshuk/skills/svc/manifest"
	"github.com/bizshuk/skills/svc/source"
	"golang.org/x/sync/errgroup"
)

type Skill struct {
	Name string
	Path string
}

type Category struct {
	PluginName string
	OwnerRepo  string
	Skills     []Skill
	FetchOK    bool
	FetchErr   string
}

type Catalog []Category

// node is one unit of work in the BFS queue.
type node struct {
	dir   string // already-materialized base dir
	depth int
}

func Walk(ctx context.Context, f fetch.Fetcher, root source.ParsedSource, maxDepth int) (Catalog, error) {
	rootDir, err := f.Materialize(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("materialize root: %w", err)
	}

	var (
		mu       sync.Mutex
		cat      Catalog
		visited  = map[string]bool{}
		queue    = []node{{dir: rootDir, depth: 0}}
	)

	for len(queue) > 0 {
		level := queue
		queue = nil

		var nextMu sync.Mutex
		g, gctx := errgroup.WithContext(ctx)

		for _, n := range level {
			n := n
			g.Go(func() error {
				parsed, err := manifest.Scan(n.dir)
				if err != nil {
					return nil // malformed → skip silently
				}
				// local plugins become categories immediately
				for _, lp := range parsed.Locals {
					c := Category{PluginName: lp.Name, FetchOK: true}
					for _, s := range lp.Skills {
						c.Skills = append(c.Skills, Skill{Name: s.Name, Path: s.Path})
					}
					mu.Lock()
					cat = append(cat, c)
					mu.Unlock()
				}
				// remote plugins: fetch + recurse (if depth allows)
				for _, rp := range parsed.Remotes {
					rp := rp
					mu.Lock()
					seen := visited[rp.OwnerRepo]
					if !seen {
						visited[rp.OwnerRepo] = true
					}
					mu.Unlock()
					if seen {
						continue
					}

					if n.depth+1 > maxDepth {
						mu.Lock()
						cat = append(cat, Category{PluginName: rp.Name, OwnerRepo: rp.OwnerRepo, FetchOK: false, FetchErr: "depth limit reached"})
						mu.Unlock()
						continue
					}

					src := source.ParsedSource{Type: source.GitHub, URL: "https://github.com/" + rp.OwnerRepo + ".git", Ref: rp.Ref}
					if rp.URL != "" {
						src.URL = rp.URL
					}
					dir, ferr := f.Materialize(gctx, src)
					if ferr != nil {
						mu.Lock()
						cat = append(cat, Category{PluginName: rp.Name, OwnerRepo: rp.OwnerRepo, FetchOK: false, FetchErr: "unable to fetch"})
						mu.Unlock()
						continue
					}
					nextMu.Lock()
					queue = append(queue, node{dir: dir, depth: n.depth + 1})
					nextMu.Unlock()
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return cat, err
		}
	}

	return cat, nil
}
```

- [ ] **Step 4: 執行確認通過**

Run: `go test ./svc/discover/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add svc/discover/
git commit -m "feat: recursive bfs discovery walker"
```

---

## Task 5: install — agents 表、偵測與複製

**Files:**
- Create: `svc/install/agents.go`
- Create: `svc/install/install.go`
- Test: `svc/install/install_test.go`

- [ ] **Step 1: 定義 agents 表**

Create `svc/install/agents.go`:
```go
// Package install holds the agent install-location table and copies selected
// skills/subagents into an agent's project- or user-level directories.
package install

import (
	"os"
	"path/filepath"
)

type AgentType string

type Agent struct {
	Type             AgentType
	ProjectSkillsDir string // relative to cwd
	UserSkillsDir    string // absolute
	ProjectAgentsDir string
	UserAgentsDir    string
	// DetectDir existing → treat agent as installed
	DetectDir string
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// Agents lists the six supported targets. Non-claude-code agents dirs are
// inferred (see spec §Install Locations) and may be corrected later.
func Agents() []Agent {
	h := home()
	return []Agent{
		{"claude-code", ".claude/skills", filepath.Join(h, ".claude/skills"), ".claude/agents", filepath.Join(h, ".claude/agents"), filepath.Join(h, ".claude")},
		{"antigravity", ".agents/skills", filepath.Join(h, ".gemini/antigravity/skills"), ".agents/agents", filepath.Join(h, ".gemini/antigravity/agents"), filepath.Join(h, ".gemini/antigravity")},
		{"antigravity-cli", ".agents/skills", filepath.Join(h, ".gemini/antigravity-cli/skills"), ".agents/agents", filepath.Join(h, ".gemini/antigravity-cli/agents"), filepath.Join(h, ".gemini/antigravity-cli")},
		{"codex", ".agents/skills", filepath.Join(h, ".codex/skills"), ".agents/agents", filepath.Join(h, ".codex/agents"), filepath.Join(h, ".codex")},
		{"opencode", ".agents/skills", filepath.Join(h, ".config/opencode/skills"), ".agents/agents", filepath.Join(h, ".config/opencode/agents"), filepath.Join(h, ".config/opencode")},
		{"hermes-agent", ".hermes/skills", filepath.Join(h, ".hermes/skills"), ".hermes/agents", filepath.Join(h, ".hermes/agents"), filepath.Join(h, ".hermes")},
	}
}

// Detect returns the agents whose DetectDir exists on disk.
func Detect() []Agent {
	var out []Agent
	for _, a := range Agents() {
		if _, err := os.Stat(a.DetectDir); err == nil {
			out = append(out, a)
		}
	}
	return out
}
```

- [ ] **Step 2: 寫失敗測試**

Create `svc/install/install_test.go`:
```go
package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkSkill(t *testing.T, dir string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# s"), 0o644))
	return dir
}

func TestApplyCopiesSkillIntoProjectDir(t *testing.T) {
	cwd := t.TempDir()
	skill := mkSkill(t, filepath.Join(t.TempDir(), "writer"))

	a := Agent{Type: "test", ProjectSkillsDir: ".claude/skills"}
	sel := Selection{
		SkillPaths: []string{skill},
		Agents:     []Agent{a},
		Global:     false,
		Cwd:        cwd,
	}
	require.NoError(t, Apply(sel))

	got := filepath.Join(cwd, ".claude/skills/writer/SKILL.md")
	_, err := os.Stat(got)
	assert.NoError(t, err)
}

func TestApplyGlobalUsesUserDir(t *testing.T) {
	userSkills := t.TempDir()
	skill := mkSkill(t, filepath.Join(t.TempDir(), "helper"))

	a := Agent{Type: "test", UserSkillsDir: userSkills}
	sel := Selection{SkillPaths: []string{skill}, Agents: []Agent{a}, Global: true, Cwd: t.TempDir()}
	require.NoError(t, Apply(sel))

	_, err := os.Stat(filepath.Join(userSkills, "helper/SKILL.md"))
	assert.NoError(t, err)
}
```

- [ ] **Step 3: 執行確認失敗**

Run: `go test ./svc/install/`
Expected: FAIL（`Selection`、`Apply` 未定義）

- [ ] **Step 4: 實作 install.go**

Create `svc/install/install.go`:
```go
package install

import (
	"io"
	"os"
	"path/filepath"
)

type Selection struct {
	SkillPaths []string
	Agents     []Agent
	Global     bool
	Cwd        string // "" → use os.Getwd
}

func Apply(sel Selection) error {
	cwd := sel.Cwd
	if cwd == "" {
		c, err := os.Getwd()
		if err != nil {
			return err
		}
		cwd = c
	}

	for _, a := range sel.Agents {
		dest := a.ProjectSkillsDir
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(cwd, dest)
		}
		if sel.Global {
			dest = a.UserSkillsDir
		}
		if dest == "" {
			continue
		}
		for _, sp := range sel.SkillPaths {
			target := filepath.Join(dest, filepath.Base(sp))
			if err := copyTree(sp, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyTree recursively copies src dir to dst.
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
```

- [ ] **Step 5: 執行確認通過**

Run: `go test ./svc/install/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add svc/install/
git commit -m "feat: agent install table and skill copy"
```

---

## Task 6: tui — bubbletea 選取介面

**Files:**
- Create: `svc/tui/tui.go`
- Test: `svc/tui/tui_test.go`

TUI 邏輯（非渲染）要可單元測：以 `Model` 的 `Update` 針對按鍵訊息驗狀態，不啟動真終端。

- [ ] **Step 1: 寫失敗測試**

Create `svc/tui/tui_test.go`:
```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/bizshuk/skills/svc/discover"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleCatalog() discover.Catalog {
	return discover.Catalog{
		{PluginName: "docs", FetchOK: true, Skills: []discover.Skill{{Name: "writer", Path: "/x/writer"}}},
		{PluginName: "remote", FetchOK: false, FetchErr: "unable to fetch"},
	}
}

func TestModelInitialSelectionAllSkillsChecked(t *testing.T) {
	m := NewModel(sampleCatalog())
	sel := m.Selection()
	require.Len(t, sel.SkillPaths, 1)
	assert.Equal(t, "/x/writer", sel.SkillPaths[0])
}

func TestModelToggleSkillOff(t *testing.T) {
	m := NewModel(sampleCatalog())
	// space toggles the focused row (row 0 = docs skill)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m2 := updated.(Model)
	assert.Empty(t, m2.Selection().SkillPaths)
}

func TestUnableToFetchRendered(t *testing.T) {
	m := NewModel(sampleCatalog())
	view := m.View()
	assert.Contains(t, view, "unable to fetch")
}
```

- [ ] **Step 2: 執行確認失敗**

Run: `go test ./svc/tui/`
Expected: FAIL（`NewModel` 未定義）

- [ ] **Step 3: 實作 tui.go**

Create `svc/tui/tui.go`:
```go
// Package tui renders the discovered catalog as an interactive tree and
// collects the user's skill/agent selection. Rows for unreachable remote
// plugins render an "unable to fetch" marker.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bizshuk/skills/svc/discover"
	"github.com/bizshuk/skills/svc/install"
)

// row is one selectable skill line (categories are headers, not selectable).
type row struct {
	category string
	skill    string
	path     string
	checked  bool
}

type Model struct {
	cat    discover.Catalog
	rows   []row
	cursor int
	global bool
	done   bool
}

func NewModel(cat discover.Catalog) Model {
	m := Model{cat: cat}
	for _, c := range cat {
		for _, s := range c.Skills {
			m.rows = append(m.rows, row{category: c.PluginName, skill: s.Name, path: s.Path, checked: true})
		}
	}
	return m
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown:
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case tea.KeySpace:
			if len(m.rows) > 0 {
				m.rows[m.cursor].checked = !m.rows[m.cursor].checked
			}
		case tea.KeyEnter:
			m.done = true
			return m, tea.Quit
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString("Select skills to install (space toggle, enter confirm)\n\n")
	lastCat := ""
	ri := 0
	for _, c := range m.cat {
		header := c.PluginName
		if !c.FetchOK {
			header += "  [unable to fetch]"
			if c.FetchErr != "" && c.FetchErr != "unable to fetch" {
				header += " (" + c.FetchErr + ")"
			}
		}
		if header != lastCat {
			b.WriteString(fmt.Sprintf("▸ %s\n", header))
			lastCat = header
		}
		for range c.Skills {
			r := m.rows[ri]
			cursor := " "
			if ri == m.cursor {
				cursor = ">"
			}
			box := "[ ]"
			if r.checked {
				box = "[x]"
			}
			b.WriteString(fmt.Sprintf("  %s %s %s\n", cursor, box, r.skill))
			ri++
		}
	}
	return b.String()
}

func (m Model) Selection() install.Selection {
	var paths []string
	for _, r := range m.rows {
		if r.checked {
			paths = append(paths, r.path)
		}
	}
	return install.Selection{SkillPaths: paths, Global: m.global}
}

// Run starts the interactive program and returns the final selection.
func Run(cat discover.Catalog, global bool) (install.Selection, error) {
	m := NewModel(cat)
	m.global = global
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return install.Selection{}, err
	}
	fm := final.(Model)
	sel := fm.Selection()
	sel.Global = global
	return sel, nil
}
```

- [ ] **Step 4: 執行確認通過**

Run: `go test ./svc/tui/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add svc/tui/
git commit -m "feat: bubbletea selection tui"
```

---

## Task 7: 接線 add 指令端到端

**Files:**
- Modify: `cmd/skills/main.go`

- [ ] **Step 1: 用真實流程替換佔位 RunE**

Replace the `RunE` body in `cmd/skills/main.go` add command with:
```go
RunE: func(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()
    src, err := source.Parse(args[0])
    if err != nil {
        return err
    }
    cat, err := discover.Walk(ctx, fetch.New(), src, depth)
    if err != nil {
        return err
    }

    // resolve target agents
    var targets []install.Agent
    if len(agents) > 0 {
        byName := map[string]install.Agent{}
        for _, a := range install.Agents() {
            byName[string(a.Type)] = a
        }
        for _, name := range agents {
            if a, ok := byName[name]; ok {
                targets = append(targets, a)
            }
        }
    } else {
        targets = install.Detect()
    }

    var sel install.Selection
    if yes {
        for _, c := range cat {
            for _, s := range c.Skills {
                sel.SkillPaths = append(sel.SkillPaths, s.Path)
            }
        }
        sel.Global = global
    } else {
        sel, err = tui.Run(cat, global)
        if err != nil {
            return err
        }
    }
    sel.Agents = targets

    if len(sel.SkillPaths) == 0 {
        return fmt.Errorf("no skills selected")
    }
    if err := install.Apply(sel); err != nil {
        return err
    }
    fmt.Fprintf(cmd.OutOrStdout(), "installed %d skill(s) into %d agent(s)\n", len(sel.SkillPaths), len(sel.Agents))
    return nil
},
```

- [ ] **Step 2: 補上 import**

確保 `cmd/skills/main.go` 的 import 區含：
```go
import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bizshuk/skills/svc/discover"
	"github.com/bizshuk/skills/svc/fetch"
	"github.com/bizshuk/skills/svc/install"
	"github.com/bizshuk/skills/svc/source"
	"github.com/bizshuk/skills/svc/tui"
)
```

- [ ] **Step 3: build 與冒煙測試（本地來源、--yes 跳過 TUI）**

準備一個本地測試 plugin 並執行：
```bash
mkdir -p /tmp/mp/.claude-plugin /tmp/mp/skills/demo
echo '{"name":"demo"}' > /tmp/mp/.claude-plugin/plugin.json
echo '# demo' > /tmp/mp/skills/demo/SKILL.md
go build ./... && go run ./cmd/skills add /tmp/mp --yes --agent claude-code
```
Expected: 印出 `installed 1 skill(s) into 1 agent(s)`，且 `.claude/skills/demo/SKILL.md` 出現在當前目錄下。

- [ ] **Step 4: 全套測試**

Run: `go test ./... && go vet ./...`
Expected: 全部 PASS、vet 無警告。

- [ ] **Step 5: Commit**

```bash
git add cmd/skills/main.go
git commit -m "feat: wire add command end to end"
```

---

## Task 8: README 與收尾

**Files:**
- Create: `README.golang.md`

- [ ] **Step 1: 寫使用說明**

Create `README.golang.md`，內容涵蓋：`skills add [path]` 用法、四個 flag、支援的六個 agent、遞迴深度 3 語意、`unable to fetch` 行為、build 指令 `go build -o skills ./cmd/skills`。以繁體中文為主、術語附英文、不使用粗體、以 backtick 強調。

- [ ] **Step 2: Commit**

```bash
git add README.golang.md
git commit -m "docs: golang skills add usage"
```

---

## Self-Review 對照

- 來源解析（相對路徑／`owner/repo`／https 連結）→ Task 1 全覆蓋。
- marketplace.json 找 plugins 與位置、plugin.json skills 為額外項 → Task 2。
- remote 並行檢查、`unable to fetch` → Task 3（fetch 重試）+ Task 4（errgroup 並行、標記）+ Task 6（渲染）。
- 遞迴深度 3、防環 → Task 4（depth 邊界測試 + visited）。
- plugin 當分類、skill 掛底下 → Task 4 Category + Task 6 樹狀渲染。
- 安裝位置 project／user、六個 agent、subagent 目錄 → Task 5（agents 表含 agents 目錄；本版 skills 複製，agents 複製沿用同一 copyTree，接線於 Task 7 skills 為主，subagents 目錄已備妥待後續啟用）。
- 型別一致性：`source.ParsedSource`、`manifest.Skill`、`discover.Skill/Category/Catalog`、`install.Selection/Agent` 於各 Task 定義後被後續 Task 沿用，命名一致。
- 所有程式碼區塊皆為可直接執行的完整內容，無 placeholder、無刻意錯字。
