package localfs

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/Droshow/PerchGuard/perchguard/pkg/audit"
)

// Writer appends GovernanceRecord entries to the project's context/snapshots/entries.json.
// It implements audit.Sink.
type Writer struct {
	root string
	mu   sync.Mutex
}

// NewWriter creates a Writer. If root is empty, FindRoot auto-discovers from CWD.
func NewWriter(root string) *Writer {
	if root == "" {
		root = FindRoot()
	}
	return &Writer{root: root}
}

// WriteGovernanceSnapshot appends rec to entries.json using a tmp-file atomic write.
func (w *Writer) WriteGovernanceSnapshot(rec audit.GovernanceRecord) error {
	if w.root == "" {
		return fmt.Errorf("project root not found — governance snapshot not written")
	}
	entriesPath := filepath.Join(w.root, contextDir, snapshotsDir, entriesFile)

	w.mu.Lock()
	defer w.mu.Unlock()

	var entries []json.RawMessage
	if data, err := os.ReadFile(entriesPath); err == nil {
		_ = json.Unmarshal(data, &entries)
	}

	newEntry, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal governance record: %w", err)
	}
	entries = append(entries, json.RawMessage(newEntry))

	out, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal entries array: %w", err)
	}
	tmp := entriesPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	return os.Rename(tmp, entriesPath)
}

// EmitAsync implements audit.Sink by writing in a background goroutine.
func (w *Writer) EmitAsync(rec audit.GovernanceRecord) {
	go func() {
		if err := w.WriteGovernanceSnapshot(rec); err != nil {
			log.Printf("[perchguard/localfs] governance snapshot write failed: %v", err)
		} else {
			log.Printf("[perchguard/localfs] governance snapshot written (session=%s agent=%s)",
				rec.SessionContext.SessionID, rec.SessionContext.AgentID)
		}
	}()
}
