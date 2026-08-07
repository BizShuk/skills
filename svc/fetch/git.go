// git.go is the fallback for git URLs that are neither GitHub nor GitLab
// (self-hosted GitLab, Gitea, Bitbucket, ssh remotes), and the auth fallback
// for private GitHub/GitLab repos when the HTTP archive path cannot
// authenticate. There is no portable archive endpoint for arbitrary hosts,
// so this path shells out to `git clone`.
package fetch

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// materializeGitWithRemoteFallback clones s.URL; if that fails and the URL
// is an https://github.com or https://gitlab.com remote, retries once with
// the matching SSH form (git@host:path.git) so users who only have SSH keys
// still succeed.
func materializeGitWithRemoteFallback(ctx context.Context, s ParsedSource) (string, error) {
	dir, err := materializeGit(ctx, s)
	if err == nil {
		return dir, nil
	}
	alt, ok := alternateGitSSHURL(s.URL)
	if !ok {
		return "", err
	}
	s2 := s
	s2.URL = alt
	dir, err2 := materializeGit(ctx, s2)
	if err2 == nil {
		return dir, nil
	}
	return "", err
}

// alternateGitSSHURL maps https://github.com/o/r.git → git@github.com:o/r.git
// (same for gitlab.com). Other hosts and already-SSH URLs return false.
func alternateGitSSHURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "github.com", "gitlab.com":
	default:
		return "", false
	}
	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	if path == "" || !strings.Contains(path, "/") {
		return "", false
	}
	return fmt.Sprintf("git@%s:%s.git", host, path), true
}

// materializeGit shallow-clones s.URL into a fresh tempdir. The clone is
// --depth 1 (and --single-branch) because the pipeline only ever reads the
// working tree; no history is needed. A Ref, when present, is passed as
// --branch, which git accepts for both branches and tags.
//
// Failures are not retried: a clone that fails is nearly always a bad URL,
// a missing ref, or an auth problem, none of which a retry fixes. Missing
// git binary is reported explicitly so the user knows what to install.
func materializeGit(ctx context.Context, s ParsedSource) (string, error) {
	if s.URL == "" {
		return "", fmt.Errorf("fetch: empty git url")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("fetch: git is required to clone %s but was not found in PATH", s.URL)
	}

	tmpDir, err := os.MkdirTemp("", "skills-fetch-*")
	if err != nil {
		return "", err
	}

	args := []string{"clone", "--depth", "1", "--single-branch"}
	if s.Ref != "" {
		args = append(args, "--branch", s.Ref)
	}
	args = append(args, s.URL, tmpDir)

	cmd := exec.CommandContext(ctx, "git", args...)
	// Never prompt: a clone that needs credentials must fail fast instead of
	// hanging the CLI on a terminal password prompt.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("unable to fetch %s: git clone failed: %s", s.URL, strings.TrimSpace(string(out)))
	}
	return tmpDir, nil
}
