package store

import (
	"log"
	"time"
)

// Reaper evicts sessions that exceed the idle timeout or maximum duration.
// On eviction it calls onExpire so the caller can emit a governance record before deletion.
// Start/Stop are safe to call from any goroutine.
type Reaper struct {
	store     SessionStore
	idleLimit time.Duration // 0 = disabled
	maxAge    time.Duration // 0 = disabled
	onExpire  func(ss *SessionState, terminatedEarly bool)
	stop      chan struct{}
}

func NewReaper(
	st SessionStore,
	idleLimit, maxAge time.Duration,
	onExpire func(*SessionState, bool),
) *Reaper {
	return &Reaper{
		store:     st,
		idleLimit: idleLimit,
		maxAge:    maxAge,
		onExpire:  onExpire,
		stop:      make(chan struct{}),
	}
}

func (r *Reaper) Start() {
	go r.run()
}

func (r *Reaper) Stop() {
	close(r.stop)
}

func (r *Reaper) run() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.sweep()
		case <-r.stop:
			return
		}
	}
}

// SweepForTest runs one sweep synchronously. Only for use in tests.
func (r *Reaper) SweepForTest() { r.sweep() }

func (r *Reaper) sweep() {
	now := time.Now()
	for _, ss := range r.store.List() {
		reason := ""
		if r.idleLimit > 0 && now.Sub(ss.UpdatedAt) > r.idleLimit {
			reason = "idle_timeout"
		} else if r.maxAge > 0 && now.Sub(ss.CreatedAt) > r.maxAge {
			reason = "max_duration"
		}
		if reason == "" {
			continue
		}
		log.Printf("[perchguard/reaper] evicting session %s (reason=%s)", ss.SessionID, reason)
		r.store.Delete(ss.SessionID)
		r.onExpire(ss, true)
	}
}
