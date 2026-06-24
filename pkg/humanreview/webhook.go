// Package humanreview implements the human-in-loop review dispatcher.
// When PerchGuard returns HUMAN_REVIEW it POSTs to a configured webhook
// and either receives the decision inline (200 OK) or polls for it (202 Accepted).
package humanreview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
)

// ErrTimeout is returned when the review webhook does not respond within TimeoutSeconds.
var ErrTimeout = errors.New("human review timed out")

// Dispatcher implements admission.ReviewDispatcher.
type Dispatcher struct {
	webhookURL   string
	timeoutSecs  int
	pollURL      string
	pollInterval time.Duration
	httpClient   *http.Client
}

type Option func(*Dispatcher)

func WithHTTPClient(c *http.Client) Option {
	return func(d *Dispatcher) { d.httpClient = c }
}

func WithPollConfig(pollURL string, interval time.Duration) Option {
	return func(d *Dispatcher) {
		d.pollURL = pollURL
		d.pollInterval = interval
	}
}

func NewDispatcher(webhookURL string, timeoutSeconds int, opts ...Option) *Dispatcher {
	d := &Dispatcher{
		webhookURL:   webhookURL,
		timeoutSecs:  timeoutSeconds,
		pollInterval: 2 * time.Second,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// TimeoutSeconds satisfies admission.ReviewDispatcher.
func (d *Dispatcher) TimeoutSeconds() int { return d.timeoutSecs }

// Dispatch POSTs the review request and blocks until approved/denied or timeout.
// Returns (false, ErrTimeout) on deadline — callers map this to DENY.
func (d *Dispatcher) Dispatch(ctx context.Context, req admission.ReviewRequest) (bool, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return false, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return false, ErrTimeout
		}
		return false, fmt.Errorf("webhook post: %w", err)
	}
	defer resp.Body.Close()

	// 200 OK → synchronous decision inline
	if resp.StatusCode == http.StatusOK {
		var r admission.ReviewResponse
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return false, fmt.Errorf("decode response: %w", err)
		}
		return r.Approved, nil
	}

	// 202 Accepted → async; poll for decision
	if resp.StatusCode == http.StatusAccepted {
		if d.pollURL == "" {
			return false, fmt.Errorf("webhook returned 202 but statusPollURL not configured")
		}
		return d.poll(ctx, req.RequestUID)
	}

	return false, fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
}

func (d *Dispatcher) poll(ctx context.Context, uid string) (bool, error) {
	timeout := time.After(time.Duration(d.timeoutSecs) * time.Second)
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false, ErrTimeout
		case <-timeout:
			return false, ErrTimeout
		case <-ticker.C:
			approved, done, err := d.checkStatus(ctx, uid)
			if err != nil {
				continue // transient, keep polling
			}
			if done {
				return approved, nil
			}
		}
	}
}

func (d *Dispatcher) checkStatus(ctx context.Context, uid string) (approved, done bool, err error) {
	url := fmt.Sprintf("%s/%s", d.pollURL, uid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, false, err
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNotFound {
		return false, false, nil // still pending
	}

	var r admission.ReviewResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return false, false, err
	}
	return r.Approved, true, nil
}
