package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"time"
)

// LoadResult wraps a parsed Config with file metadata needed for hot reload and API inspection.
type LoadResult struct {
	Config   *Config
	Hash     string // first 8 bytes of SHA-256 of raw YAML, hex-encoded (16 chars)
	LoadedAt time.Time
	Path     string
}

// LoadWithMeta is a drop-in replacement for Load that also returns file metadata.
func LoadWithMeta(path string) (*LoadResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(data)
	return &LoadResult{
		Config:   cfg,
		Hash:     hex.EncodeToString(sum[:8]),
		LoadedAt: time.Now().UTC(),
		Path:     path,
	}, nil
}

// Watcher polls a policy file every interval and calls onReload when the content hash changes.
// Uses polling rather than fsnotify to stay dependency-free and work reliably in WSL2/containers.
type Watcher struct {
	path     string
	interval time.Duration
	lastHash string
	onReload func(*LoadResult)
	stop     chan struct{}
}

// NewWatcher creates a Watcher that polls path every interval.
// onReload is called with the new LoadResult whenever the policy file changes.
func NewWatcher(path string, interval time.Duration, onReload func(*LoadResult)) *Watcher {
	return &Watcher{
		path:     path,
		interval: interval,
		onReload: onReload,
		stop:     make(chan struct{}),
	}
}

// Start begins polling in a background goroutine. Call Stop to shut it down.
func (w *Watcher) Start(initialHash string) {
	w.lastHash = initialHash
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.check()
			case <-w.stop:
				return
			}
		}
	}()
}

// Stop signals the polling goroutine to exit.
func (w *Watcher) Stop() {
	close(w.stop)
}

func (w *Watcher) check() {
	data, err := os.ReadFile(w.path)
	if err != nil {
		log.Printf("[perchguard/policy] watcher: read error: %v", err)
		return
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:8])
	if hash == w.lastHash {
		return
	}

	result, err := LoadWithMeta(w.path)
	if err != nil {
		log.Printf("[perchguard/policy] watcher: reload failed (keeping current policy): %v", err)
		return
	}

	w.lastHash = hash
	log.Printf("[perchguard/policy] policy reloaded: hash %s → %s", w.lastHash, hash)
	w.onReload(result)
}
