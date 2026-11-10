# Pullwell: window, tray and notifications (design)

Status: **design for review, no code written yet.** Written 2026-11-10 against NetMerge `main` at commit
`0bfab33` (3.11.20) and `docs/convoy-handover.md` from that commit. Target: Windows 10 and 11, 64-bit (x64).

Legend: **[PC]** = needs a real Windows PC to test. **[here]** = can be checked in the cloud workspace (Linux, Go
1.24.7 installed): unit tests of the pure-Go parts, and a cross-compile with `GOOS=windows` that proves the Windows
code *compiles*, not that it *works*.

---

## 0. Decisions in one table

| # | Decision | Why |
| --- | --- | --- |
| D1 | The name lives in one file, `appname.go`. Everything else is derived from it: the window title, the data folder, the custom header (`X-Pullwell`), the mutex name, the toast app id, the link scheme (`pullwell://`) and the window classes. | A rename changes one constant. |
| D2 | One UI thread owns the window, the tray icon, WebView2 and the toasts, with one Windows message loop. The download engine never touches them; it sends messages to that thread. | WebView2 and Windows COM objects must be used from the thread that created them. One owner avoids a whole class of crashes. |
| D3 | **WebView2 without `WebView2Loader.dll`:** own small wrapper, standard library only. It finds the runtime the way the open-source loaders do (registry, then `EmbeddedBrowserWebView.dll`). No DLL is written to disk. | It keeps the "no third-party modules" rule. Dropping a DLL and loading it is the kind of thing antivirus products dislike (handover 7.7). Details in 4.1. |
| D4 | Fallback chain for the window: WebView2, then Edge/Chrome app mode (NetMerge's tested code), then the default browser. The page is told which one it is in. | The program must always open, even if WebView2 is missing or broken. |
| D5 | Single instance per data folder. A second start hands over to the first one with a window message (no network), and the first window comes to the front. | A test copy with its own data folder can still run next to the real one (lesson of NetMerge 3.11.12). |
| D6 | Closing the window hides it; the program stays in the tray. The web view is **closed** when the window hides and recreated on open, to give back its memory. | A download manager lives in the tray for days. A hidden web view keeps about 100 MB or more in its helper processes (to be measured **[PC]**). |
| D7 | Toasts are for the installed version only (as agreed). Toast buttons call back through a `pullwell://` link that carries a one-time random token. | It needs no background COM server, it works after the toast has moved to the Action Center, and the token stops web pages from using the link. Details in 6.4. |
| D8 | Notification setting becomes three-way: **All**, **Problems only**, **Off**. Default: All. The old `noNotify: true` reads as "Problems only". | Today `noNotify` silences only "finished", while "needs attention", "checksum does not match" and "downloads finished" ignore it. That's sensible, but the setting doesn't say so. Now it does. |
| D9 | No PowerShell anywhere. Shortcuts are made through the Windows shell API, and "Start with Windows" uses the per-user Run key (no scheduled task, no administrator). | Handover 7.7. NetMerge uses PowerShell for its desktop shortcut and to adjust its scheduled task; Pullwell copies neither. |

**D3 is the one decision that changes a stated default in a meaningful way, so here is the alternative:**
vendoring `github.com/wailsapp/go-webview2` (MIT, used by the Wails framework) at a pinned commit. Its default build
already uses a pure-Go loader with no DLL, but it brings `golang.org/x/sys` as a dependency and about 35,000 lines
of generated code. My choice is the own wrapper, using that library's code only as the reference for method order
and the runtime lookup (with credit in the source). If the own wrapper cannot be made stable on a real PC within a
reasonable effort, vendoring the library is the fallback plan, and I will tell you before doing it.

## 1. The name constant (D1)

```go
// appname.go: the only place the product name is written.
const AppName = "Pullwell"

var (
    appID      = AppName + ".App"                 // toast app id (AppUserModelID)
    appScheme  = strings.ToLower(AppName)         // pullwell://
    appHeader  = "X-" + AppName                   // required on every POST to the local API
    appDataDir = AppName                          // %LOCALAPPDATA%\Pullwell\data when installed
)
```

A test (**[here]**) fails if the string "Pullwell" appears anywhere else in Go code, except in `appname.go`, the
changelog and the docs. Copied `dm/` texts that say "NetMerge" (for example "Open NetMerge to cancel") are changed to
use `AppName`.

## 2. Threading model (D2)

```
main goroutine ── starts the local server, the download engine, then the UI thread
UI thread (runtime.LockOSThread, COM single-threaded) ── message loop for:
    hidden message window   (second-start hand-over, tray callbacks, "post to UI" calls)
    main window             (WebView2 inside)
    tray icon
    toasts
engine goroutines ── call ui.Post(func(){ ... }) to ask the UI thread to do something
```

- `ui.Post(f)` puts `f` on a queue and posts one message to the hidden window, which runs the queued functions.
- Every function run on the UI thread is wrapped so a panic is logged and the loop keeps running (same idea as
  `safeHandler`, rule 4.5.7).
- Windows callbacks are created once at start (`syscall.NewCallback` has a limit of about 2,000 for the whole
  process, so callbacks are never created per window, per toast or per event).

## 3. Single instance and second start (D5)

1. At start, create a named mutex `Local\Pullwell-<hash of the data folder>`.
2. If it already exists, this is a second start. Find the first instance's hidden window (class
   `PullwellMsg-<hash>`), call `AllowSetForegroundWindow` with the first instance's process id, send it the
   command line with `WM_COPYDATA` (for example "show", or a `pullwell://` link from a toast), and exit.
