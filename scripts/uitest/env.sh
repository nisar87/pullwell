#!/bin/bash
# env.sh [fresh]: make sure the fixtures server, headless Chrome and a test copy of the program are running.
# The test copy is ./pw built from the repository (go build -o "$UIT/pw" .), with its data in $UIT.
# Then run, for example: node scripts/uitest/menu_check.mjs 9333 http://127.0.0.1:9197/ http://127.0.0.1:8765 "$UIT"
UIT="${UIT:-/tmp/pullwell-uitest}"
mkdir -p "$UIT"
cd "$(dirname "$0")/../.."
curl -s -o /dev/null 127.0.0.1:8765/file/1KB || (nohup python3 scripts/dmtest/fixtures.py --port 8765 --out "$UIT/fx" > "$UIT/fx.log" 2>&1 &)
curl -s -o /dev/null 127.0.0.1:9333/json/version || (nohup google-chrome --headless=new --no-sandbox --disable-gpu --remote-debugging-port=9333 --user-data-dir="$UIT/chrome" about:blank > /dev/null 2>&1 &)
if [ "$1" = fresh ]; then
  pkill -f "$UIT/pw -root"; while pgrep -f "$UIT/pw -root" >/dev/null; do sleep 0.3; done
  rm -rf "$UIT/root"; mkdir -p "$UIT/root"
  (nohup "$UIT/pw" -root "$UIT/root" -tray > "$UIT/pw.out" 2>&1 &)
fi
for i in $(seq 40); do curl -s -o /dev/null 127.0.0.1:9197/ && curl -s -o /dev/null 127.0.0.1:8765/file/1KB && curl -s -o /dev/null 127.0.0.1:9333/json/version && exit 0; sleep 0.5; done
echo "not ready"; exit 1
