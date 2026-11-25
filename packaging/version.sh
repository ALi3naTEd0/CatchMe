#!/usr/bin/env bash
# Prints the version to stamp on a build: $VERSION if set, otherwise the
# pubspec version without the build number.
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ -n "${VERSION:-}" ]]; then
  echo "$VERSION"
else
  grep '^version:' pubspec.yaml | sed -e 's/version:[[:space:]]*//' -e 's/+.*//'
fi
