package stat

import (
	"strings"

	"github.com/bizshuk/skills/svc/agent"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

// sessionRoot returns the index-th session root that svc/agent declares for
// agentType, already ~-expanded.
//
// svc/agent/providers/*.json is the single owner of these paths — the same
// table svc/session reads — so adding an agent or moving its transcript
// directory is a one-file change. overrideKey names a viper key that, when
// a user config sets it, replaces that one root without touching the
// embedded provider table. An unknown agent or an out-of-range index
// yields "", which the parsers treat as "nothing to scan".
func sessionRoot(agentType agent.Type, index int, overrideKey string) string {
	if override := strings.TrimSpace(viper.GetString(overrideKey)); override != "" {
		return expandHome(override)
	}

	for _, configured := range agent.Agents() {
		if configured.Type != agentType {
			continue
		}
		if index < len(configured.SessionDirs) {
			return configured.SessionDirs[index]
		}
		return ""
	}
	return ""
}

func expandHome(path string) string {
	homedir.DisableCache = true
	if expanded, err := homedir.Expand(path); err == nil {
		return expanded
	}
	return path
}