3. The first instance shows its window and brings it to the front.

Why `AllowSetForegroundWindow`: the second start is the program the person just launched, so Windows lets it hand
its right to take the focus to the first instance. This is the documented way, and it should avoid the
`AttachThreadInput` workaround NetMerge needed for Explorer (handover 7.4). That workaround stays available for
"Show in folder", where it was proven to work. **[PC]**

Tests: the parsing of the hand-over message **[here]**; two starts, a start while the window is minimised, a start
while another program has the focus **[PC]**.

## 4. The window (D3, D4, D6)

### 4.1 Starting WebView2 without the loader DLL

The open-source loaders do this, and I read the MIT-licensed Go port to confirm the steps:

1. Read the registry value `EBWebView` under
   `Software\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}` (the stable WebView2 runtime),
   first in `HKEY_LOCAL_MACHINE`, then in `HKEY_CURRENT_USER`, using the 32-bit registry view. The value is the
   runtime's folder, whose name is its version.
2. Refuse versions older than the minimum the loaders use (86.0.616.0).
3. Load `<folder>\EBWebView\x64\EmbeddedBrowserWebView.dll` and call its export
   `CreateWebViewEnvironmentWithOptionsInternal`.
4. From the environment, create the controller for our window, then the web view, and navigate to
   `http://127.0.0.1:<port>/`.

**Risk:** step 3 calls an entry point Microsoft does not document. Wails and the open-source loaders rely on it, so
it's widely used, but a future runtime could change it. Protection: any failure in steps 1-4 is logged with the
reason and the program falls back to Edge/Chrome app mode (D4). It never fails to open.

The web view's own data (cache, local storage) goes in `data\webview`. Settings: no developer tools unless started
with `--debug`, no status bar, default context menu kept for copy and paste.

### 4.2 What the window allows

- Only pages from `http://127.0.0.1:<port>/` load inside the window. Any other link (a help page, the GitHub
  releases page) opens in the default browser. A request for a new window does the same.
- The page already sends the custom header and a local `Origin`, so the local API guard (handover 4.6) works
  unchanged.
- The page can ask `GET /api/app/info` which host it runs in (`webview2`, `app-mode` or `browser`) and show a
  one-line note in the last two cases.

### 4.3 Window behaviour

| Action | Result |
| --- | --- |
| Start normally | Window opens. |
| Start with Windows (`--tray`) | Tray icon only, no window. |
| Close (X) | Window hides, web view closes, downloads continue. The first time only, a balloon or toast says "Pullwell is still running in the notification area." |
| Tray click, "Open" in the tray menu, second start | Window shows, web view is recreated if it was closed, window comes to the front. |
| Quit (tray menu) | Engine stops and saves its state (at most 5 seconds), tray icon is removed, web view closes, program exits. No confirmation; unfinished downloads continue at the next start. |

- Size and position are remembered in `data\window.json`. On load they're checked against the monitors that exist
  now. A position on a monitor that has gone becomes centred on the main monitor. Impossible sizes (smaller than
  720x480, larger than the screen) are replaced (rule 4.5.5).
- Per-monitor DPI awareness is set at start, so the window is sharp on high-DPI screens. **[PC]**
- The title bar follows the Windows light or dark setting (nice to have). **[PC]**

Measure on the PC **[PC]**: memory of the hidden program with the web view closed, and the time from tray click to
a usable window. If reopening takes more than about a second, keep the web view alive instead and accept the memory.

