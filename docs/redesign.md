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

Pre-existing issues noted: the classic notification fallback cannot
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
| [+ New download]  Resume  Pause  Delete  |  Resume all  Pause all  Clear finished  ...  Queues Grab Options |
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

0.5.0: text size (4 levels), Website column, non-resumable warnings (Pause, Pause all, progress window), Can resume
line, Open when finished (add dialog, remembered; right-click), safety (programs only revealed; checksum-gated),
Move to another folder, Check the checksum, partial-file names and takeover (engine, untouched).

Dialogs: Add, file info (probe), Add with options, torrent, Properties (mirrors, connections, relink, torrent
files), Progress, Rename, Move, Checksum, Queues, Folders, Grabber, Connect a browser, Options (7 tabs, proxy,
updates, uninstall), Keyboard shortcuts, About, confirmations (askBox), Quit.

Engine, API, Windows integration, update and release files: not touched.

## What changed (on the branch)

- `ui/app.css`: rewritten around tokens (colour, type, radius, row height, shadows) for light and dark; the
  Windows theme setting still decides between them, and View > Theme still overrides it.
- `ui/index.html`: a top bar (mark, name, menus, search) and a command bar with one primary action
  (New download), the list commands, the bulk commands (Resume all, Pause all, Clear finished) and the tools on the
  right. Labels collapse by the window's own width, so they also fit at the larger text sizes.
- `ui/app.js`:
  - the flow line in the Progress column, with the engine's part map folded to at most 16 cells
  - state words Done, Failed, Waiting, Retry soon and Paused, each with its own colour and line style
  - sidebar sections Library (with a new Downloading view and a live dot), File types and Queues
  - View > Row spacing (Comfortable or Compact)
  - an empty state with a New download button
  - the progress window: header, large percentage and state, speed and time left, part line, a speed graph of
    the last two minutes (bounded, kept only while the download runs), details in three columns, events, footer
  - renamed commands: "Add URL…" became "New download…" and "Delete completed" became "Clear finished"
- The icon colour follows the accent: the drawn icon in `scripts/mkres` and the window's fallback icon. Nothing
  else in the native code changed.
- Window checks updated to the new words and structure (`scripts/uitest`).

## Results after the redesign (Linux, headless Chromium)

| Check | Result |
| --- | --- |
| `go vet`, `go test` (program) | pass |
| `ui_check` | 22/22, no script errors |
| `menu_check` | 36/36 |
| `prog_check` | 12/12 |
| `feat_check` | 25/25 |
| `flows_check` | 14/14 |
| `more_check` | 13/13 |
| `ext_check` (browser extension) | 10/10 |

Screens reviewed in light and dark at 980×560, 1280×720, 1366×768 and 1920×1080, at the text sizes 90 %, 115 %
and 130 %, and with Compact rows. The dialogs reviewed were Add, File info, Rename, Move, Checksum, Delete
confirmation, Queues, Grab links, Folders, Connect a browser, About, Keyboard shortcuts, Properties, Options and
Progress.

## Results on Windows (GitHub Actions)

All on GitHub-hosted runners (`windows-latest`, image `win25-vs2026 20261103.298.1`, Windows Server 2025, build
10.0.26100), never on the owner's PC. Each job starts from a clean machine.

| Job | What it does | Result |
| --- | --- | --- |
| tests | `go vet`, program tests, engine tests | pass |
| program | `scripts/wintest`: the built program through its command line and local API (start, single instance and hand-over, `pullwell://` links and wrong codes, downloads with checksum, pause and resume, rename, move to another drive, checksum check, typed proxy, Start with Windows, Start menu entry, keeping the PC awake, dropped update and Go back, separate copy next to an installed one, Uninstall, protected program folder, Quit) | 56/56 |
| window | the real WebView2 window (runtime 156.0.4314.58) with a remote debugging port: the window checks of `scripts/uitest` and 56 screenshots | ui 22/22, menus 36/36, progress 12/12, features 25/25, flows 14/14, more 13/13 |

