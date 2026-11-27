#!/bin/sh
set -eu
cd "$(dirname "$0")"
stage=$(mktemp -d "${TMPDIR:-/tmp}/googlesuggest-build.XXXXXX")
trap 'rm -rf "$stage"' EXIT
for arch in arm64 amd64; do
    CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "$stage/$arch" ./cmd/googlesuggest
done
lipo -create "$stage/arm64" "$stage/amd64" -output "$stage/googlesuggest"
codesign --force --sign - "$stage/googlesuggest"
codesign --verify --all-architectures "$stage/googlesuggest"
mv "$stage/googlesuggest" workflow/googlesuggest
