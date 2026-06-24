// defaults.go — policy file discovery shared by all modes.
//
// findPolicyPath is called by both server mode (policies.yaml) and
// copilot mode (copilot-profile.yaml). Everything else in this file
// is path resolution logic with no mode-specific behaviour.
package main

import (
	"os"
	"path/filepath"
)

// findPolicyPath returns the first policy file that exists, searching:
//  1. PERCHGUARD_POLICY env var (explicit override — always wins)
//  2. ~/.config/perchguard/<name>  (installed alongside the binary)
//  3. <binary-dir>/../configs/<name>  (running from the source repo)
//  4. ./configs/<name>  (CWD fallback, classic dev usage inside the repo)
//
// name is one of "copilot-profile.yaml" or "policies.yaml".
// Returns "" when no file is found — caller decides how to handle it.
func findPolicyPath(name string) string {
	if p := os.Getenv("PERCHGUARD_POLICY"); p != "" {
		return p
	}

	candidates := []string{
		filepath.Join(configDir(), name),
	}

	// When running the binary directly from the build output or via go run,
	// look next to the source tree's configs/ directory.
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "..", "configs", name),
			filepath.Join(filepath.Dir(exe), "configs", name),
		)
	}

	candidates = append(candidates, filepath.Join("configs", name))

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return ""
}

// configDir returns the XDG-style user config directory for PerchGuard.
//   ~/.config/perchguard  (Linux / macOS)
func configDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "perchguard")
}
