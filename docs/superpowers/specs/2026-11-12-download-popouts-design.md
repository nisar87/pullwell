# Per-download pop-out windows

Add an IDM-style independent progress window for each download, available from the progress dialog and the download context/menu commands. Keep the existing inline progress dialog and all transfer operations.

Each window owns one download ID and shows the existing progress, speed history, connections, file path, recent events, and Pause/Resume/Open controls. It updates independently of the main window, survives closing the main view, and closes without changing the download. Reopening a download focuses its existing window. Different downloads have independent windows.

## Approach

Use a dedicated presentation mode at `/?progress=<id>` and share the progress markup and controls with the existing dialog. Browser/app mode uses a named `window.open` popup from an explicit user action, with a useful blocked-popup fallback. The Windows WebView2 UI host recognizes this exact local route and creates a small independent WebView2 window using its existing environment. Native windows are keyed by download ID and released when closed.

A browser-only popup would be sent to the default browser by the current desktop host. Replacing the full native window system would add unnecessary risk. A focused additional Windows UI window is the selected implementation choice.

## Boundaries

- Keep branch `feature/frontend-modernization` and preserve all earlier work.
- Download engine, server, API handlers/contracts, native file launching, updater, persistence, and extension remain unchanged.
- Windows UI-host code may change solely to create, focus, size, and close progress windows. This is UI work; it does not change the stable backend.
- No dependencies, polling endpoints, or transfer state stores are added.
- A pop-out must not consume pending browser/notification add requests, change tray language, run main-window shortcuts, or open other downloads' completion dialogs.
- Preserve light/dark themes, text scaling, Urdu/RTL, keyboard focus, and reduced motion. Provide loading, disconnected, removed-download, and completed states. A removed download must not retain actionable stale controls or sensitive stale details.

## Acceptance

1. Explicit pop-out actions open a real separate window; the main workspace remains usable.
2. One window per ID, multiple IDs independently, title reflects filename/progress, and popup data refreshes without its opener.
3. Pause/resume use existing APIs; completion offers Open and Open folder; closing never pauses, removes, or quits.
4. Properties/confirmation dialogs work over the pop-out and return to its progress view.
5. Browser blocking keeps the inline progress dialog and explains how to continue.
6. Native routing accepts only the application's exact local origin/path and a bounded valid ID; external links retain existing handling.
7. Closing during asynchronous native controller creation, reopening, process failure, resize/DPI changes, and application shutdown release resources safely.
8. Focused browser tests, existing progress/modernization checks, accessibility scans, Go tests, and Windows cross-build pass, with unavailable Windows runtime verification reported honestly.
