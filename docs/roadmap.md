# Pullwell: road to a daily-usable 1.0

Written 2026-11-10. Each phase ends with a check that something was **seen working on the owner's Windows PC**, not
only in tests. Status is kept up to date in this file; the CHANGELOG lists what each build changed.

Legend: ✅ done and verified on Windows · 🟡 done, verified on Linux only · ⬜ to do.

## Where we start

The download engine (from NetMerge, in daily use there), the compact window, its own WebView2 window
(✅ "window: ready" on the owner's PC, 2026-11-10 09:17), tray, single instance, browser extension, Start with
Windows, grouped notifications and self-update (🟡) exist. Tests: 150 engine tests, ~40 program tests, 22 automated
window checks, 8 extension tests, all green.

## Phase 1: prove the basics on the real PC

| | Check | How |
| --- | --- | --- |
| ✅ | Window opens, list, toolbar, dialogs, dark mode | screen |
| ✅ | A real download from the internet finishes with the right checksum | Go installer, published SHA-256 |
| ✅ | Close keeps it in the tray; tray click and a second start bring it back; no second icon | screen |
| ✅ | Open, Show in folder (Explorer comes to the front) | screen |
| ⬜ | Notification when a download finishes; grouped for several | screen |
| ⬜ | Quit during a download, start again: it continues, not from 0 | screen + log |
| 🟡 | **Self-update**: a dropped build installs by itself and restarts (✅ twice, 09:27 and 09:29); Go back works (to check) | drop file + log |
| ⬜ | Start with Windows writes one startup entry and removes it again | Options + log |

Gate: all rows ✅, or a bug found, fixed, released through the self-update and re-checked.

## Phase 2: menu bar (owner's request)

| | Item |
| --- | --- |
| ✅ | A menu bar above the toolbar: **Tasks** (Add URL, Add batch, Add torrent, Grabber, Import/Export list, Exit), **Downloads** (Resume, Stop, Stop all, Start all, Delete, Delete completed, Redownload, Properties, Queue, Speed limit), **View** (categories panel, toolbar labels, columns, theme, refresh), **Tools** (Scheduler, Options, Connect a browser, Open download folder, Open log), **Help** (Keyboard shortcuts, Check for updates, About) |
| ✅ | Works with the mouse and the keyboard (Alt opens it, arrows move, Enter chooses, Escape closes), shows shortcuts |
| ⬜ | The design stays our own; IDM was only the reference for how things are found |

Gate: automated window checks extended to the menus; seen on the PC.

## Phase 3: everything a daily user touches

| | Item |
| --- | --- |
| ⬜ | Browser extension tested in the owner's Chrome and Edge: big download taken over, right-click "Download with Pullwell", browser keeps the download when Pullwell is closed |
| ⬜ | Live progress dialog for a download (speed, parts, connections, log), refreshing while open |
| 🟡 | Redownload a finished or failed file (Downloads → Download again); open the folder of any item |
| ⬜ | Copied links: offer to add a link when one is copied (off by default, switch in Options) |
| ⬜ | Plain-language messages for every failure (disk full, folder not writable, site refuses, login needed) checked against the engine's texts |
| 🟡 | Keyboard shortcuts list (Help menu) |

## Phase 4: looks like an installed program

| | Item |
| --- | --- |
| ⬜ | Program icon in Explorer, taskbar and Alt+Tab (icon resource built without extra tools) |
| ⬜ | Start menu entry (so Windows search finds Pullwell) and an "uninstall" that removes it, the startup entry and the data on request |
| ⬜ | First public release v0.2.0 on GitHub with notes; the self-update from GitHub proven on the PC |

## Phase 5: trust

| | Item |
| --- | --- |
| ⬜ | Unpleasant cases on the PC: cable pulled mid-download, disk nearly full, read-only folder, very long file names, PC sleep and wake, hundreds of items |
| ⬜ | A day of normal use with the log checked afterwards |
| ⬜ | README with screenshots, privacy note, how to install the extension |

When phases 1-5 are ✅, Pullwell is 1.0.
