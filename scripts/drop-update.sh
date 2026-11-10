#!/bin/sh
# Builds Pullwell.exe for Windows and writes it as Pullwell-update.exe (+ .sha256 with the version and commit) into
# the folder given, for a running Pullwell next to it to pick up as an update (see update.go, "local").
# Usage: scripts/drop-update.sh <folder>
set -e
out="${1:?folder}"
cd "$(dirname "$0")/.."
build="$(git rev-parse --short HEAD)$(git diff --quiet || echo -dirty)"
ver="$(tr -d ' \r\n' < VERSION)"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -H=windowsgui -X main.appBuild=$build" -o "$out/Pullwell-update.exe" .
sum="$(sha256sum "$out/Pullwell-update.exe" | cut -d' ' -f1)"
echo "$sum $ver+$build" > "$out/Pullwell-update.exe.sha256"
echo "$out/Pullwell-update.exe  $ver+$build  $sum"
