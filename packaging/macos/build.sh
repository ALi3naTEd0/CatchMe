#!/usr/bin/env bash
# Builds CatchMe for macOS (Apple Silicon and Intel in one app) with the
# download engine inside, and writes dist/CatchMe-<version>-macos.dmg.
#
# The app is signed ad hoc, not with an Apple Developer ID, so the first
# time macOS asks to confirm opening it (right click > Open).
set -euo pipefail
cd "$(dirname "$0")/../.."
VERSION=$(packaging/version.sh)
DIST=dist
mkdir -p "$DIST"

echo "== CatchMe $VERSION for macOS"
flutter build macos --release --dart-define=CATCHME_VERSION="$VERSION"
BUILT=build/macos/Build/Products/Release/CatchMe.app

STAGE=build/dmg
rm -rf "$STAGE"
mkdir -p "$STAGE"
cp -R "$BUILT" "$STAGE/CatchMe.app"
APP="$STAGE/CatchMe.app"

# One engine binary for both architectures, in Resources where the app looks.
for arch in arm64 amd64; do
  (cd catchmed && CGO_ENABLED=0 GOOS=darwin GOARCH=$arch \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "../build/catchmed-$arch" ./cmd/catchmed)
done
lipo -create -output "$APP/Contents/Resources/catchmed" build/catchmed-arm64 build/catchmed-amd64

# Adding a file breaks the app's signature: sign the engine, then the app.
codesign --force --sign - "$APP/Contents/Resources/catchmed"
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"

ln -s /Applications "$STAGE/Applications"
hdiutil create -volname "CatchMe" -srcfolder "$STAGE" -ov -format UDZO "$DIST/CatchMe-$VERSION-macos.dmg"
ls -lh "$DIST"
