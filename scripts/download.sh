#!/bin/bash

# download and install discord-rpc-bridge from GitHub releases

set -e

APP_NAME="discord-rpc-bridge"
REPO="barrettotte/discord-rpc-bridge"
BIN_DIR="$HOME/.local/bin"
CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
CONFIG_DIR="$CONFIG_HOME/$APP_NAME"
SERVICE_DIR="$CONFIG_HOME/systemd/user"

echo "Installing $APP_NAME..."

# get latest release tag
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
if [ -z "$TAG" ]; then
  echo "ERROR: Failed to fetch latest release tag."
  exit 1
fi
echo "Latest release: $TAG"

# download binary
mkdir -p "$BIN_DIR"
echo "Downloading binary..."
curl -fsSL "https://github.com/$REPO/releases/download/$TAG/$APP_NAME" -o "$BIN_DIR/$APP_NAME"
chmod +x "$BIN_DIR/$APP_NAME"

# download service file
mkdir -p "$SERVICE_DIR"
echo "Downloading service file..."
curl -fsSL "https://raw.githubusercontent.com/$REPO/master/$APP_NAME.service" -o "$SERVICE_DIR/$APP_NAME.service"

# download default config if one doesn't exist
mkdir -p "$CONFIG_DIR"
if [ ! -f "$CONFIG_DIR/config.json" ]; then
  echo "Downloading default config..."
  curl -fsSL "https://raw.githubusercontent.com/$REPO/master/config.json" -o "$CONFIG_DIR/config.json"
fi

# enable and start the service
systemctl --user daemon-reload
systemctl --user enable --now "$APP_NAME.service"

echo "Installation completed ($TAG)."
echo ""
echo "Verify with: systemctl --user status $APP_NAME"
echo "View logs:   journalctl --user -u $APP_NAME -f"
