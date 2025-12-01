#!/bin/bash

# install discord-rpc-bridge as systemd service

set -ex

APP_NAME="discord-rpc-bridge"
CONFIG_HOME="$HOME/.config"
BIN_HOME="$HOME/.local/bin"

echo "Installing $APP_NAME as systemd service..."
echo "Config Home: $CONFIG_HOME"
echo "Bin Home: $BIN_HOME"

mkdir -p "$CONFIG_HOME/$APP_NAME"
cp -f config.json "$CONFIG_HOME/$APP_NAME/"

cp -f "bin/$APP_NAME" "$BIN_HOME/"

mkdir -p "$CONFIG_HOME/systemd/user"
cp -f "$APP_NAME.service" "$CONFIG_HOME/systemd/user/"

systemctl --user daemon-reload && systemctl --user enable --now "$APP_NAME.service"

echo "Installation completed."
