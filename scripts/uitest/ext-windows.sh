#!/bin/bash
# The browser extension on a Windows runner: Playwright's Chromium loads the extension exactly as the program hands
# it out, pairs with the code, and the downloads go to the real Windows build (its own data folder, nothing
# registered with Windows). Needs: npm i --no-save playwright && npx playwright install chromium (repository root).
# Usage: scripts/uitest/ext-windows.sh <program.exe> <out-dir>
set -u
exe="$1"; out="$2"; mkdir -p "$out"
here="$(cd "$(dirname "$0")" && pwd)"
work="$(cygpath -m "${RUNNER_TEMP:-/tmp}")/extwin"
rm -rf "$work"; mkdir -p "$work/app" "$work/run"

python "$here/../dmtest/fixtures.py" --port 8765 --out "$work/fx" > "$work/fixtures.log" 2>&1 &
for i in $(seq 40); do curl -s -o /dev/null http://127.0.0.1:8765/file/1KB && break; sleep 0.5; done

"$exe" -root "$(cygpath -w "$work/app")" -tray &
for i in $(seq 60); do curl -s -o /dev/null http://127.0.0.1:9197/ && break; sleep 0.5; done
curl -s -o /dev/null http://127.0.0.1:9197/ || { echo "the program did not come up"; tail -20 "$work/app/data/"*.log 2>/dev/null; exit 1; }

# ext_check prints STOP_PROGRAM and writes run/stop when it wants the program gone; answer with run/stopped
( for i in $(seq 1200); do
    if [ -f "$work/run/stop" ]; then
      taskkill //F //IM "$(basename "$exe")" > /dev/null 2>&1
      sleep 1; touch "$work/run/stopped"; break
    fi; sleep 0.25
  done ) &
watcher=$!

node "$here/ext_check.mjs" http://127.0.0.1:9197/ "$work/run" 2>&1 | tee "$out/ext_check.txt"
rc=${PIPESTATUS[0]}
touch "$work/run/stop" # let the watcher end either way
wait $watcher 2>/dev/null
grep -q "^FAIL" "$out/ext_check.txt" && exit 1
grep -q "passed$" "$out/ext_check.txt" || exit 1
exit $rc
