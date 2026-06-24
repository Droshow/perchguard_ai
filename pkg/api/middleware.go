package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// requireAPIKey returns middleware that enforces Bearer token auth.
// Delegates comparison to KeyStore.Verify which uses ConstantTimeCompare
// across all active keys — supports key rotation without downtime.
func requireAPIKey(ks *KeyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			token, ok := strings.CutPrefix(auth, "Bearer ")
			if !ok || !ks.Verify(token) {
				writeError(w, http.StatusUnauthorized, "missing or invalid API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