### 4.4 Fallbacks

- **Edge/Chrome app mode:** NetMerge's tested `openWindow` code, with its own profile folder in
  `data\ui-profile`, and focus through NetMerge's `focusExistingWindow` (matched by window class and the title
  "Pullwell").
- **Default browser:** last resort, as in NetMerge.

## 5. The tray icon

A port of NetMerge's `sys_tray_windows.go`, which is tested on real PCs, including putting the icon back after
Explorer restarts.

- Icon: the real program icon (embedded `.ico`), with a second variant shown while something is downloading.
- Tooltip: `Pullwell - 3 downloading, 4.2 MB/s` or `Pullwell - nothing downloading` (Windows allows 127
  characters).
- Click: open the window. Right-click menu: **Open Pullwell**, **Pause all** or **Resume all**, **Quit Pullwell**.
- Clicking a balloon opens the window with the related download selected.

Tests: the tooltip text for different states **[here]**; everything else **[PC]**.

## 6. Notifications (D7, D8)

### 6.1 What the engine reports

`Host.Notify(title, text)` becomes `Host.Notify(n Notice)`, a change to Pullwell's copy of `dm/` only:

```go
type Notice struct {
    Kind   string // "finished", "attention", "checksum", "allDone", "shutdown"
    Title  string // plain words, already written by the engine today
    Text   string
    ItemID string // empty for "allDone" and "shutdown"
}
```

The engine already has the four places that notify (finished, needs attention, checksum does not match, downloads
finished with the power action). Only their call changes. The engine checks the setting in one place:
"Problems only" lets `attention`, `checksum` and `shutdown` through.

### 6.2 No notification storms

Notices are collected for **3 seconds** before anything is shown.

- One finished download: "Download finished" with the file name and buttons **Open** and **Show in folder**.
- Several: "4 downloads finished" with the first three names and one button **Show list**.
- Problems are grouped the same way, separately from finished ones, and never hidden by them.
- Each toast has a tag, so a new group replaces the previous one instead of piling up.

This logic is pure Go, so it's fully testable **[here]** with a fake clock.

### 6.3 How a toast is shown (installed version)

- The installer creates a Start-menu shortcut carrying the app id (`Pullwell.App`) as a shortcut property, through
  the Windows shell API (no PowerShell). The program also sets the same id on itself at start, so its taskbar button
  and its toasts belong together.
- The toast is shown through the Windows Runtime notification API, called directly from Go (the
  `ToastNotificationManager` for our app id, an XML document, `Show`). All text from file names and servers is
  XML-escaped.
- If anything fails (portable copy, no shortcut, an API error), the same notice becomes a tray balloon. The failure
  is logged once, not on every notice.
- Windows' own "Do not disturb" and notification settings apply on top, and Pullwell doesn't fight them.

Example of the XML for one finished download:

```xml
<toast launch="pullwell://show?item=d1762741234-7&amp;t=9f2c41a0d3" activationType="protocol">
  <visual><binding template="ToastGeneric">
    <text>Download finished</text>
    <text>ubuntu-24.04.iso (5.8 GB)</text>
  </binding></visual>
  <actions>
    <action content="Open" activationType="protocol" arguments="pullwell://open?item=d1762741234-7&amp;t=9f2c41a0d3"/>
    <action content="Show in folder" activationType="protocol" arguments="pullwell://reveal?item=d1762741234-7&amp;t=9f2c41a0d3"/>
  </actions>
</toast>
```

### 6.4 Toast buttons and the `pullwell://` link (security matters here)

The installer registers `pullwell://` for the current user, pointing to `Pullwell.exe "%1"`. A click starts a short
second copy, which hands the link to the running program (section 3) and exits. If Pullwell isn't running, it
starts and handles the link.

**Any web page can also try to open a `pullwell://` link** (the browser asks the person first, but people click
"Allow"). Download ids are predictable (`d<time>-<number>` in `dl_manager.go`), and "Open" runs a downloaded file,
which may be a program. So:

