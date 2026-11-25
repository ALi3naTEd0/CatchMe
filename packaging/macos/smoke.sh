#!/usr/bin/env bash
# Smoke test of the macOS disk image on a CI Mac:
#   1. the .dmg mounts and the app inside starts and finds its engine
#   2. the app connects to the engine (report file)
#   3. a screenshot is saved for people who cannot run macOS builds
#   4. closing the app also stops the engine
# Usage: packaging/macos/smoke.sh dist/CatchMe-x.y.z-macos.dmg
set -euo pipefail
DMG=$1
OUT=$(cd "${2:-dist}" && pwd)
REPORT="$OUT/smoke-macos.json"
rm -f "$REPORT"

MNT=$(mktemp -d)
hdiutil attach "$DMG" -mountpoint "$MNT" -nobrowse -quiet
trap 'hdiutil detach "$MNT" -quiet || true' EXIT
APP="$MNT/CatchMe.app"
[[ -x "$APP/Contents/Resources/catchmed" ]] || { echo "FAIL: engine missing from the app"; exit 1; }
lipo -archs "$APP/Contents/Resources/catchmed"

CATCHME_SMOKE_FILE="$REPORT" "$APP/Contents/MacOS/CatchMe" >"$OUT/smoke-macos.log" 2>&1 &
PID=$!

for _ in $(seq 1 60); do [[ -s "$REPORT" ]] && break; sleep 1; done
if [[ ! -s "$REPORT" ]]; then
  echo "FAIL: the app did not connect to its engine within 60 s"; cat "$OUT/smoke-macos.log"; exit 1
fi
cat "$REPORT"; echo
grep -q '"connected":true' "$REPORT" || { echo "FAIL: not connected"; exit 1; }
pgrep -x catchmed >/dev/null || { echo "FAIL: engine process not running"; exit 1; }

sleep 3 # let the window paint
screencapture -x "$OUT/screenshot-macos.png" || echo "(screenshot not allowed on this runner)"

kill -TERM $PID
for _ in $(seq 1 15); do pgrep -x catchmed >/dev/null || break; sleep 1; done
if pgrep -x catchmed >/dev/null; then
  echo "FAIL: the engine kept running after the app closed"; exit 1
fi
echo "OK: macOS disk image mounts, starts, connects, and cleans up"
