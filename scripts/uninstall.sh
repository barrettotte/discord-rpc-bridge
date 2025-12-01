#!/bin/bash

# uninstall discord-rpc-bridge systemd service

set -x

APP_NAME="discord-rpc-bridge"
CONFIG_HOME="$HOME/.config"
BIN_HOME="$HOME/.local/bin"
CACHE_HOME="$HOME/.cache"

echo "Uninstalling $APP_NAME systemd service..."
echo "Config Home: $CONFIG_HOME"
echo "Bin Home: $BIN_HOME"
echo "Cache Home: $CACHE_HOME"

systemctl --user disable --now "$APP_NAME.service" 2>/dev/null || true

rm -f "$CONFIG_HOME/systemd/user/$APP_NAME.service"
rm -f "$BIN_HOME/$APP_NAME"
rm -rf "$CACHE_HOME/$APP_NAME"
rm -rf "$CONFIG_HOME/$APP_NAME"

systemctl --user daemon-reload

echo "Uninstallation completed."
