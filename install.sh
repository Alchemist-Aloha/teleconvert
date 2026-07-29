#!/usr/bin/env bash
set -euo pipefail

REPOSITORY="Alchemist-Aloha/teleconvert"
INSTALL_DIR="${TELECONVERT_INSTALL_DIR:-$HOME/.local/bin}"
CONFIG_DIR="${TELECONVERT_CONFIG_DIR:-$HOME/.config/teleconvert}"
VERSION="${TELECONVERT_VERSION:-latest}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "teleconvert installer: required command not found: $1" >&2
    exit 1
  fi
}

require_command curl
require_command tar

case "$(uname -s)" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *)
    echo "teleconvert installer: unsupported operating system: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *)
    echo "teleconvert installer: unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

asset="teleconvert_${os}_${arch}.tar.gz"
if [[ "$VERSION" == "latest" ]]; then
  latest_url=$(curl -fsSL --retry 3 -o /dev/null -w '%{url_effective}' \
    "https://github.com/${REPOSITORY}/releases/latest")
  latest_url=${latest_url%/}
  installing_version=${latest_url##*/}
  if [[ -z "$installing_version" || "$installing_version" == "latest" ]]; then
    echo "teleconvert installer: could not resolve the latest release version" >&2
    exit 1
  fi
else
  installing_version=$VERSION
fi
release_base="https://github.com/${REPOSITORY}/releases/download/${installing_version}"

installed_binary="$INSTALL_DIR/teleconvert"
if [[ -x "$installed_binary" ]]; then
  installed_version=$("$installed_binary" -version 2>/dev/null || true)
  if [[ -z "$installed_version" ]]; then
    installed_version="unknown (older build)"
  fi
else
  installed_version="not installed"
fi

echo "Installed version:  $installed_version"
echo "Installing version: teleconvert $installing_version"

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

echo "Downloading teleconvert ${installing_version} for ${os}/${arch}..."
curl -fsSL --retry 3 -o "$tmp_dir/$asset" "$release_base/$asset"
curl -fsSL --retry 3 -o "$tmp_dir/checksums.txt" "$release_base/checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  (
    cd "$tmp_dir"
    grep " ${asset}$" checksums.txt | sha256sum -c -
  )
elif command -v shasum >/dev/null 2>&1; then
  (
    cd "$tmp_dir"
    grep " ${asset}$" checksums.txt | shasum -a 256 -c -
  )
else
  echo "teleconvert installer: sha256sum or shasum is required" >&2
  exit 1
fi

tar -xzf "$tmp_dir/$asset" -C "$tmp_dir"
mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp_dir/teleconvert" "$INSTALL_DIR/teleconvert"

config_file="$CONFIG_DIR/teleconvert.yaml"
if [[ ! -f "$config_file" ]]; then
  mkdir -p "$CONFIG_DIR"
  curl -fsSL --retry 3 \
    "https://raw.githubusercontent.com/${REPOSITORY}/main/example-config.yaml" \
    -o "$config_file"
  echo "Created configuration: $config_file"
else
  echo "Preserved existing configuration: $config_file"
fi

echo "Installed teleconvert ${installing_version}: $INSTALL_DIR/teleconvert"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "Add $INSTALL_DIR to PATH to run teleconvert from any directory." ;;
esac
