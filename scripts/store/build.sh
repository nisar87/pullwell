#!/bin/bash
# Builds the zip to upload to the Chrome Web Store: scripts/store/build.sh -> extension/dist/Pullwell-extension-chrome-edge.zip
set -e
cd "$(dirname "$0")/../.."
python3 extension/build.py
unzip -p extension/dist/Pullwell-extension-chrome-edge.zip manifest.json | python3 -c "import json,sys; m=json.load(sys.stdin); print('version', m['version'], '| permissions', m['permissions'])"
ls -l extension/dist/Pullwell-extension-chrome-edge.zip
