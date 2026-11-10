# Pullwell

A fast, reliable download manager for Windows, with a browser extension and no telemetry.

> **Status: early development. There is nothing to install yet.** The download engine is being moved here from
> [NetMerge](https://github.com/nisar87/netmerge), where it has been in daily use. "Pullwell" is a working name;
> it may change after a trademark check.

## What it will do

- Download with many connections at once, and resume after a crash, a restart or a lost connection.
- Never report a file as finished unless its size (and checksum, when one is known) is right.
- Queues, schedules, speed limits, categories, batch links, mirrors, FTP, HLS/DASH video and BitTorrent.
- A browser extension (Chrome, Edge, Brave, Firefox) that hands downloads to Pullwell. If Pullwell isn't running,
  the browser simply downloads the file itself.
- With [NetMerge](https://github.com/nisar87/netmerge) running on the same PC, a download also uses all your
  internet connections. Pullwell doesn't need NetMerge.

## Privacy

Pullwell contacts only the sites you download from, and GitHub to check for updates. It sends no telemetry. Its
local web interface answers only on `127.0.0.1`.

## Building

Go 1.24, standard library only, no other modules.

```
go vet ./...
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o Pullwell.exe .
```

The full test suite takes about 7 minutes, most of it the download engine in `dm/`.

## Documents

- [Window, tray and notifications design](docs/window-tray-notifications-design.md)
- The plan and the handover for this program are kept in the NetMerge repository:
  [docs/convoy-handover.md](https://github.com/nisar87/netmerge/blob/main/docs/convoy-handover.md) and
  [docs/standalone-download-manager-plan.md](https://github.com/nisar87/netmerge/blob/main/docs/standalone-download-manager-plan.md).

## Licence

MIT. Pullwell is free and open source; voluntary donations are welcome.
