package model

import "time"

// AgentSession is one agent session associated with a working directory.
type AgentSession struct {
	Agent        string
	ID           string
	StartedAt    time.Time
	LastActivity time.Time
	Path         string
}

// SessionEvent is one provider-neutral event in an agent session timeline.
type SessionEvent struct {
	Timestamp time.Time
	Role      string
	Kind      string
	Summary   string
	Raw       string
}

// AgentSessionDetail contains session metadata and its normalized timeline.
type AgentSessionDetail struct {
	Session AgentSession
	Title   string
	CWD     string
	Events  []SessionEvent
}
