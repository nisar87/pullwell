# Changelog

## 0.10.2 (2026-11-13)

- The name Pullwell is final: the README no longer calls it a working name.

## 0.10.1 (2026-11-13)

- Help → About now links to the developer's site, [nisarworks.com](https://nisarworks.com/), where the other free
  tools are listed (with an Urdu translation of the line).

## 0.10.0 (2026-11-12)

- **A refreshed workspace.** Warm light and dark themes, clearer navigation and file icons, a live transfer summary,
  useful empty states, tidier settings and dialogs, and responsive layouts that keep controls practical in smaller
  windows. Comfortable and compact rows remain available, with restrained animations that respect reduced motion.
- **A separate progress window for each download.** Click the arrow in a download's progress view, or choose
  **Open in separate window** from its right-click or Downloads menu. Each window shows live progress, speed,
  connections and recent events, with its own Pause, Resume, Open and Properties controls. Reopening the same
  download restores its existing window. Closing a progress window or the main view leaves downloads running.
- **Clearer recovery and keyboard behavior.** Progress windows recover after a lost connection and clear their
  details and related dialogs if the download is removed. Dialogs contain and restore keyboard focus, settings tabs
  support arrow keys, progress indicators expose their values, and controls have accessible names.
- **A matching browser popup.** Clear loading, connected, pairing and unavailable states, with the same visual
  language as the application. Existing browser download handoff and permissions are unchanged.
- English and Urdu cover the new interface, including larger text and right-to-left layouts. File names, URLs and
  queue names stay literal. The download engine, server and API behavior are unchanged.

## 0.9.3 (2026-11-12)

- Fixed: a download that the browser kept starting again (a page that retries after the browser's download was taken
  over) brought the "Download file info" box back every time you pressed Cancel. A link you cancel in that box is now
  not offered again for three minutes when the browser starts it by itself. A link you choose by hand (right-click
  menu, the button on a video) is never held back. Needs the extension from 0.9.3 (it tells the program which
  downloads the browser started by itself).

## 0.9.2 (2026-11-12)

- Test-only: the Windows check "a link on the command line reaches the running copy" no longer fails at random; it
  waits for the hand-over and accepts the window taking the link first. The log now says when the window took it.

## 0.9.1 (2026-11-12)

- **Languages: English and اردو only.** The first release carried five more languages (Español, Français, Deutsch,
  Português, العربية). They are removed: every language has to be proofread and tested on every change, and that is
  more than one person can keep up with. Choosing one of them now falls back to English. The translation system
  is unchanged, so another language can be added later by adding one file.

## 0.9.0 (2026-11-12)

- **Languages.** Español, Français, Deutsch, Português (Brasil), العربية and اردو (right-to-left, mirrored layout) for the
  whole window: menus, dialogs, Options, messages and errors. Choose it in View → Language or Options → General;
  "Automatic" follows the browser's language. Text that has no translation stays in English, never blank. The
  tray menu and the Windows notifications follow the window's language too, and the browser extension (its popup and
  the button on videos) follows the browser's language. The translations were written by Claude and have not been
  reviewed by native speakers yet: wording corrections are welcome.
- The toolbar drops its labels earlier in the translated languages so everything still fits.
- Dates and times in the list follow the chosen language.
- Checks: every language is tested on GitHub's Windows runners (translated menus and buttons, no English left in any Options
  tab, right-to-left for Arabic and Urdu, the choice kept after a restart), and a missing or mismatched translation fails
  the build.

## 0.8.0 (2026-11-12)

- **Saved logins per website.** Options → Connection → Manage logins: type a website, a user name and a password once.
  Every download from that website and its subdomains then uses it, also the "Download file info" check, and it is
  never sent to another website (not even to a mirror). The password is kept in `data/logins.json` sealed with your
  Windows account (it is useless on another PC or for another Windows user) and is never shown again.
- **Speed limit for one download, any number.** Properties and Add with options have a "Speed limit, KB/s" field (the
  right-click menu still has its presets).
- **Metalink.** A `.meta4` or `.metalink` link (typed, pasted, or right-click → Download with Pullwell) adds every file
  it lists: the best address first, the others as mirrors, with its checksum (SHA-256, SHA-1 or MD5) and its name.
  Both Metalink versions 3 and 4 are read.
- A link that is already in the list as a mirror of another download now counts as a duplicate.

## 0.7.5 (2026-11-11)

