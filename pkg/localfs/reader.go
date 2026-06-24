// Package localfs provides local filesystem adapters for the audit.ContextProvider
// and audit.Sink interfaces. It reads project context (PRDs, snapshots) and writes
// governance records into a project's context/ directory structure.
package localfs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Droshow/PerchGuard/perchguard/pkg/audit"
)

const (
	contextDir   = "context"
	snapshotsDir = "snapshots"
	prdsDir      = "prds"
	entriesFile  = "entries.json"
)

// Reader reads project context from the local filesystem.
// It implements audit.ContextProvider.
type Reader struct {
	root string
}

// NewReader creates a Reader. If root is empty, FindRoot auto-discovers by
// walking up from the current working directory.
func NewReader(root string) *Reader {
	if root == "" {
		root = FindRoot()
	}
	return &Reader{root: root}
}

// Root returns the project root path. Empty string means not found.
func (r *Reader) Root() string { return r.root }

// FindRoot walks up from CWD looking for a directory containing context/snapshots/.
func FindRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, contextDir, snapshotsDir)); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// ReadContext loads PRD + recent human-authored snapshots.
// limit is the max number of recent snapshots to include in intent enrichment.
// Returns audit.Context{Loaded: false} when no valid root is found.
func (r *Reader) ReadContext(limit int) audit.Context {
	if r.root == "" {
		return audit.Context{}
	}
	if _, err := os.Stat(filepath.Join(r.root, contextDir, snapshotsDir)); err != nil {
		return audit.Context{}
	}
	return audit.Context{
		Loaded:          true,
		PRDSummary:      r.readLatestPRD(),
		RecentSnapshots: r.readRecentSnapshots(limit),
	}
}

// readLatestPRD returns the first 800 chars of the most recently authored PRD.
func (r *Reader) readLatestPRD() string {
	prdPath := filepath.Join(r.root, contextDir, prdsDir)
	entries, err := os.ReadDir(prdPath)
	if err != nil {
		return ""
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, filepath.Join(prdPath, e.Name()))
		}
	}
	if len(files) == 0 {
		return ""
	}
	sort.Strings(files)
	data, err := os.ReadFile(files[len(files)-1])
	if err != nil {
		return ""
	}
	text := string(data)
	if len(text) > 800 {
		text = text[:800]
	}
	return text
}

// readRecentSnapshots returns the last `limit` human-authored snapshots.
// PerchGuard-emitted entries are excluded to prevent circular intent feedback.
func (r *Reader) readRecentSnapshots(limit int) []audit.SnapshotSummary {
	path := filepath.Join(r.root, contextDir, snapshotsDir, entriesFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw []map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	var human []map[string]any
	for _, e := range raw {
		if src, _ := e["source"].(string); src != "perchguard" {
			human = append(human, e)
		}
	}
	if len(human) > limit {
		human = human[len(human)-limit:]
	}
	var result []audit.SnapshotSummary
	for _, e := range human {
		summary, _ := e["summary"].(string)
		id, _ := e["id"].(string)
		ts, _ := e["timestamp"].(string)
		if summary != "" {
			result = append(result, audit.SnapshotSummary{ID: id, Summary: summary, Timestamp: ts})
		}
	}
	return result
}
