#!/usr/bin/env bash
# Build release binaries and linux packages (deb, rpm, pkg.tar.zst) into dist/.
# Usage: ./dist.sh [version]
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
cd "$ROOT"

VERSION=${1:-${VERSION:-dev}}
VERSION=${VERSION#v}
DIST="$ROOT/dist"
STAGE="$ROOT/build"

NFPM=${NFPM:-nfpm}
if ! command -v "$NFPM" >/dev/null 2>&1; then
  echo "dist: nfpm not found (go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest)" >&2
  exit 1
fi

rm -rf "$DIST" "$STAGE"
mkdir -p "$DIST" "$STAGE"

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*}
  arch=${target#*/}
  asset="teleconvert_${os}_${arch}"

  mkdir -p "$STAGE/$asset"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags="-s -w -X main.version=v${VERSION}" \
    -o "$STAGE/$asset/teleconvert" .
  tar -C "$STAGE/$asset" -czf "$DIST/${asset}.tar.gz" teleconvert

  [[ "$os" == linux ]] || continue
  sed -e "s|\${ARCH}|$arch|g" -e "s|\${VERSION}|$VERSION|g" \
    "$ROOT/nfpm.yaml" > "$STAGE/nfpm.yaml"
  for packager in deb rpm archlinux; do
    case "$packager" in
      deb) ext=deb ;;
      rpm) ext=rpm ;;
      archlinux) ext=pkg.tar.zst ;;
    esac
    "$NFPM" package --config "$STAGE/nfpm.yaml" --packager "$packager" \
      --target "$DIST/${asset}.${ext}"
  done
done

(
  cd "$DIST"
  sha256sum ./*.tar.gz ./*.deb ./*.rpm ./*.pkg.tar.zst > checksums.txt
)

echo "dist: wrote $(ls "$DIST" | wc -l) files to $DIST"