- **Only one highlighted row in menus.** In the menu bar and the right-click menu, a row could stay highlighted
  (the first one, or the one that opened a submenu) while the row under the mouse was highlighted too, so two or three
  rows were green at once, mostly in menus with a submenu. The highlight now follows the mouse and the keyboard
  together, so there is one green row; a menu title or submenu parent that is open is only tinted grey.

## 0.7.4 (2026-11-11)

- **One word for pausing a download.** The toolbar and right-click menu said "Stop", the list said "Paused", the
  progress window said "Pause" and the bulk buttons said "Stop all" / "Start all". It is now Pause, Paused and Resume
  everywhere: Pause, Pause all, Resume, Resume all (also in the keyboard help, the warning for sites that cannot
  resume, the queue dialog and the quit question).

## 0.7.3 (2026-11-11)

- **Update check no longer fails with "GitHub answered HTTP 403".** GitHub limits anonymous requests to its programming
  interface now and then, so the check failed at random times while the window still said "You have the newest
  version". The check now falls back to GitHub's ordinary release page, which has no such limit, and when it cannot
  check at all the window says "Could not check for updates" instead of claiming you are up to date.

## 0.7.2 (2026-11-11)

- **Sites that only let a real browser in (Cloudflare and similar).** testfile.org answers Pullwell with HTTP 403 from
  Cloudflare however the request is made, while the browser is let in; Pullwell cannot get past that. Before, a
  right-click "Download with Pullwell" on such a link did nothing. Now the browser downloads it itself (the extension's
  badge shows B), and so does the video button. A download the browser starts by itself on such a site stays with the
  browser, as in 0.7.1.

## 0.7.1 (2026-11-11)

- **Sites that refuse Pullwell (for example testfile.org).** When a site answers "refused" (HTTP 401/403), Pullwell
  now asks again the way a browser does (with the site's own address as the page it came from, then browser-style
  headers, then without a range request) before giving up. The message now shows the HTTP status and server, so the
  cause is visible. For a browser download, if the site still refuses Pullwell, the browser keeps its own download
  instead of the download being lost.
- **No download button on YouTube.** YouTube plays video in small separate pieces, so the button could only say "No
  video link found" there. It no longer appears on YouTube.

## 0.7.0 (2026-11-11)

- **Ask before a browser download starts.** A download the browser extension takes over now opens the "Download file
  info" box first (name, folder, size, Start download / Download later / Cancel), like IDM, so a wrong click does not
  start a download. Options -> Browser -> "Ask before a browser download starts" turns it off. The box keeps the
  browser's sign-in details, so links that need them still work.
- **A download button on videos** in the browser (extension): a small "Download with Pullwell" button appears over a
  video when the mouse is on it. It works for direct video links and for streams the page loads (HLS). It needs the
  extension's "Allow for all sites" and can be switched off in the extension's popup. Reload the page after
  installing or changing it.
