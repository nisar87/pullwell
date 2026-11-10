#!/bin/bash
# env.sh [fresh]: make sure the fixtures server, headless Chrome and a Pullwell test copy are running.
SP=/tmp/claude-0/-home-claude/30b109fa-1e20-575d-bed2-fadefa0e67c3/scratchpad
cd /home/claude/pullwell
curl -s -o /dev/null 127.0.0.1:8765/file/1KB || (nohup python3 scripts/dmtest/fixtures.py --port 8765 --out $SP/ui/fx > $SP/ui/fx.log 2>&1 &)
curl -s -o /dev/null 127.0.0.1:9333/json/version || (nohup google-chrome --headless=new --no-sandbox --disable-gpu --remote-debugging-port=9333 --user-data-dir=$SP/ui/chromeprof-$$ about:blank > /dev/null 2>&1 &)
if [ "$1" = fresh ]; then
  pkill -f "$SP/pw -root"; while pgrep -f "$SP/pw -root" >/dev/null; do sleep 0.3; done
  rm -rf $SP/ui/rootx ~/Downloads/*; mkdir -p $SP/ui/rootx
  (nohup $SP/pw -root $SP/ui/rootx -tray > $SP/ui/pwx.out 2>&1 &)
fi
for i in $(seq 40); do curl -s -o /dev/null 127.0.0.1:9197/ && curl -s -o /dev/null 127.0.0.1:8765/file/1KB && curl -s -o /dev/null 127.0.0.1:9333/json/version && exit 0; sleep 0.5; done
echo "env not ready"; exit 1
