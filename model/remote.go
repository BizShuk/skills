package model

// RemotePlugin is a plugin whose skills must be fetched from another repo.
// OwnerRepo + URL + Ref identify the source; Subdir narrows it inside the
// repo for git-subdir sources.
type RemotePlugin struct {
	Name      string // grouping name taken from manifest
	OwnerRepo string // normalized lowercase "owner/repo"
	URL       string // full git URL or marketplace entry url
	Ref       string // pinned branch or tag (may be empty)
	Subdir    string // repo-internal path (git-subdir sources only)
}