- **Steady window sizes.** The Options window is the same size on every tab (it used to grow and shrink with each
  tab's content), and all dialogs now use one of four widths, so the window does not change size as you move between
  related dialogs (Add, Checking the link, Download file info).
- Browser extension: a download of unknown size whose link has no file ending is taken over by its content type
  (included since 0.6.2).

## 0.6.2 (2026-11-11)

- Fixed: the browser extension did not take over a download when the site did not announce its size and the link had
  no file ending (for example a "Save link as" on testfile.org/files-5GB), so the browser saved the file itself. Such
  a download is now taken when the site says it is an archive, a program, a disc image or a video.

## 0.6.1 (2026-11-11)

- Fixed: the speed limit list said "250 KB/s" for a limit that is really 256 KB/s (limits count in binary units, as
  the sizes do); it now says 256 KB/s everywhere.
- Fixed: Uninstall with "also delete data" could leave the data folder behind when the window's helper processes were
  slow to let go of it; it now tries six times instead of three.
- Tests only: steadier Windows checks (Space to resume, uninstall).

## 0.6.0 (2026-11-11)

- **New look, "Precision flow".** A redesigned window: calmer light and dark themes, Segoe UI Variable text with
  Bahnschrift numbers, a flow line showing how each download is split into parts, a clearer progress window with a
  speed graph, and tidier menus and dialogs. View -> Row spacing switches between comfortable and compact rows.
- **Separate copy**: a copy of Pullwell with a `separate-copy` file next to it keeps its own data and never touches
  the Start menu entry, the `pullwell://` link or Start with Windows, so it can be tried next to the installed one.
- Tests: the real window, the tray icon, finished-download notifications and the browser extension are now checked on
  a Windows machine at GitHub, including 150 % and 200 % screen scales.
- Fixed: Uninstall could leave the data folder behind when the program was still closing.
- Fixed: a timing check of the download engine that failed at random on Windows (tests only).

## 0.5.0 (2026-11-10)

- **Downloads that cannot resume are marked.** Some websites do not allow continuing a download; stopping one throws
  away what is downloaded. Stop (and Stop all) now asks first for such a download, and its progress window says
  "Can resume: No".
- **View → Text size** (Small, Normal, Large, Larger), for big or high-resolution screens; menus and dialogs fit.
- A **Website** column (View → Columns), off at first.
- Fixed (found by the new Windows check): **Start with Windows** failed on a Windows user profile that had no
  startup list yet; it is now made when missing.
- Fixed: Uninstall right after Pullwell started could leave the Start menu entry behind.
- **Open when finished**: a tick in the "Download file info" dialog (remembered for the next download), or
  right-click → Open when finished. A program or installer is never started by itself: it is shown in its folder
  instead. A file with a checksum is only opened when the checksum matched.
- **Move to another folder** (right-click, or the Downloads menu): moves a finished file, also to another drive.
- **Check the checksum** of a finished file afterwards: paste the checksum from the website.
- Unfinished files are now called `name.pullwell-part` (and `.pullwell-state`) instead of `.nmpart` and `.nmdl`;
  downloads that were running during the update are taken over and continue.
- Tests now also run on a Windows machine at GitHub, which starts the real program and checks Start with Windows, the
  Start menu entry, the link registration, the self-update and Go back, keeping the PC awake, a protected folder and
  Uninstall.
- Pullwell is a standalone program: it needs nothing from NetMerge and no longer follows NetMerge's copy of the
  download engine. The engine notes about keeping the two in step are removed. A download list saved by NetMerge can
  still be loaded (Tasks → Load a list), so existing downloads can be moved over.
- Anonymous FTP logins give "pullwell@" instead of "netmerge@" as the password.

## 0.4.1 (2026-11-10)

- The "Download this?" offer for a copied link is a real Windows notification with a **Download…** button. The
  classic notification used before could not be clicked on Windows 11.
- README: the features added in 0.3 and 0.4.

## 0.4.0 (2026-11-10)

- **Real Windows notifications.** "Download finished" now has **Open** and **Open folder** buttons, and a click on
  a notification brings up Pullwell (several finished downloads: the Finished list). They also stay in the
  notification centre. The buttons work through `pullwell://` links that only Pullwell itself can make valid (each
  carries a one-time code), and a program file is never opened from a notification. Without the Start menu entry
  Windows does not allow such notifications, and the classic ones are used.
- The empty list offers **Connect a browser**.
- A second copy of a file keeps double endings together: `go.src (2).tar.gz` instead of `go.src.tar (2).gz`.
- **Show a finished download** (Options → General, off by default): when a download finishes, the window comes up
  with that download, Complete, and Open and Open folder buttons, also when the window was closed.
- Help → **What's new** (the release notes) and **Report a problem** (GitHub, with the version filled in).
- A full disk says how much space the file needs and how much is free.
- When the program's folder is protected (Program Files, for example), the data goes to your own app data folder,
  and Updates explains why it cannot replace itself there.
- A failed automatic update waits an hour before it is tried again (it was tried every 15 seconds).
- While a download waits to try again (the site did not answer), the list keeps showing how much was already
  downloaded instead of 0%; it continues from there.

## 0.3.0 (2026-11-10)

- **The PC stays awake while downloading** (Options → Schedule, on by default): Windows' sleep timer no longer
  stops a long download. The screen can still turn off, and the PC may sleep again when nothing downloads.
- **Proxy.** Options → Connection → Proxy: "Same as Windows" (the default; what Settings → Network → Proxy says,
  including the list of addresses that go direct), "No proxy", or "This proxy" (host:port, or socks5://host:port,
  with user:password@ if the proxy asks for it). Downloads and the update check use it; **Test** checks it works.
  An automatic proxy setup script is not read; Options says so, and you can type the proxy instead.
- **Rename** a download (F2, or right-click → Rename): a finished file is renamed in its folder; one that has not
  started yet is saved under the new name.
- **Speed limit at a click**: click the speed in the status bar, or Tools → Speed limit for all downloads.
- The tray menu has **Add a download** (with the copied link already in it, when there is one) and **Open the
  download folder**.
- `Pullwell.exe <link>` opens the Add dialog with that link (also when Pullwell is already running).
- **Save the list** now writes the file into your download folder and shows it; in the program's own window the
  old way saved nothing.
- Loading a list whose downloads are all already there says so, instead of showing an error.
- Fixed: a numbered series typed in the Add dialog (`file[001-050].jpg`) was checked as one literal link and
  failed; it is now added as a series.
- Confirmations (shut down when finished, delete a queue, run a program after each download) are Pullwell dialogs
  instead of browser boxes.

## 0.2.0 (2026-11-10)

The first public version. Download `Pullwell.exe`, put it in a folder of its own and start it; from then on it
updates itself. See the README for connecting your browser.

- **A file name you type is kept.** A name you type in the "Download file info" dialog was replaced by the website's
  own name whenever the website suggested one. This is fixed (it was inherited from NetMerge's engine and is reported
  there).
- **Connect a browser** saves the extension straight into your download folder and opens it, instead of relying on
  the window to download the file.
- Notifications show Pullwell's own icon.
- **Looks installed.** The program has its own icon (in Explorer, the taskbar, Alt+Tab and the title bar) and version
  details (Explorer → Properties → Details, Task Manager). Pullwell adds itself to your Start menu, so Windows search
  finds it and notifications show "Pullwell" with its icon; the entry follows the program if you move its folder,
  and can be switched off in Options → General. **Options → Advanced → Uninstall** removes the Start menu entry, the
  startup entry and the program file, and on request the download list, settings and log; downloaded files are
  never touched. No administrator rights are needed for any of it. The icon and version details are generated by
  `go run ./scripts/mkres` (plain Go, no extra tools).
- **Progress window** for a download: double-click (or Enter) on an unfinished download, or "Progress" in the
  right-click and Downloads menus. It shows the percentage, size, what is downloaded, speed, time left, the number of
  connections, the parts of the file and the latest events, and refreshes while it is open. Pause and Resume work
  from it; when the download finishes it says Complete and offers Open and Open folder.
- **Offer links I copy** (Options → General, off by default): copying a link to a file (program, archive, video,
  PDF and so on) or a magnet link anywhere in Windows brings a notification "Download this with Pullwell?";
  clicking it opens the Add dialog with the link. Nothing is downloaded without confirming, each link is offered
  once, and links copied inside Pullwell's own window are not offered. Windows tells Pullwell about clipboard
  changes; it does not keep reading the clipboard.
- **Go back** now sticks: the version you went back from is not installed by itself again (you can still install it
  by hand), and the restart says "went back" instead of "updated".
- Two more failures explained in plain words: a file name or folder path too long for Windows, and a download
  folder that is not there any more (a removed drive).
- **Menu bar** at the top of the window: Tasks (add a link, add with options, torrent file, grab links from a page,
  load or save the list, quit), Downloads (open, open folder, resume, stop, download again, start/stop all, queue,
  speed limit, copy link, properties, select all, delete), View (categories panel, toolbar labels, status bar,
  columns, sort, show, theme, refresh), Tools (scheduler and queues, folders, options, connect a browser, open the
  download or log folder) and Help (keyboard shortcuts, check for updates, about). It works as in other Windows
  programs: Alt or F10 goes to it, Alt+T/D/V/O/H opens a menu, arrows move (also into submenus), Enter chooses,
  Escape closes; with the mouse, moving across the titles switches menus. Entries that do not fit the selection are
  greyed out. Columns can now be hidden, and **Download again** fetches a finished or failed file once more (the old
  file is kept).
- **Updates itself.** Install once; new versions are installed by Pullwell, which then restarts. Options → Updates
  shows the version, checks, installs, and can go back to the previous version. By default an update is installed
  by itself when nothing is downloading (unfinished downloads continue afterwards); it can be switched to "ask
  first". Updates come from GitHub releases (or test builds) once the project is public, and, while testing, from a
  `Pullwell-update.exe` placed next to the program. Every update is checked against its SHA-256 fingerprint. No
  script or PowerShell is used: Pullwell renames its own file to `Pullwell-previous.exe`, puts the new one in its
  place, starts it and closes; the new copy waits until the old one has ended.
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
  referer.
- The parts of the window, tray and notification design that need no Windows, each with tests:
  - `pullwell://` links from toast buttons. Only five actions exist; everything except "show" needs a one-time
    random token that Pullwell itself put in the toast, so a web page cannot use the link to open your files; and a
    program file (`.exe`, `.msi`, scripts and similar) is never opened from a link.
  - Notification grouping: downloads that finish together give one notification ("5 downloads finished"), and
    problems are shown separately. New setting levels: all, problems only, off.
  - The saved window position is checked against the monitors that exist, so the window never opens off screen.
