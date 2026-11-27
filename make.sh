#!/bin/sh
set -eu
cd "$(dirname "$0")"
version=${WORKFLOW_VERSION-$(cat VERSION)}
case "$version" in
    *[!0-9.]*) echo "Invalid workflow version" >&2; exit 1 ;;
esac
if ! printf '%s\n' "$version" | LC_ALL=C grep -Exq '[0-9]+\.[0-9]+\.[0-9]+'; then
    echo "Expected a workflow version such as 1.0.0" >&2
    exit 1
fi
./build.sh
plutil -lint workflow/info.plist
stage=$(mktemp -d "${TMPDIR:-/tmp}/googlesuggest-package.XXXXXX")
trap 'rm -rf "$stage"' EXIT
cp workflow/info.plist "$stage/info.plist"
plutil -replace version -string "$version" "$stage/info.plist"
zip -jq "$stage/package.zip" "$stage/info.plist" workflow/googlesuggest workflow/icon.png LICENSE workflow/LICENSE-Naver workflow/LICENSE-icon
unzip -tq "$stage/package.zip"
mv "$stage/package.zip" AlfGoogleSuggestPlus.alfredworkflow
