// skillentry.go classifies one entry of a manifest's `skills` array: is it
// a path on disk, or a remote repo to fetch? The ambiguity between the two
// (a bare "skills/foo" is valid for both) is resolved here and nowhere else.
package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/bizshuk/skills/model"
	"github.com/bizshuk/skills/svc/fetch"
)

// parseManifestSkillEntries splits a manifest's `skills` array into
// on-disk paths (resolved relative to pluginBase) and remote plugins to be
// fetched. String entries are classified by resolveSkillEntry; object
// entries are always remote sources.
func parseManifestSkillEntries(entries []json.RawMessage, pluginBase string) ([]string, []model.RemotePlugin) {
	var paths []string
	var remotes []model.RemotePlugin
	for _, entry := range entries {
		var path string
		if err := json.Unmarshal(entry, &path); err == nil {
			path = strings.TrimSpace(path)
			if path != "" {
				if rp, ok := resolveSkillEntry(path, pluginBase); ok {
					remotes = append(remotes, rp)
				} else {
					paths = append(paths, path)
				}
			}
			continue
		}

		var obj map[string]any
		if err := json.Unmarshal(entry, &obj); err != nil || len(obj) == 0 {
			continue
		}
		name, _ := obj["name"].(string)
		name = strings.TrimSpace(name)

		sourceObj := obj
		if nested, ok := obj["source"].(map[string]any); ok {
			sourceObj = nested
		}
		if name == "" {
			name = inferRemoteSkillName(sourceObj)
		}
		if name == "" {
			continue
		}
		if rp, ok := classifyRemote(name, sourceObj); ok {
			remotes = append(remotes, rp)
		}
	}
	return paths, remotes
}

// resolveSkillEntry decides what a string entry in a manifest's `skills`
// array actually names, and returns a RemotePlugin only when the entry is a
// repo. ok=false means "treat it as a path" — the caller hands it to
// resolveManifestSkillDirs.
//
// The decision is made against the target itself, in this order:
//
//  1. Explicit filesystem markers ("./", "../", absolute) are always paths.
//  2. Explicit remote markers ("github:", a URL scheme, "git@") are always
//     repos.
//  3. What is left is the genuinely ambiguous case: "skills/foo" is valid
//     GitHub shorthand AND a valid relative path. It is resolved against
//     the disk — if it exists under pluginBase it is a path, otherwise it
//     is treated as owner/repo. A repo checked out locally therefore wins
//     over a same-named repo on GitHub, which is what a plugin author
//     shipping their own skills directory expects.
func resolveSkillEntry(raw, pluginBase string) (model.RemotePlugin, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || fetch.IsLocalPath(raw) {
		return model.RemotePlugin{}, false
	}

	source, ref, _ := strings.Cut(strings.TrimPrefix(raw, "github:"), "#")
	explicitRemote := strings.HasPrefix(raw, "github:") ||
		strings.Contains(raw, "://") ||
		strings.HasPrefix(raw, "git@")

	if !explicitRemote && existsUnder(pluginBase, raw) {
		return model.RemotePlugin{}, false
	}

	// A full URL keeps its own address; only its identity has to be derived.
	// GitHub and GitLab URLs yield a real owner/repo, anything else (Gitea,
	// self-hosted, ssh remotes) falls back to the trailing two path segments
	// so the walker still has a stable key for the entry.
	if strings.Contains(source, "://") || strings.HasPrefix(source, "git@") {
		ownerRepo := fetch.OwnerRepoFromURL(source)
		if ownerRepo == "" {
			ownerRepo = trailingOwnerRepo(source)
		}
		if ownerRepo == "" {
			return model.RemotePlugin{}, false
		}
		return model.RemotePlugin{
			Name:      lastPathSegment(ownerRepo),
			OwnerRepo: ownerRepo,
			URL:       source,
			Ref:       ref,
		}, true
	}

	ownerRepo := fetch.NormalizeOwnerRepo(source)
	parts := strings.Split(ownerRepo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return model.RemotePlugin{}, false
	}
	return model.RemotePlugin{
		Name:      parts[1],
		OwnerRepo: ownerRepo,
		URL:       fetch.RepoURL(parts[0], parts[1]),
		Ref:       ref,
	}, true
}

