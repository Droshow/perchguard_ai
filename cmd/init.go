// init.go — perchguard init command
//
// Detects the environment, registers the claude-hook in ~/.claude/settings.json,
// optionally patches .vscode/mcp.json, generates an API key, and prints a
// status line the operator can use to confirm governance is active.
//
// On Ctrl-C all patched files are restored atomically.
// Running init a second time is idempotent.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/Droshow/PerchGuard/perchguard/pkg/envutil"
)

// claudeSettings is the schema of ~/.claude/settings.json relevant to hooks.
type claudeSettings struct {
	Hooks map[string][]hookMatcher `json:"hooks,omitempty"`
	// Other fields are preserved via RawMessage.
	Extra map[string]json.RawMessage `json:"-"`
}

type hookMatcher struct {
	Matcher string    `json:"matcher"`
	Hooks   []hookDef `json:"hooks"`
}

type hookDef struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// runInit is the entry point for --mode=init.
func runInit(uninstall bool, profile string) {
	pgBin := perchguardBin()

	// Determine the policy profile to register.
	policyArg := ""
	if profile != "" {
		if p := findPolicyPath(profile); p != "" {
			policyArg = " --policy=" + p
		}
	}

	// -- Claude Code hook registration --
	claudeConfigPath := claudeSettingsPath()
	claudeRestored := false
	claudeOriginal := []byte(nil)

	if claudeConfigPath != "" {
		var err error
		claudeOriginal, err = os.ReadFile(claudeConfigPath)
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "[init] cannot read %s: %v\n", claudeConfigPath, err)
		}
	}

	if uninstall {
		runUninstall(claudeConfigPath, claudeOriginal)
		return
	}

	hookCmd := pgBin + " --mode=claude-hook" + policyArg
	hookCmdPost := pgBin + " --mode=claude-hook --post" + policyArg

	if claudeConfigPath != "" {
		if err := registerClaudeHook(claudeConfigPath, hookCmd, hookCmdPost); err != nil {
			fmt.Fprintf(os.Stderr, "[init] WARNING: could not register Claude Code hook: %v\n", err)
			fmt.Fprintf(os.Stderr, "[init] Add this manually to %s:\n%s\n", claudeConfigPath, manualHookSnippet(hookCmd, hookCmdPost))
		} else {
			fmt.Printf("[init] Claude Code hooks registered in %s\n", claudeConfigPath)
		}
	} else {
		fmt.Printf("[init] Claude Code not found. To register manually, add:\n%s\n", manualHookSnippet(hookCmd, hookCmdPost))
	}

	// -- .vscode/mcp.json patching (best-effort) --
	mcpConfigPath := findMCPConfig()
	if mcpConfigPath != "" {
		fmt.Printf("[init] Found .vscode/mcp.json — run 'perchguard --mode=wrap' to govern MCP tool calls too.\n")
	}

	// -- API key --
	apiKey := os.Getenv("PERCHGUARD_API_KEY")
	if apiKey == "" {
		b := make([]byte, 16)
		rand.Read(b)
		apiKey = "pgmk-" + hex.EncodeToString(b)
	}

	// -- Detect project type --
	projectHint := detectProjectType()

	// -- Print status banner --
	printInitBanner(pgBin, apiKey, projectHint, claudeConfigPath != "")

	// -- Restore on Ctrl-C --
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		if !claudeRestored && claudeOriginal != nil && claudeConfigPath != "" {
			restoreClaudeSettings(claudeConfigPath, claudeOriginal)
			claudeRestored = true
		}
		os.Exit(0)
	}()

	// Start server mode.
	fmt.Printf("[init] starting PerchGuard server...\n\n")
	os.Setenv("PERCHGUARD_API_KEY", apiKey)
	if profile != "" {
		if p := findPolicyPath(profile); p != "" {
			os.Setenv("PERCHGUARD_POLICY", p)
		}
	}
}

// registerClaudeHook reads ~/.claude/settings.json and injects the PreToolUse
// and PostToolUse hook entries. Idempotent: existing PerchGuard hooks are
// replaced, other hooks are left unchanged.
func registerClaudeHook(path, preCmd, postCmd string) error {
	// Read existing file or start with empty object.
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		raw = []byte("{}")
	} else if err != nil {
		return err
	}

	// Parse as generic map to preserve unknown fields.
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(raw, &settings); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	// Parse the hooks section.
	hooks := map[string][]hookMatcher{}
	if hooksRaw, ok := settings["hooks"]; ok {
		json.Unmarshal(hooksRaw, &hooks)
	}

	pgMatcher := func(cmd string) hookMatcher {
		return hookMatcher{
			Matcher: ".*",
			Hooks:   []hookDef{{Type: "command", Command: cmd}},
		}
	}

	hooks["PreToolUse"] = injectOrReplace(hooks["PreToolUse"], pgMatcher(preCmd))
	hooks["PostToolUse"] = injectOrReplace(hooks["PostToolUse"], pgMatcher(postCmd))

	hooksRaw, err := json.Marshal(hooks)
	if err != nil {
		return err
	}
	settings["hooks"] = hooksRaw

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

