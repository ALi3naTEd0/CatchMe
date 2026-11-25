#!/usr/bin/env bash
# Smoke test of the packaged app on a virtual screen (Xvfb):
#   1. the AppImage starts and finds its bundled engine
#   2. the app connects to the engine (it writes a report file)
#   3. a screenshot is saved for people who cannot run Linux builds
#   4. closing the app also stops the engine (no orphan process)
# Usage: packaging/linux/smoke.sh dist/CatchMe-x.y.z-x86_64.AppImage
set -euo pipefail
APP=$(readlink -f "$1")
OUT=$(readlink -f "${2:-dist}")
REPORT="$OUT/smoke-linux.json"
rm -f "$REPORT"

Xvfb :99 -screen 0 1280x800x24 >/dev/null 2>&1 &
XVFB=$!
trap 'kill $XVFB 2>/dev/null || true' EXIT
export DISPLAY=:99
# Draw on the virtual screen even inside a Wayland session.
unset WAYLAND_DISPLAY
export GDK_BACKEND=x11
sleep 2

export CATCHME_SMOKE_FILE="$REPORT" APPIMAGE_EXTRACT_AND_RUN=1
"$APP" >"$OUT/smoke-linux.log" 2>&1 &

for _ in $(seq 1 60); do [[ -s "$REPORT" ]] && break; sleep 1; done
if [[ ! -s "$REPORT" ]]; then
  echo "FAIL: the app did not connect to its engine within 60 s"; cat "$OUT/smoke-linux.log"; exit 1
fi
cat "$REPORT"; echo
grep -q '"connected":true' "$REPORT" || { echo "FAIL: not connected"; exit 1; }
pgrep -x catchmed >/dev/null || { echo "FAIL: engine process not running"; exit 1; }

sleep 3 # let the window paint
import -window root "$OUT/screenshot-linux.png"

pkill -TERM -x catchme
for _ in $(seq 1 15); do pgrep -x catchmed >/dev/null || break; sleep 1; done
if pgrep -x catchmed >/dev/null; then
  echo "FAIL: the engine kept running after the app closed"; exit 1
fi
echo "OK: Linux package starts, connects, and cleans up"
