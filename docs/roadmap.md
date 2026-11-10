# Pullwell: road to a daily-usable 1.0

Written 2026-11-10, updated the same day at 18:15. Each phase ends with a check that something was **seen working on the owner's Windows PC**, not
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
| ✅ | Notification when a download finishes; grouped for several | screen |
| ✅ | Quit during a download, start again: it continues, not from 0 | screen + log |
| ✅ | **Self-update**: a dropped build installs by itself and restarts (three times); Go back works (14:42) | drop file + log |
| ✅ | Start with Windows writes one startup entry and removes it again | Options + log |

Gate: all rows ✅, or a bug found, fixed, released through the self-update and re-checked.

## Phase 2: menu bar (owner's request)

| | Item |
| --- | --- |
| ✅ | A menu bar above the toolbar: **Tasks** (Add URL, Add batch, Add torrent, Grabber, Import/Export list, Exit), **Downloads** (Resume, Stop, Stop all, Start all, Delete, Delete completed, Redownload, Properties, Queue, Speed limit), **View** (categories panel, toolbar labels, columns, theme, refresh), **Tools** (Scheduler, Options, Connect a browser, Open download folder, Open log), **Help** (Keyboard shortcuts, Check for updates, About) |
| ✅ | Works with the mouse and the keyboard (Alt opens it, arrows move, Enter chooses, Escape closes), shows shortcuts |
| ✅ | The design stays our own; IDM was only the reference for how things are found |

Gate: automated window checks extended to the menus; seen on the PC.

## Phase 3: everything a daily user touches

| | Item |
| --- | --- |
| 🟡 | Browser extension: 10 checks pass in Chromium with the extension as Pullwell hands it out (pairing, big download taken over, small one left, right-click entry, browser keeps the download when Pullwell is closed); still to try in the owner's Chrome and Edge. The program saves the extension into the download folder and opens it there (✅ on the PC, 16:12). |
| ✅ | Live progress dialog for a download (speed, parts, connections, log), refreshing while open: 12 automated checks, and seen on the PC from 43% to Complete with Open offered (16:15) |
| ✅ | Redownload a finished or failed file (Downloads → Download again); open the folder of any item |
| 🟡 | Copied links: offer to add a link when one is copied (off by default, switch in Options). On the PC the switch works and the listener starts; the notification itself needs a link copied in another program, which only the owner can do (copies made in Pullwell's own window are ignored on purpose) |
| 🟡 | Plain-language messages for every failure (disk full, folder not writable, site refuses, login needed) checked against the engine's texts; two added (path too long, folder gone); "cannot write" seen on the PC |
| ✅ | Keyboard shortcuts list (Help menu) |

## Phase 4: looks like an installed program

| | Item |
| --- | --- |
| ✅ | Program icon in Explorer, taskbar and Alt+Tab (icon resource built without extra tools; seen in the taskbar and title bar, 14:53) |
| ✅ | Start menu entry (so Windows search finds Pullwell; notifications now say "Pullwell" with its icon, seen 15:47). 🟡 Uninstall: written, not run on the PC (it would remove the test copy) |
| ✅ | First public release v0.2.0 on GitHub with notes; the self-update from GitHub proven on the PC (found by "Check for updates" and installed by itself, 16:11) |

## Phase 5: trust

| | Item |
| --- | --- |
| 🟡 | Unpleasant cases. ✅ read-only folder (C:\Windows\System32: clear message and a notification, PC), ✅ a 240-character file name (shortened, saved, PC), ✅ quit and restart mid-download (PC), ✅ 400 items (list in 3 ms, redraw ~35 ms; Linux), ✅ disk filled up in the middle of a download (says so, Resume after freeing space gives the right file; Linux, small RAM disk), ✅ server gone for a minute in the middle (tries again, continues from where it was, right file; Linux). ⬜ cable pulled and sleep/wake on the PC: need the owner |
| ⬜ | A day of normal use with the log checked afterwards |
| ✅ | README with screenshots, privacy note, how to install the extension |

When phases 1-5 are ✅, Pullwell is 1.0.

## Added after the first plan (0.3.0 and later)

| | Item |
| --- | --- |
| ✅ | Proxy: same as Windows (default), none, or typed (http or socks5); Test button (seen on the PC: "Windows uses no proxy", test passes) |
| ✅ | Rename a download (F2): finished file renamed on disk (PC), or the name used when it starts |
| ✅ | Speed limit from the status bar and Tools menu (PC) |
| ✅ | Save the list writes into the download folder (PC); Load a list through the Windows file picker (PC) |
| ✅ | Keep the PC awake while downloading (Options → Schedule, on by default) |
| ✅ | Help → What's new (opens the releases page in the default browser, PC) and Report a problem |
| 🟡 | Tray menu: Add a download, Open the download folder (written; the tray menu needs a right-click, which the remote control here cannot do) |
| 🟡 | A click on a notification shows the finished downloads. On the owner's Windows 11 the click on a classic notification does not reach the program (logged: shown, timed out, never "clicked"); real Windows toasts would be needed |
| ✅ | Fixed: a numbered series typed in the Add dialog failed; a typed file name was replaced by the site's; the shown progress dropped to 0 while a retry waited |
