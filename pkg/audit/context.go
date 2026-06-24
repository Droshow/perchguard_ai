package audit

import "strings"

// ContextProvider enriches an agent's intent baseline with project context
// before the first tool call hits the admission pipeline.
// The zero-value NoOpContextProvider is the default when no provider is configured.
type ContextProvider interface {
	ReadContext(limit int) Context
}

// Context holds optional enrichment data returned by a ContextProvider.
type Context struct {
	PRDSummary      string
	RecentSnapshots []SnapshotSummary
	Loaded          bool // false when the provider has no context to offer
}

// SnapshotSummary is a lightweight view of one recorded project snapshot.
type SnapshotSummary struct {
	ID        string `json:"id"`
	Summary   string `json:"summary"`
	Timestamp string `json:"timestamp"`
}

// IntentText returns the combined text for enriching a SessionAgent intent baseline.
// Returns empty string when Loaded is false.
func (c Context) IntentText() string {
	if !c.Loaded {
		return ""
	}
	var parts []string
	if c.PRDSummary != "" {
		parts = append(parts, c.PRDSummary)
	}
	for _, s := range c.RecentSnapshots {
		if s.Summary != "" {
			parts = append(parts, s.Summary)
		}
	}
	return strings.Join(parts, " ")
}

// NoOpContextProvider returns an empty context. Used when no provider is configured.
type NoOpContextProvider struct{}

func (NoOpContextProvider) ReadContext(int) Context { return Context{} }