// existsUnder reports whether rel names something that exists inside base.
// Entries that escape base do not count as existing, so a manifest cannot
// use "../../etc/passwd" to win the ambiguity check.
func existsUnder(base, rel string) bool {
	if base == "" {
		return false
	}
	candidate := filepath.Join(base, filepath.FromSlash(rel))
	if !isContainedIn(candidate, base) {
		return false
	}
	_, err := os.Stat(candidate)
	return err == nil
}

func inferRemoteSkillName(obj map[string]any) string {
	if repo, _ := obj["repo"].(string); repo != "" {
		return lastPathSegment(strings.TrimSuffix(repo, ".git"))
	}
	if urlStr, _ := obj["url"].(string); urlStr != "" {
		ownerRepo := fetch.OwnerRepoFromURL(urlStr)
		if ownerRepo != "" {
			return lastPathSegment(ownerRepo)
		}
		return lastPathSegment(strings.TrimSuffix(urlStr, ".git"))
	}
	return ""
}

// trailingOwnerRepo derives an "owner/repo"-shaped identity from a URL that
// neither GitHub nor GitLab claims (Gitea, self-hosted, ssh remotes), by
// taking its last two path segments. It exists only to give such entries a
// stable dedupe key; the fetch itself always uses the original URL.
func trailingOwnerRepo(rawURL string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(rawURL), ".git")
	if i := strings.Index(trimmed, "://"); i >= 0 {
		trimmed = trimmed[i+3:]
	}
	trimmed = strings.TrimPrefix(trimmed, "git@")
	trimmed = strings.Replace(trimmed, ":", "/", 1)
	segments := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(segments) < 2 {
		return ""
	}
	return strings.ToLower(segments[len(segments)-2] + "/" + segments[len(segments)-1])
}

func lastPathSegment(raw string) string {
	raw = strings.Trim(strings.TrimSpace(raw), "/")
	if raw == "" {
		return ""
	}
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		return raw[i+1:]
	}
	return raw
}

// classifyRemote maps the object form of `source` to a model.RemotePlugin.
// Returns ok=false for unrecognized shapes or missing owner/repo info, so
// the caller can drop the entry silently per the design spec.
func classifyRemote(name string, obj map[string]any) (model.RemotePlugin, bool) {
	srcType, _ := obj["source"].(string)
	ref, _ := obj["ref"].(string)
	_ = obj["sha"]
	switch srcType {
	case "github":
		repo, _ := obj["repo"].(string)
		ownerRepo := fetch.NormalizeOwnerRepo(repo)
		if ownerRepo == "" {
			return model.RemotePlugin{}, false
		}
		return model.RemotePlugin{
			Name:      name,
			OwnerRepo: ownerRepo,
			URL:       "https://github.com/" + ownerRepo + ".git",
			Ref:       ref,
		}, true
	case "url":
		urlStr, _ := obj["url"].(string)
		ownerRepo := fetch.OwnerRepoFromURL(urlStr)
		if ownerRepo == "" {
			return model.RemotePlugin{}, false
		}
		return model.RemotePlugin{Name: name, OwnerRepo: ownerRepo, URL: urlStr, Ref: ref}, true
	case "git-subdir":
		urlStr, _ := obj["url"].(string)
		subdir, _ := obj["path"].(string)
		ownerRepo := fetch.OwnerRepoFromURL(urlStr)
		if ownerRepo == "" {
			return model.RemotePlugin{}, false
		}
		return model.RemotePlugin{
			Name:      name,
			OwnerRepo: ownerRepo,
			URL:       urlStr,
			Ref:       ref,
			Subdir:    subdir,
		}, true
	}
	return model.RemotePlugin{}, false
}
