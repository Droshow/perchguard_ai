package store

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// LokiSink ships AuditRecord events to a Grafana Loki instance.
// Each call is fire-and-forget in a goroutine; if Loki is unreachable it is
// silently skipped — SQLite / JSONL are the source of truth.
type LokiSink struct {
	endpoint string
	client   *http.Client
}

func NewLokiSink(endpoint string) *LokiSink {
	return &LokiSink{
		endpoint: endpoint + "/loki/api/v1/push",
		client:   &http.Client{Timeout: 3 * time.Second},
	}
}

// Push ships rec to Loki non-blocking.
func (l *LokiSink) Push(rec AuditRecord) {
	go l.push(rec)
}

func (l *LokiSink) push(rec AuditRecord) {
	// Loki push API expects:
	// {"streams":[{"stream":{labels},"values":[["nanosecond_ts","log_line"]]}]}
	labels := map[string]string{
		"job":      "perchguard",
		"session":  rec.SessionID,
		"tool":     rec.ToolName,
		"decision": rec.Decision,
	}
	if rec.PolicyHit != "" {
		labels["policy"] = rec.PolicyHit
	}

	line, _ := json.Marshal(rec)
	ts := strconv.FormatInt(rec.Timestamp.UnixNano(), 10)

	payload := lokiPush{
		Streams: []lokiStream{
			{
				Stream: labels,
				Values: [][]string{{ts, string(line)}},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	resp, err := l.client.Post(l.endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return // Loki unreachable — silent skip
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		log.Printf("[loki] push failed: %s", resp.Status)
	}
}

type lokiPush struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"` // [[nanosecond_ts, line], ...]
}

// PushFunc returns a func(AuditRecord) compatible with AuditRingBuffer.SetPushHook.
func (l *LokiSink) PushFunc() func(AuditRecord) { return l.Push }
