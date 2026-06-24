#!/usr/bin/env bash
# install.sh — build and install perchguard to ~/.local/bin (or /usr/local/bin with --system)
#
# Usage:
#   ./scripts/install.sh             # installs to ~/.local/bin/perchguard
#   ./scripts/install.sh --system    # installs to /usr/local/bin (requires sudo)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

SYSTEM=false
for arg in "$@"; do
  [[ "$arg" == "--system" ]] && SYSTEM=true
done

if $SYSTEM; then
  INSTALL_DIR="/usr/local/bin"
else
  INSTALL_DIR="${HOME}/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

echo "→ Building perchguard..."
cd "$REPO_ROOT"
go build -o perchguard ./cmd

echo "→ Installing to $INSTALL_DIR/perchguard"
install -m 755 perchguard "$INSTALL_DIR/perchguard"

# Install default policy configs to ~/.config/perchguard/
# These are the built-in policies the binary uses when no project-local policy exists.
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/perchguard"
mkdir -p "$CONFIG_DIR"
for f in configs/policies.yaml configs/copilot-profile.yaml; do
  if [[ -f "$REPO_ROOT/$f" ]]; then
    dest="$CONFIG_DIR/$(basename "$f")"
    # Only install if not already customised by the user (check via mtime).
    if [[ ! -f "$dest" ]]; then
      cp "$REPO_ROOT/$f" "$dest"
      echo "→ Installed default policy: $dest"
    else
      echo "→ Kept existing policy:     $dest  (delete to reset to defaults)"
    fi
  fi
done

# Verify it's on PATH
if ! command -v perchguard &>/dev/null; then
  echo ""
  echo "⚠  $INSTALL_DIR is not on your PATH."
  echo "   Add this to your ~/.zshrc or ~/.bashrc:"
  echo ""
  echo "   export PATH=\"\$HOME/.local/bin:\$PATH\""
  echo ""
else
  echo "✓  perchguard installed: $(command -v perchguard)"
  perchguard --help 2>&1 | head -3 || true
fi

echo ""
echo "Quickstart for any project:"
echo ""
echo "  1. Add a server URL to .vscode/mcp.json (your real MCP upstream)"
echo "  2. Run:  perchguard --mode=wrap"
echo "  3. Open a second terminal and run the meter:"
echo "     perchguard --mode=meter --watch-addr http://localhost:8080"
echo "  4. Start Copilot — every tool call now flows through PerchGuard."
echo "  5. Ctrl-C to stop; .vscode/mcp.json is automatically restored."
