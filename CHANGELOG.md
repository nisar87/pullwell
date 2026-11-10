# Changelog

## 0.1.0 (not released)

- **Start with Windows** (Options → General): one entry in the current user's startup list, no administrator rights,
  no scheduled task. Pullwell then starts in the notification area. A copy started with its own data folder
  (`-root`) refuses, so a test copy cannot replace the real one.
- Notifications are grouped: downloads that finish within a few seconds of each other give one notification
  ("4 downloads finished") instead of one each, and problems are shown separately.
- **Browser extension** for Chrome, Edge, Brave and Firefox (from NetMerge, renamed): big downloads and "Download
  with Pullwell" from the right-click menu go to Pullwell; if Pullwell is not running the browser downloads as
  usual. The program carries the extension and offers it in **Options → Browser → Connect a browser**, with the
  pairing code, so the extension always matches the program. The extension finds Pullwell on its fixed ports
  (9197, then 9297, 9397, 9497) and checks it is really Pullwell before pairing. Connected browsers are listed
  there and can be disconnected; the size limit and an off switch are in the same tab.
- **Its own window.** The program now opens in a normal Windows window with Microsoft's WebView2 inside (built into
  Windows 10 and 11), instead of an Edge or Chrome "app" window. It needs no extra DLL and no third-party code. If
  WebView2 is missing or fails, it falls back to Edge or Chrome by itself, and the log says why. The window remembers
  its size, place and whether it was maximised, follows the screen's scaling, has a dark title bar when Windows is
  dark, and its own taskbar entry. Closing it keeps the program in the notification area (a one-time notice says
  so). Links that open a new window go to the default browser, web links only. `-ui app` or `-ui browser` forces
  the old ways, `-debug` allows the developer tools.
- **A compact main window in the style of IDM** instead of one long page: a toolbar (Add URL, Resume, Stop, Stop all,
  Start all, Delete, Delete completed, Scheduler, Grabber, Options), categories and queues on the left, a table of
  downloads (name, size, status with a progress bar, speed, time left, date added, description) and a status bar
  with the total speed. Columns can be sorted and resized. Select with click, Ctrl+click, Shift+click or the arrow
  keys; right-click for Open, Open folder, Resume, Stop, move in the queue, queue, speed limit, copy link,
  properties and delete. Keys: Ctrl+N or Insert (add), Ctrl+V with a link copied (add it), Enter (open),
  Alt+Enter (properties), Space (stop or resume), Delete, Shift+Delete (with the file), Ctrl+A, Ctrl+F.
  Links and .torrent files can be dropped onto the window. Options are in one dialog with tabs. Light and dark
  follow Windows, or can be chosen.
- **The first program you can run (Windows).** It starts, shows the downloads page in its own window (Edge or
  Chrome in app mode for now; the built-in WebView2 window comes later), sits in the notification area, and
  downloads. Closing the window keeps it running; the tray icon opens it again, shows what is downloading, and has
  Pause all, Resume all and Quit. Starting it a second time brings the open window to the front instead of
  starting a second copy. Finished downloads show a notification next to the tray icon. Data is kept in a `data`
  folder next to the program.
- The local page and API listen on 127.0.0.1 only, port 9197 (9297, 9397 or 9497 if it is taken). They refuse
  requests addressed to any other name, changes without the program's own header, and calls from other web pages.
- The engine's messages name the program instead of NetMerge, and the page drops NetMerge's per-connection settings
  (one connection here; NetMerge, if it runs, merges lines underneath). The browser extension comes later.
- Repository started: licence, README, design of the window, tray and notifications, and the product name kept in
  one place (`appname.go`) so a rename is cheap.
- The download engine (`dm/`) copied from NetMerge 3.11.20 (commit `0bfab33`), including the 3.11.20 fix:
  when a server refuses a download and no referer was given, it asks once more with the site's own address as the
  referer. Its few differences from NetMerge's copy are listed in `docs/engine-sync.md`.
- The parts of the window, tray and notification design that need no Windows, each with tests:
  - `pullwell://` links from toast buttons. Only five actions exist; everything except "show" needs a one-time
    random token that Pullwell itself put in the toast, so a web page cannot use the link to open your files; and a
    program file (`.exe`, `.msi`, scripts and similar) is never opened from a link.
  - Notification grouping: downloads that finish together give one notification ("5 downloads finished"), and
    problems are shown separately. New setting levels: all, problems only, off.
  - The saved window position is checked against the monitors that exist, so the window never opens off screen.
