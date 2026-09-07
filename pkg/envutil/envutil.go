// Package envutil holds tiny helpers shared across PerchGuard's separate main
// packages (cmd/main.go, cmd/redteam-agent, cmd/perchguard-operator) — each one
// had its own byte-for-byte copy of GetEnv until a code-review pass flagged the
// triplication.
package envutil

import "os"

// GetEnv returns the environment variable's value, or fallback if unset/empty.
func GetEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
