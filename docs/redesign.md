# Pullwell UI redesign: audit, plan and checklist

Branch `feature/pullwell-ui-redesign`, started from `main` at `1ee4490` (version 0.5.0). `VERSION` and
`appVersion` are not changed on this branch, and nothing here is merged or released without the owner's approval.

## Baseline (before any change, run on 1ee4490)

| Check | Result |
| --- | --- |
| `go test` (program, engine) | pass (engine 386 s) |
| `scripts/uitest/ui_check.mjs` | 22/22, no script errors |
| `menu_check.mjs` | 36/36 |
| `prog_check.mjs` | 12/12 |
| `feat_check.mjs` | 25/25 |
| `flows_check.mjs` | 14/14 |
| `more_check.mjs` | 13/13 |
| Windows workflow (tests + 48 program checks) | pass |

Pre-existing issues noted: speed-limit labels round (250 KB/s is 256 KB/s); the classic notification fallback cannot
be clicked on Windows 11.

## How the window works today

- `ui/index.html`: one static shell (menu bar, toolbar, banner, sidebar, table, status bar). Everything else is
  drawn by `ui/app.js` and `ui/downloads-more.js` as HTML strings.
- Data: `loadDL()` polls `/api/downloads` once a second (every ~5 s while hidden) and calls `render()`, which
  redraws the sidebar, table rows (kept per id and only rewritten when their HTML changes), toolbar state, status
  bar, banners and an open progress dialog. Selection, focus and scroll are kept across refreshes.
- Dialogs: `dlDialog(html)` puts one card in `#dlDlg` and makes the rest inert; `askBox`, `inputBox`, `renameBox`,
  `progressBox`, `showOptions`, `dlProps`, `dlQueues`, `dlGrab`, `dlFolders`, `extConnect`, `addDialog` /
  `showProbe` / `confirmAdd`, `aboutBox`, `shortcutsBox`.
- Menus: `showMenu(x, y, entries)` for the menu bar, right-click and status-bar menus, with keyboard handling.
- Text size: CSS `zoom` on the root with `--zoom` used for heights; menus divide positions by the zoom.

## Problems the redesign solves

1. Twelve equally loud toolbar buttons with coloured icons: the main action does not stand out, and the bar
   reads as a copy of IDM.
2. The progress bars are boxed, heavy and the same for every state; a running download looks like a paused one at
   a glance except for the colour.
3. The colour-filled file-type blocks dominate the name column.
4. Section headings in the sidebar are tracked-out capitals; counts and labels compete.
5. The progress window is a plain form: the percentage, speed and the parts of the file have no hierarchy.
6. Dialogs, options and menus are functional but visually unrelated to one another.

## Design plan: "Precision flow"

The subject is file transfer: data moving through a pipe, in parts, at a measured rate. The memorable element is
the **flow line**: every download's progress is a thin line, and a running download shows its real parts (the
`map` the engine reports) along that line. Everything around it stays quiet.

Colour (dark / light), all as tokens in `ui/app.css`:

| Token | Dark | Light | Use |
| --- | --- | --- | --- |
| graphite (bg) | `#14171b` | `#eef1f3` | window background |
| surface | `#1a1e23` | `#fbfcfc` | list, sidebar, dialogs |
| raised | `#21262c` | `#ffffff` | header, inputs, menus |
| line | `#2a3037` | `#dfe4e8` | hairlines |
| ink / ink-2 | `#e8ebee` / `#9aa5b0` | `#17202a` / `#5a6672` | text |
| flow (accent) | `#3ec7b5` | `#0d8d80` | progress, primary action, focus |
| states | done `#7fcf9b`, paused `#8b96a2`, error `#ef7470`, waiting `#7ea6d8` | done `#2f8a55`, paused `#8d97a1`, error `#c93a35`, waiting `#3f6fa8` | state lines and labels |

Type: Segoe UI Variable (Windows' own, optical sizes) for text; **Bahnschrift** (also built into Windows 10/11, a
DIN-like engineering face) for numbers: sizes, speeds, percentages, times. No downloaded fonts. Sentence case
everywhere; no capitals for labels.

Layout:

```
+--------------------------------------------------------------------------------+
| [mark] Pullwell   Tasks  Downloads  View  Tools  Help        [ search      ]   |  top bar
+--------------------------------------------------------------------------------+
| [+ New download]  Resume  Stop  Delete  |  Start all  Stop all  Clear finished  ...  Queues Grab Options |
+--------------+-----------------------------------------------------------------+
| Library      | Name                          Size   Progress        Speed  Left |
|  All      12 | [zip] holiday-photos.zip      14 MB  ────────── Done           |
|  Downloading | [iso] ubuntu.iso             419 MB  ━━━━╍╍━━──── 28%  7 MB/s 44s|
|  ...         |                                                                 |
| File types   |                                                                 |
| Queues       |                                                                 |
+--------------+-----------------------------------------------------------------+
| 12 downloads   1 downloading   ↓ 7.0 MB/s ~~~~      saving to C:\Users\…        |
+--------------------------------------------------------------------------------+
```

Principles: one accent; state is carried by line style and a short word, never by colour alone; numbers line up
(tabular, right-aligned); radius by hierarchy (badges 4 px, controls 6 px, dialogs 12 px); motion only when
something changes (progress width, dialog open), and none with reduced motion.

Review against the brief: the first sketch used a near-black background with one bright green accent, which is
a common default; the graphite surfaces are lifted, the accent is a muted teal that works on white too, and the
distinctive part moved from colour to the flow line and the Bahnschrift numbers.

## Feature-preservation checklist

Main window: menu bar (mouse, Alt, F10, Alt+letter, arrows, submenus), every toolbar command (`data-tb` add,
resume, stop, stopall, startall, del, delfin, queues, grab, opts), toolbar labels on/off, categories panel on/off,
status bar on/off, search (Ctrl+F), sidebar views and counts, queues, columns (show/hide, resize, sort, Website
off by default), selection (click, Ctrl, Shift, arrows, Home/End, Ctrl+A), keyboard (Enter, Alt+Enter, Space, Del,
Shift+Del, F2, F5, Ctrl+N, Insert, Ctrl+V), right-click menu, drag and drop of links and .torrent files, banners
(update ready/installing, shutdown, downloads waiting), status bar (counts, speed, sparkline, limit menu,
download folder), empty state with Connect a browser.

0.5.0: text size (4 levels), Website column, non-resumable warnings (Stop, Stop all, progress window), Can resume
line, Open when finished (add dialog, remembered; right-click), safety (programs only revealed; checksum-gated),
Move to another folder, Check the checksum, partial-file names and takeover (engine, untouched).

Dialogs: Add, file info (probe), Add with options, torrent, Properties (mirrors, connections, relink, torrent
files), Progress, Rename, Move, Checksum, Queues, Folders, Grabber, Connect a browser, Options (7 tabs, proxy,
updates, uninstall), Keyboard shortcuts, About, confirmations (askBox), Quit.

Engine, API, Windows integration, update and release files: not touched.
