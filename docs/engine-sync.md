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
| `dm/dl_features.go` | Import error says "that is not a download list" | Same. Download lists exported by NetMerge (`"app": "netmerge-downloads"`) are still accepted, on purpose. |

Comments that mention NetMerge were left as they are.

## How to bring over a NetMerge fix

1. In NetMerge, see what changed in `dm/` since the base above: `git diff 0bfab33..<new> -- dm/`.
2. Apply that diff here. Where it touches a line from the table, keep the change in the table.
3. Run `go vet ./... && go test ./...` (about 7 minutes), add a line to `CHANGELOG.md`, and update the base commit
   above.
