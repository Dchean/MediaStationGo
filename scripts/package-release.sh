#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT_DIR="${OUT_DIR:-dist-release}"
PLATFORMS="${PLATFORMS:-linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ "$VERSION" == *dirty* ]]; then
  echo "Refusing to package a dirty working tree version: $VERSION" >&2
  exit 1
fi

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

if [[ "$VERSION" == MediaStationGo-v* ]]; then
  PACKAGE_PREFIX="$VERSION"
else
  PACKAGE_PREFIX="MediaStationGo-$VERSION"
fi

echo "Building frontend bundle"
(cd web && npm ci && npm run build)

for target in $PLATFORMS; do
  os="${target%/*}"
  arch="${target#*/}"
  name="${PACKAGE_PREFIX}-${os}-${arch}"
  work="$OUT_DIR/$name"
  mkdir -p "$work/web"

  ext=""
  if [[ "$os" == "windows" ]]; then
    ext=".exe"
  fi

  echo "Building $target"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build \
    -trimpath -ldflags="-s -w" \
    -o "$work/mediastation-go$ext" ./cmd/server

  cp -R web/dist "$work/web/dist"
  cp docker-compose.yml config.example.yaml README.md README_EN.md LICENSE "$work/"

  if [[ "$os" == "windows" ]]; then
    (cd "$OUT_DIR" && python3 - "$name" "$name.zip" <<'PY'
import os
import sys
import zipfile

root, archive = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as zf:
    for base, _, files in os.walk(root):
        for filename in files:
            path = os.path.join(base, filename)
            zf.write(path, path)
PY
    )
  else
    tar -C "$OUT_DIR" -czf "$OUT_DIR/$name.tar.gz" "$name"
  fi
  rm -rf "$work"
done

echo "Release packages written to $OUT_DIR"
