#!/usr/bin/env bash
# Builds a single-file binary for every supported platform into dist/,
# plus a SHA256SUMS.txt file. Usage: scripts/build.sh [version]
set -euo pipefail

VERSION="${1:-dev}"
OUT="${OUT:-dist}"
NAME="confluence-exporter"

# The version ends up inside the binary; accept only plain version text.
if [[ ! "$VERSION" =~ ^[A-Za-z0-9._+-]+$ ]]; then
  echo "Invalid version: $VERSION" >&2
  exit 1
fi

# GOOS GOARCH file-suffix
TARGETS=(
  "darwin  arm64 macos-apple-silicon"
  "darwin  amd64 macos-intel"
  "windows amd64 windows-x64.exe"
  "windows arm64 windows-arm64.exe"
  "linux   amd64 linux-x64"
  "linux   arm64 linux-arm64"
)

rm -rf "$OUT"
mkdir -p "$OUT"
for target in "${TARGETS[@]}"; do
  read -r goos goarch suffix <<<"$target"
  echo "Building $NAME-$suffix ($VERSION)"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$NAME-$suffix" .
done

cd "$OUT"
if command -v sha256sum >/dev/null; then
  sha256sum "$NAME"-* > SHA256SUMS.txt
else
  shasum -a 256 "$NAME"-* > SHA256SUMS.txt
fi
echo "Done: $(ls "$NAME"-* | wc -l | tr -d ' ') binaries in $OUT/"
