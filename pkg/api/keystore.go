package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"
)

// KeyInfo is the public view of an API key — label and creation time only.
// Key material is never returned after initial issuance.
type KeyInfo struct {
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

type keyEntry struct {
	KeyInfo
	key []byte // plaintext in memory; never serialized or logged
}

// KeyStore manages named management API keys.
// All token comparisons use crypto/subtle.ConstantTimeCompare.
// Key material is held in process memory and never written to disk or logs.
type KeyStore struct {
	mu      sync.RWMutex
	entries []*keyEntry
}

// NewKeyStore creates a KeyStore seeded with a single key under the given label.
func NewKeyStore(initialKey, label string) *KeyStore {
	return &KeyStore{
		entries: []*keyEntry{{
			KeyInfo: KeyInfo{Label: label, CreatedAt: time.Now()},
			key:     []byte(initialKey),
		}},
	}
}

// Verify returns true if token matches any active key.
func (ks *KeyStore) Verify(token string) bool {
	tb := []byte(token)
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	for _, e := range ks.entries {
		if subtle.ConstantTimeCompare(tb, e.key) == 1 {
			return true
		}
	}
	return false
}

// Rotate generates a new pgmk-... key for label, evicts any existing key
// with that label, and returns the new key plaintext. The key is returned
// exactly once — the caller must relay it to the operator immediately.
func (ks *KeyStore) Rotate(label string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	newKey := "pgmk-" + hex.EncodeToString(b)

	ks.mu.Lock()
	defer ks.mu.Unlock()

	filtered := ks.entries[:0:0]
	for _, e := range ks.entries {
		if e.Label != label {
			filtered = append(filtered, e)
		}
	}
	ks.entries = append(filtered, &keyEntry{
		KeyInfo: KeyInfo{Label: label, CreatedAt: time.Now()},
		key:     []byte(newKey),
	})
	return newKey, nil
}

// Primary returns the key material of the first (oldest) active key.
// Used only to embed the key in the localhost-only browser dashboard.
func (ks *KeyStore) Primary() string {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	if len(ks.entries) == 0 {
		return ""
	}
	return string(ks.entries[0].key)
}

// List returns public metadata for all active keys. Key material is never included.
func (ks *KeyStore) List() []KeyInfo {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	out := make([]KeyInfo, len(ks.entries))
	for i, e := range ks.entries {
		out[i] = e.KeyInfo
	}
	return out
}
