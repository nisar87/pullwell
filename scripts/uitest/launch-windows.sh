#!/bin/bash
# The browser starting the program on a Windows runner: the program is opened once (it registers its starter for the
# browsers under HKCU), is closed, and Playwright's Chromium with the extension then starts it again by itself when a
# big download is clicked or the popup is opened. Needs: npm i --no-save playwright && npx playwright install chromium.
# Usage: scripts/uitest/launch-windows.sh <program.exe> <out-dir>
set -u
exe="$1"; out="$2"; mkdir -p "$out"
here="$(cd "$(dirname "$0")" && pwd)"
work="$(cygpath -m "${RUNNER_TEMP:-/tmp}")/launchwin"
rm -rf "$work"; mkdir -p "$work"
python "$here/../dmtest/fixtures.py" --port 8765 --out "$work-fx" > "$out/fixtures.log" 2>&1 &
for i in $(seq 40); do curl -s -o /dev/null http://127.0.0.1:8765/file/1KB && break; sleep 0.5; done
node "$here/launch_check.mjs" "$(cygpath -m "$exe")" "$work" 2>&1 | tee "$out/launch_check.txt"
rc=${PIPESTATUS[0]}
cp "$work"/app/data/*.log "$out/" 2>/dev/null
taskkill //F //IM pw.exe > /dev/null 2>&1
grep -q "^FAIL" "$out/launch_check.txt" && exit 1
grep -q "passed$" "$out/launch_check.txt" || exit 1
exit $rc