The window job's log counts more PASS lines for flows and more (28 and 26) because those checks also print
their passes a second time in a summary; the checks themselves are 14 and 13.

Fonts in the real window (`windows/fonts.json`): Segoe UI Variable Text and Display, Segoe UI, Bahnschrift,
Cascadia Mono and Consolas are all present, so text is in Segoe UI Variable and numbers in Bahnschrift.

Found and fixed through the real window: Bahnschrift's figures sat 1 to 2 px above the file names in the list
(fixed with line-height 1 and a 1 px nudge that applies only when Bahnschrift is there); a check raced with
the Explorer window that Open the download folder brings up (test fixed: the window rightly closes its menus
when it loses the focus); the Pause/Resume test clicked inside the button's deliberate 0.9 s guard.

What this does not cover: theme and window size are switched through the browser engine (emulated colour
scheme and viewport), not by changing the Windows theme or dragging the window; display scaling other than
100 % was not tried; the runner's desktop is 1024×768; toasts and the tray icon are not looked at (the
program job checks the notification code paths it can, not what Windows draws); the browser extension is
checked on Linux only.

## Screenshots

From the real window on Windows (window job, commit `f2e2bf7`), stored at reduced colour depth:

- [The window on the Windows desktop](redesign/windows/desktop.png) (native title "Pullwell (separate copy)"; the
  "Select an app" dialog is left over from a check that opens a .bin file)
- Main window [light](redesign/windows/main-light.png) and [dark](redesign/windows/main-dark.png), 1280×720;
  other sizes [light](redesign/windows/sizes-light.png) and [dark](redesign/windows/sizes-dark.png): 980×560, 1366×768, 1920×1080
- Progress window [light](redesign/windows/progress-light.png) and [dark](redesign/windows/progress-dark.png)
- Menus and right-click menu [light](redesign/windows/menus-light.png) and [dark](redesign/windows/menus-dark.png)
- All dialogs [light](redesign/windows/dialogs-light.png) and [dark](redesign/windows/dialogs-dark.png): add, file
  info, properties, rename, move, checksum, confirmation, queues, grab links, folders, connect a browser, options
  (general, connection, updates), about, keyboard shortcuts
- [Text sizes and compact rows](redesign/windows/text-sizes.png): 90 %, 115 %, 130 %
- [Before and after](redesign/before-after.png) (Linux, headless Chromium, fallback fonts)

## Separate copy (added for testing the redesign safely)

A file named `separate-copy` next to the program makes it behave as if it had been started with `-root` on its own
folder: its own data folder, single-instance lock and port; it never changes the Start menu entry, the
`pullwell://` link registration or Start with Windows (it can no longer switch the installed copy's entry off
either). The window title, tray tip and top bar say "Separate copy". Before this, a plain second copy in another
folder would have pointed the Start menu entry and the notification links at itself.

## Status and known issues

Done: everything in "What changed", the separate copy, the Windows window job, and the alignment fix it found.

Open:

1. With large text in a narrow window the File name column is squeezed and the list scrolls sideways (the other
   columns keep their widths; same as before the redesign).
2. The part line folds the engine's part map by count, not by bytes, so parts of very different sizes are shown
   approximately.
3. Notifications from a separate copy use the classic balloon (its clicks cannot reach it through `pullwell://`,
   which belongs to the installed copy). By design.
4. From before the redesign: the classic notification cannot
   be clicked on Windows 11 when the Start menu entry is off; the Firefox extension is only loaded temporarily.
5. Not covered by automation: a real Windows theme switch, dragging the window, display scaling above 100 %,
   toasts and the tray icon as Windows draws them (see "Results on Windows").
6. Test harness: `ext_check.mjs` needs its helper to stop the program when it prints STOP_PROGRAM; run without it,
   its last check fails on any build.
