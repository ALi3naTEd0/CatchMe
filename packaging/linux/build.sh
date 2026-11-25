#!/usr/bin/env bash
# Builds CatchMe for Linux x86_64 with the download engine inside, and
# writes to dist/:
#   CatchMe-<version>-x86_64.AppImage   one file, runs on most distributions
#   CatchMe-<version>-linux-x86_64.tar.gz
set -euo pipefail
cd "$(dirname "$0")/../.."
VERSION=$(packaging/version.sh)
DIST=dist
BUNDLE=build/linux/x64/release/bundle
mkdir -p "$DIST" build/tools

echo "== CatchMe $VERSION for Linux"
flutter build linux --release --dart-define=CATCHME_VERSION="$VERSION"

# The engine goes next to the app executable, where the app looks for it.
(cd catchmed && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "../$BUNDLE/catchmed" ./cmd/catchmed)

tar -C "$(dirname "$BUNDLE")" -czf "$DIST/CatchMe-$VERSION-linux-x86_64.tar.gz" \
  --transform "s,^bundle,CatchMe-$VERSION," bundle

if [[ "${SKIP_APPIMAGE:-}" == 1 ]]; then
  echo "SKIP_APPIMAGE=1: not building the AppImage"
  ls -lh "$DIST"
  exit 0
fi

# AppImage: the bundle plus a launcher, a .desktop entry and an icon.
APPDIR=build/AppDir
rm -rf "$APPDIR"
cp -r "$BUNDLE" "$APPDIR"
cp packaging/linux/AppRun "$APPDIR/AppRun"
chmod +x "$APPDIR/AppRun"
cp packaging/linux/io.github.ali3nated0.CatchMe.desktop "$APPDIR/"
cp assets/app-icon.png "$APPDIR/catchme.png"

TOOL=${APPIMAGETOOL:-build/tools/appimagetool-x86_64.AppImage}
if [[ ! -x "$TOOL" ]]; then
  curl -fsSL -o "$TOOL" https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage
  chmod +x "$TOOL"
fi
# Extract-and-run: works without FUSE (CI runners, containers).
APPIMAGE_EXTRACT_AND_RUN=1 ARCH=x86_64 "$TOOL" --no-appstream "$APPDIR" "$DIST/CatchMe-$VERSION-x86_64.AppImage"

ls -lh "$DIST"
