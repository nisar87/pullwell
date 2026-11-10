#!/bin/bash
# Runs the window checks and the screenshots against the real program window (WebView2) on a Windows runner.
# WebView2 is given a remote debugging port (WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS), so the same checks that run in
# headless Chrome on Linux drive the real window here. Every check gets a fresh program with its own data folder
# (-root), so nothing is registered with Windows.
# Usage: scripts/uitest/windows-window.sh <program.exe> <out-dir>
set -u
exe="$1"; out="$2"
mkdir -p "$out"
here="$(cd "$(dirname "$0")" && pwd)"
work="$(cygpath -u "${RUNNER_TEMP:-/tmp}")/pwwindow"
rm -rf "$work"; mkdir -p "$work"
app=http://127.0.0.1:9197/ fx=http://127.0.0.1:8765 dbg=9333

{
  echo "runner image: ${ImageOS:-?} ${ImageVersion:-?}"
  cmd //c ver | tr -d '\r' | grep -v '^$'
  echo "fonts in C:\\Windows\\Fonts:"; ls /c/Windows/Fonts | grep -iE '^(segui|segoeui|bahn|cascadia|consola)' | tr '\n' ' '; echo
  reg query 'HKLM\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}' //v pv 2>/dev/null | grep pv | sed 's/^ */WebView2 runtime: /'
} | tee "$out/environment.txt"

python "$here/../dmtest/fixtures.py" --port 8765 --out "$work/fx" > "$work/fixtures.log" 2>&1 &
for i in $(seq 40); do curl -s -o /dev/null $fx/file/1KB && break; sleep 0.5; done

shot_desktop() { powershell -NoProfile -Command "Add-Type -AssemblyName System.Windows.Forms,System.Drawing; \$b=[System.Windows.Forms.Screen]::PrimaryScreen.Bounds; \$m=New-Object System.Drawing.Bitmap \$b.Width,\$b.Height; \$g=[System.Drawing.Graphics]::FromImage(\$m); \$g.CopyFromScreen(\$b.Location,[System.Drawing.Point]::Empty,\$b.Size); \$m.Save('$(cygpath -w "$out/$1.png")')" || echo "desktop screenshot failed"; }

start_app() { # $1: name of the data folder
  local root="$work/$1"; rm -rf "$root"; mkdir -p "$root"
  WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS="--remote-debugging-port=$dbg" "$exe" -root "$(cygpath -w "$root")" -debug &
  for i in $(seq 120); do
    curl -s "http://127.0.0.1:$dbg/json/list" 2>/dev/null | grep -q '"url": *"http://127.0.0.1:9197/' && curl -s -o /dev/null $app && return 0
    sleep 0.5
  done
  echo "the window did not come up for $1 (WebView2 debugging port $dbg)"; tail -20 "$root/data/pullwell.log" 2>/dev/null; return 1
}
stop_app() {
  curl -s -X POST -H 'X-Pullwell: 1' -H 'Content-Type: application/json' -d '{"quit":true}' ${app}api/app > /dev/null
  for i in $(seq 40); do curl -s -o /dev/null $app || break; sleep 0.5; done
  sleep 1
}

failed=0
summary="$out/summary.txt"; : > "$summary"
for c in ui_check menu_check prog_check feat_check flows_check more_check; do
  if ! start_app "$c"; then echo "$c: the window did not start" | tee -a "$summary"; failed=1; continue; fi
  mkdir -p "$out/$c"; node "$here/$c.mjs" $dbg $app $fx "$out/$c" > "$out/$c.txt" 2>&1
  rc=$?
  pass=$(grep -c '^PASS' "$out/$c.txt"); fail=$(grep -c '^FAIL' "$out/$c.txt")
  echo "$c: $pass passed, $fail failed (exit $rc)" | tee -a "$summary"
  grep -E '^FAIL|errors' "$out/$c.txt" | sed 's/^/    /' | tee -a "$summary"
  [ "$rc" != 0 ] || [ "$fail" != 0 ] && failed=1
  stop_app
done

if start_app shots; then
  sleep 2; shot_desktop desktop-window
  node "$here/shots.mjs" $dbg $app $fx "$out/shots" 2>&1 | tee -a "$summary"
  shot_desktop desktop-after
  stop_app
else
  echo "shots: the window did not start" | tee -a "$summary"; failed=1
fi
echo; cat "$summary"
exit $failed
