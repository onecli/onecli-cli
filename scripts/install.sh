#!/bin/sh
set -e

# OneCLI CLI - manage agents, secrets, and configuration from the terminal
# Source: https://github.com/onecli/onecli-cli
# License: Apache 2.0
#
# Usage: curl -fsSL https://onecli.sh/cli/install | sh
#
# This script downloads the latest onecli binary from GitHub Releases,
# installs it to /usr/local/bin (or ~/.local/bin), and verifies the install.

REPO="onecli/onecli-cli"
BINARY="onecli"

main() {
  # Detect OS
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$OS" in
    darwin) OS="darwin" ;;
    linux)  OS="linux" ;;
    *)
      echo "Error: unsupported operating system: $OS" >&2
      exit 1
      ;;
  esac

  # Detect architecture
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)
      echo "Error: unsupported architecture: $ARCH" >&2
      exit 1
      ;;
  esac

  echo "Detected: $OS/$ARCH"

  # Fetch latest release tag from GitHub API (public repo, no auth needed)
  echo "Fetching latest release..."
  LATEST=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')

  if [ -z "$LATEST" ]; then
    echo "Error: could not determine latest release" >&2
    exit 1
  fi

  VERSION="${LATEST#v}"
  echo "Latest version: $VERSION"

  # Download archive from GitHub Releases
  ARCHIVE="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
  DOWNLOAD_URL="https://github.com/$REPO/releases/download/$LATEST/$ARCHIVE"
  TMPDIR=$(mktemp -d)
  trap 'rm -rf "$TMPDIR"' EXIT

  echo "Downloading $ARCHIVE..."
  HTTP_CODE=$(curl -fsSL -w "%{http_code}" -o "$TMPDIR/$ARCHIVE" "$DOWNLOAD_URL" 2>/dev/null) || true

  if [ ! -f "$TMPDIR/$ARCHIVE" ] || [ "$HTTP_CODE" = "404" ]; then
    echo "Error: failed to download $ARCHIVE" >&2
    echo "Check available releases at https://github.com/$REPO/releases" >&2
    exit 1
  fi

  # Extract binary
  tar -xzf "$TMPDIR/$ARCHIVE" -C "$TMPDIR"

  # Install
  if [ -w /usr/local/bin ]; then
    INSTALL_DIR="/usr/local/bin"
  elif [ -d "$HOME/.local/bin" ] || mkdir -p "$HOME/.local/bin" 2>/dev/null; then
    INSTALL_DIR="$HOME/.local/bin"
  else
    echo "Error: cannot find a writable install directory" >&2
    echo "Try: sudo sh -c 'curl -fsSL https://onecli.sh/cli/install | sh'" >&2
    exit 1
  fi

  cp "$TMPDIR/$BINARY" "$INSTALL_DIR/$BINARY"
  chmod +x "$INSTALL_DIR/$BINARY"

  echo ""
  echo "$BINARY $VERSION installed to $INSTALL_DIR/$BINARY"

  # Ensure install dir is in PATH
  PATH_ADDED=""
  SHELL_RC=""
  case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
      PATH_ADDED="1"
      PATH_LINE="export PATH=\"$INSTALL_DIR:\$PATH\""
      case "${SHELL##*/}" in
        zsh)  SHELL_RC="$HOME/.zshrc" ;;
        bash)
          if [ -f "$HOME/.bash_profile" ]; then
            SHELL_RC="$HOME/.bash_profile"
          else
            SHELL_RC="$HOME/.bashrc"
          fi
          ;;
      esac
      if [ -n "$SHELL_RC" ]; then
        if ! grep -q "$INSTALL_DIR" "$SHELL_RC" 2>/dev/null; then
          echo "" >> "$SHELL_RC"
          echo "# OneCLI" >> "$SHELL_RC"
          echo "$PATH_LINE" >> "$SHELL_RC"
        fi
        echo "PATH configured in $SHELL_RC"
      else
        echo ""
        echo "Add $INSTALL_DIR to your PATH:"
        echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
      fi
      export PATH="$INSTALL_DIR:$PATH"
      ;;
  esac

  # Verify installation
  echo ""
  "$INSTALL_DIR/$BINARY" version 2>/dev/null || echo "$BINARY installed successfully"

  echo ""
  echo "Get started:"
  echo "  onecli auth login --api-key oc_..."
  echo "  onecli agents list"
  echo "  onecli secrets list"

  # Remind user to reload shell if PATH was just added
  if [ -n "$PATH_ADDED" ] && [ -n "$SHELL_RC" ]; then
    echo ""
    echo "To start using $BINARY, run:"
    echo ""
    echo "  source $SHELL_RC"
    echo ""
    echo "Or open a new terminal."
  fi
}

main
