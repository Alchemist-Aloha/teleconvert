#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
BIN_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.config/teleconvert"
CONFIG_FILE="$CONFIG_DIR/teleconvert.yaml"

mkdir -p "$BIN_DIR" "$CONFIG_DIR"

pushd "$SCRIPT_DIR" >/dev/null
  echo "Building teleconvert..."
  go build -o ./teleconvert .

  echo "Installing teleconvert to $BIN_DIR"
  install -m 0755 ./teleconvert "$BIN_DIR/teleconvert"

  if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "Creating config at $CONFIG_FILE"
    cp ./example-config.yaml "$CONFIG_FILE"
  else
    echo "Config already exists at $CONFIG_FILE"
  fi
popd >/dev/null

echo "Installation complete."