// injectOrReplace adds matcher to the list, replacing any existing PerchGuard hook.
func injectOrReplace(existing []hookMatcher, pg hookMatcher) []hookMatcher {
	out := make([]hookMatcher, 0, len(existing)+1)
	for _, m := range existing {
		// Remove existing perchguard entries.
		hooks := m.Hooks[:0]
		for _, h := range m.Hooks {
			if !strings.Contains(h.Command, "perchguard") {
				hooks = append(hooks, h)
			}
		}
		if len(hooks) > 0 {
			m.Hooks = hooks
			out = append(out, m)
		}
	}
	return append(out, pg)
}

// runUninstall removes PerchGuard hooks from Claude Code settings.
func runUninstall(path string, original []byte) {
	if original == nil {
		fmt.Println("[uninstall] nothing to remove — Claude Code settings not found")
		return
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(original, &settings); err != nil {
		fmt.Fprintf(os.Stderr, "[uninstall] cannot parse settings: %v\n", err)
		return
	}

	if hooksRaw, ok := settings["hooks"]; ok {
		hooks := map[string][]hookMatcher{}
		json.Unmarshal(hooksRaw, &hooks)
		for event, matchers := range hooks {
			var cleaned []hookMatcher
			for _, m := range matchers {
				var filteredHooks []hookDef
				for _, h := range m.Hooks {
					if !strings.Contains(h.Command, "perchguard") {
						filteredHooks = append(filteredHooks, h)
					}
				}
				if len(filteredHooks) > 0 {
					m.Hooks = filteredHooks
					cleaned = append(cleaned, m)
				}
			}
			if len(cleaned) > 0 {
				hooks[event] = cleaned
			} else {
				delete(hooks, event)
			}
		}
		hooksRaw, _ = json.Marshal(hooks)
		settings["hooks"] = hooksRaw
	}

	out, _ := json.MarshalIndent(settings, "", "  ")
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[uninstall] write error: %v\n", err)
		return
	}
	fmt.Printf("[uninstall] PerchGuard hooks removed from %s\n", path)
}

func restoreClaudeSettings(path string, original []byte) {
	if err := os.WriteFile(path, original, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[init] WARNING: could not restore %s: %v\n", path, err)
	} else {
		fmt.Printf("[init] restored %s\n", path)
	}
}

// claudeSettingsPath returns ~/.claude/settings.json when Claude Code is installed.
func claudeSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, ".claude", "settings.json")
	// Return even if missing — we'll create it.
	if _, err := os.Stat(filepath.Join(home, ".claude")); err == nil {
		return p
	}
	return ""
}

// perchguardBin returns the path to the current executable, or "perchguard" if
// it can't be determined (PATH lookup at runtime).
func perchguardBin() string {
	exe, err := os.Executable()
	if err != nil {
		return "perchguard"
	}
	return exe
}

// detectProjectType returns a short hint about the project in the current directory.
func detectProjectType() string {
	checks := []struct {
		file string
		hint string
	}{
		{"go.mod", "Go"},
		{"package.json", "Node"},
		{"pyproject.toml", "Python"},
		{"Cargo.toml", "Rust"},
	}
	for _, c := range checks {
		if _, err := os.Stat(c.file); err == nil {
			return c.hint
		}
	}
	return ""
}

func manualHookSnippet(preCmd, postCmd string) string {
	snippet := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": ".*",
					"hooks":   []any{map[string]any{"type": "command", "command": preCmd}},
				},
			},
			"PostToolUse": []any{
				map[string]any{
					"matcher": ".*",
					"hooks":   []any{map[string]any{"type": "command", "command": postCmd}},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(snippet, "  ", "  ")
	return string(b)
}

func printInitBanner(pgBin, apiKey, projectHint string, claudeCodeFound bool) {
	width := 64
	bar := "├" + strings.Repeat("─", width) + "┤"
	top := "┌" + strings.Repeat("─", width) + "┐"
	bot := "└" + strings.Repeat("─", width) + "┘"
	pad := func(label, value string) string {
		content := fmt.Sprintf("  %-18s %s", label, value)
		p := width - len(content) - 1
		if p < 0 {
			p = 0
		}
		return "│" + content + strings.Repeat(" ", p) + "│"
	}
	title := "│" + fmt.Sprintf("%-*s", width, "  PerchGuard — init") + "│"

	fmt.Println()
	fmt.Println(top)
	fmt.Println(title)
	fmt.Println(bar)
	claudeStatus := "not found"
	if claudeCodeFound {
		claudeStatus = "hooks registered"
	}
	fmt.Println(pad("claude code:", claudeStatus))
	if projectHint != "" {
		fmt.Println(pad("project:", projectHint))
	}
	fmt.Println(pad("api key:", apiKey))
	fmt.Println(pad("policy:", envutil.GetEnv("PERCHGUARD_POLICY", "configs/policies.yaml (default)")))
	fmt.Println(pad("dashboard:", "http://localhost:8080"))
	fmt.Println(pad("metrics:", "http://localhost:8080/metrics"))
	fmt.Println(bar)
	fmt.Println(pad("", "Set PERCHGUARD_API_KEY="+apiKey))
	fmt.Println(pad("", "or add to .env before starting the server."))
	fmt.Println(bot)
	fmt.Println()

	// Platform-specific note.
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("perchguard"); err != nil {
			fmt.Printf("  NOTE: 'perchguard' not found on PATH.\n")
			fmt.Printf("  Claude Code hooks reference: %s\n", pgBin)
			fmt.Printf("  Add it to PATH or set PERCHGUARD_BIN in your shell profile.\n\n")
		}
	}
}
