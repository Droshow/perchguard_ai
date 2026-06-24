package audit

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// JSONLRecordSink appends each store.AuditRecord as a single JSON line to a JSONL file.
// This is the per-call forensic record: survives ring buffer rollover and server restarts.
// The ring buffer remains the live query layer; this file is for post-incident reconstruction.
type JSONLRecordSink struct {
	path string
	mu   sync.Mutex
}

// NewJSONLRecordSink creates the sink and ensures the parent directory exists.
func NewJSONLRecordSink(path string) (*JSONLRecordSink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("jsonl sink: mkdir %s: %w", filepath.Dir(path), err)
	}
	return &JSONLRecordSink{path: path}, nil
}

// Push writes rec to the JSONL file in a background goroutine — never blocks the caller.
func (s *JSONLRecordSink) Push(rec store.AuditRecord) {
	go func() {
		if err := s.write(rec); err != nil {
			log.Printf("[perchguard/audit] jsonl write failed: %v", err)
		}
	}()
}

func (s *JSONLRecordSink) write(rec store.AuditRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	_, err = f.Write(append(line, '\n'))
	return err
}
