#!/bin/sh
set -eu
cd "$(dirname "$0")"
./build.sh
plutil -lint workflow/info.plist
stage=$(mktemp -d "${TMPDIR:-/tmp}/googlesuggest-package.XXXXXX")
trap 'rm -rf "$stage"' EXIT
zip -jq "$stage/package.zip" workflow/info.plist workflow/googlesuggest workflow/icon.png LICENSE workflow/LICENSE-icon
unzip -tq "$stage/package.zip"
mv "$stage/package.zip" AlfGoogleSuggestPlus.alfredworkflow
