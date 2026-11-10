# Keeping the download engine in step with NetMerge

Until NetMerge drops its own copy, fixes to the download engine land in NetMerge first
(`github.com/nisar87/netmerge`, folder `dm/`). This file lists every way Pullwell's `dm/` differs, so a fix can be
copied over without undoing these changes.

**Base:** NetMerge 3.11.20, commit `0bfab33`.

## Differences

| File | Change | Why |
| --- | --- | --- |
| `dm/host.go` | `Host.Name` field and `hName()` | Messages name the program instead of "NetMerge". |
| `dm/host.go` | `PauseAll()` and `ResumeAll()` | Tray menu. |
| `dm/bt_net.go`, `dm/bt_peer.go`, `dm/dl_dash.go`, `dm/dl_engine.go`, `dm/dl_hls.go`, `dm/dl_manager.go` | The word "NetMerge" in messages, the user agent and the BitTorrent client name replaced by `hName()` | Same. |
| `dm/dl_ext.go` | `/ext/ping` answers `"app": "<product name in lower case>"` | The extension checks it found this program, not NetMerge. |
| `dm/dl_manager.go` | `dlFriendly` knows two more Windows errors: a path that is too long, and a folder or drive that is gone | Plain words instead of "The download could not continue". **Also useful in NetMerge** (reported to the owner). |
| `dm/dl_manager.go` | `Named` field; `nameFixed()` returns it instead of always `false` | A file name typed in the Add dialog was replaced by the website's name. **Bug in NetMerge too** (reported to the owner, see `docs/netmerge-reports.md`). |
| `dm/host.go` | `DownloadDir()` | Tools → Open the download folder. |
| `dm/dl_features.go` | Import error says "that is not a download list" | Same. Download lists exported by NetMerge (`"app": "netmerge-downloads"`) are still accepted, on purpose. |

Comments that mention NetMerge were left as they are.

## How to bring over a NetMerge fix

1. In NetMerge, see what changed in `dm/` since the base above: `git diff 0bfab33..<new> -- dm/`.
2. Apply that diff here. Where it touches a line from the table, keep the change in the table.
3. Run `go vet ./... && go test ./...` (about 7 minutes), add a line to `CHANGELOG.md`, and update the base commit
   above.