- Every toast gets a **one-time random token** (16 random bytes from the system's secure random source). The program
  remembers which item and which actions it allows, for 24 hours, in memory only.
- A link without a valid token only shows the window. It never opens or reveals anything.
- Even with a valid token, **Open** never runs a program file directly (`.exe`, `.msi`, `.bat`, `.cmd`, `.ps1`,
  `.vbs`, `.js`, `.scr`, `.lnk` and similar). For those it shows the window with the item selected, and the person
  opens it from there. **Show in folder** is always allowed with a valid token.
- Only the verbs `show`, `open`, `reveal`, `list` and `cancel-shutdown` exist. Anything else, or anything over 512
  characters, is ignored and logged.

The parser and the token checks are pure Go and fully testable **[here]**, including links made by an attacker.

### 6.5 The shutdown warning

When "shut down after downloads" fires, the toast says "The PC shuts down in one minute" with a **Cancel
shutdown** button (`pullwell://cancel-shutdown`, with a token). The window's existing cancel button stays.

## 7. Installed and portable side by side

| | Installed (per user, no administrator) | Portable zip |
| --- | --- | --- |
| Data folder | `%LOCALAPPDATA%\Pullwell\data` | `data` beside the exe |
| Window, tray, single instance | yes | yes |
| Toasts with buttons | yes | no, tray balloons |
| `pullwell://` registered | yes | no |
| Start with Windows | per-user Run key | per-user Run key, refused from a copy with a custom data folder |
| Registry use | shortcut, link scheme, Run key, uninstall entry | Run key only, and only if switched on |

The installer and the updater get their own design next. This document only fixes what they must provide (the
shortcut with the app id, and the link registration).

## 8. Files in the new repository

| File | Content | Tested |
| --- | --- | --- |
| `appname.go` | D1 | [here] |
| `notify_group.go` | grouping and storm control (6.2) | [here] |
| `applink.go` | `pullwell://` parsing, tokens, allowed actions (6.4) | [here] |
| `wingeom.go` | checking the saved window position (4.3) | [here] |
| `win_ui.go` | UI thread, message loop, `ui.Post` | compile [here], run [PC] |
| `win_instance.go` | mutex, hand-over, focus (section 3) | compile [here], run [PC] |
| `win_tray.go` | tray (section 5) | compile [here], run [PC] |
| `win_webview.go` | WebView2 lookup and wrapper (4.1, 4.2) | compile [here], run [PC] |
| `win_toast.go` | toasts (6.3) | compile [here], run [PC] |
| `win_appmode.go` | Edge/Chrome fallback (4.4) | compile [here], run [PC] |

## 9. Steps (each small, tests after each)

1. Repository skeleton: `go.mod` (module `pullwell`, Go 1.24), MIT licence, README, CHANGELOG, `VERSION` 0.1.0,
   CI that runs vet, tests and a Windows build. `appname.go` with its test. **[here]**
2. Copy `dm/` from NetMerge 3.11.20 (commit `0bfab33`), unchanged, and run its full test suite. **[here]**
3. The pure-Go parts: grouping, link parsing with tokens, window-position check, with tests. **[here]**
4. UI thread, single instance and tray, with the window still opened in Edge/Chrome app mode. Release 0.1.0 for the
   Windows Sandbox. **[PC]**
5. WebView2 window with automatic fallback. **[PC]** Best done by a stronger model (see section 10).
6. Toasts and the `pullwell://` link, which depend on the installer (its own design). **[PC]** Stronger model.

For each **[PC]** step I'll write a short checklist for you or Codex, in the format of the existing test plan,
and report each line as PASS, FAIL or BLOCKED with the reason. Nothing will be reported as working until someone
has seen it work on Windows.

## 10. Where a stronger model or your PC is needed

- **Stronger model:** steps 5 and 6. Both call Windows COM interfaces by hand from Go. A wrong method order or
  interface id doesn't fail to compile; it crashes at runtime. A Sonnet-class model can do steps 1-4.
- **Your PC (or the Sandbox):** everything marked **[PC]**. The Windows Sandbox is enough for steps 4 and 5. Toasts
  (step 6) should also be tried on the real PC, because notification settings and "Do not disturb" differ there.
- **Not testable in the Sandbox:** "Start with Windows" (the Sandbox doesn't keep a login).

## 11. Bug check of the copied code

Nothing to report to NetMerge from this part. Two things I looked at and found safe in NetMerge:

- Download ids are predictable, but NetMerge's local API only answers requests with its custom header and a local
  origin, so a web page can't use them. They only matter for Pullwell's new `pullwell://` link, which 6.4 handles.
- Only "finished" respects `noNotify`; the other notices ignore it. This looks intended (problems should still be
  shown), so it's a wording issue for Pullwell (D8), not a NetMerge bug.

Separately, NetMerge's desktop shortcut and its scheduled-task adjustment use hidden PowerShell. That's a NetMerge
choice, not a bug in the download code; Pullwell simply won't copy it.
