package audit

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// FileSink appends governance records to a local JSON file owned by PerchGuard.
// It is the default sink for standalone deployments without a project context root.
type FileSink struct {
	path string
	mu   sync.Mutex
}

// NewFileSink creates a FileSink that writes to path.
// The parent directory is created on first write if it does not exist.
func NewFileSink(path string) *FileSink {
	return &FileSink{path: path}
}

// EmitAsync writes rec in a background goroutine so it never blocks the admission pipeline.
func (f *FileSink) EmitAsync(rec GovernanceRecord) {
	go func() {
		if err := f.write(rec); err != nil {
			log.Printf("[perchguard/audit] governance record write failed: %v", err)
		} else {
			log.Printf("[perchguard/audit] governance record written (session=%s agent=%s)",
				rec.SessionContext.SessionID, rec.SessionContext.AgentID)
		}
	}()
}

func (f *FileSink) write(rec GovernanceRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(f.path), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	var entries []json.RawMessage
	if data, err := os.ReadFile(f.path); err == nil {
		_ = json.Unmarshal(data, &entries)
	}

	newEntry, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	entries = append(entries, json.RawMessage(newEntry))

	out, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal entries array: %w", err)
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	return os.Rename(tmp, f.path)
}
