# Download pop-out verification

November 12, 2026 — `feature/frontend-modernization`, based on `4b4450a`.

This adds separate per-download progress windows to the [frontend modernization](2026-11-12-frontend-modernization.md). Open one with the arrow in a download's progress dialog, or choose **Open in separate window** from its context menu or the Downloads menu. Reopening the same download reuses its window. Different downloads have independent controls and live data.

## Implementation and boundaries

- `ui/progress-window.js` owns the standalone `/?progress=<id>` view and browser window reuse. Progress content and controls use the same renderer and existing APIs as the inline dialog.
- Closing either the progress window or its main browser view leaves downloads running. Pop-outs do not consume pending add requests or change the tray language.
- Properties and confirmation dialogs return to the progress view. Completed downloads offer Open and Open folder. Removed downloads clear their details and actions. Disconnected windows disable stale actions and recover on reconnection.
- Light/dark themes, reduced motion, text scaling, English/Urdu and keyboard operation use the existing design system. Filenames and URLs stay literal.
- `progress_windows.go` creates Windows WebView2 UI windows with per-ID reuse, independent resizing, native titles, close handling and COM cleanup. `progress_route.go` validates the exact local origin, root path, single query parameter and generated download ID.
- Changes to `webview_windows.go` route progress requests and coordinate UI shutdown/process failure; the single tray change calls UI cleanup. These are desktop UI changes. The download engine, server, APIs, persistence, native file launching, updater and extension behavior remain unchanged by this feature. No runtime dependency was added.

## Results

| Check | Result |
| --- | --- |
| Missing-feature browser control | Failed before implementation as expected |
| Real download pop-out checks | 32/32 pass, zero runtime errors |
| Existing progress dialog | 12/12 pass |
| Modernization regression checks | 49/49 pass |
| Shared UI accessibility scans | 34/34 surfaces pass, zero script errors |
| Removed-download accessibility regression | Failed for an unnamed unavailable action before the fix; passes after hiding unavailable actions and restoring focus |
| Localization generation | 690 application phrases; 41 extension phrases |
| JavaScript syntax / whitespace | Pass |
| Strict native route tests | Red → green in the isolated worker; integration race check passes |
| Combined Go tests | Pass; integrated engine suite completed in 410 seconds, and final application tests also pass |
| Linux and Windows Go vet | Pass |
| Windows amd64 build | Pass, `/tmp/Pullwell-popouts.exe` |
| Independent review | Pass after two rounds; all three findings resolved |
| Real Windows/WebView2 pop-outs | 22/22 pass in GitHub Actions |
| Complete Windows window suites | 225 passing assertions across eight suites; zero failures |

The browser checks use actual transfers from an isolated local fixture server, including resumable, completed and non-resumable files. Network aborts verify disconnection/recovery; a blocked-window stub verifies the fallback without changing production behavior. Test downloads and application data stay under `/tmp`.

The final review reproduced two issues: cached native proxies bypassed Windows restore, and an open Properties dialog remained actionable after its download was removed. Native requests now always reach the host, which owns reuse and restore. A removed download closes its standalone dialogs through their cancel path and returns focus to Close. Both browser regressions failed before these fixes and pass afterward. A third regression confirms the removed-download message is translated in Urdu.

## Visual evidence

- [Light progress window](../evidence/2026-11-12/popout-light.png)
- [Dark progress window](../evidence/2026-11-12/popout-dark.png)
- [Urdu, narrow window at 130% text size](../evidence/2026-11-12/popout-urdu.png)
- [Real Windows paused progress](../evidence/2026-11-12/native-progress-one.png)
- [Real Windows live progress](../evidence/2026-11-12/native-progress-two.png)

## Local development and repeatability

The development instance runs at `http://127.0.0.1:9297/`, with application data and downloads under `/tmp/pullwell-development`. Start it from the repository with:

```sh
GOCACHE=/tmp/pullwell-go-cache go run . -root /tmp/pullwell-development -debug -tray -ui browser
```

Frontend assets are embedded by Go, so source changes require restarting this command. The separate disposable verification instance uses port 9197 and `/tmp/pullwell-popout-data`; tests never use the development data.

With the disposable application and fixture server running, repeat the feature checks using Playwright and axe-core installed outside the repository:

```sh
NODE_PATH=/tmp/pullwell-ui-tools/node_modules PLAYWRIGHT_BROWSERS_PATH=/tmp/pullwell-browsers \
  node scripts/uitest/popout_check.mjs http://127.0.0.1:9197/ http://127.0.0.1:8765 /tmp/pullwell-popouts
```

Detailed evidence: `/tmp/pullwell-popouts-reviewed`, `/tmp/pullwell-popout-a11y`, `/tmp/pullwell-popout-legacy-reviewed`, and `/tmp/pullwell-popout-modern-final`.

## Windows runtime verification

The release preparation subsequently verified the merged commit `2287399` on real Windows/WebView2 in [GitHub Actions run 43258859108](https://github.com/nisar87/pullwell/actions/runs/43258859108). All Windows jobs passed: engine/application tests, program integration, browser extension, and window checks. The dedicated 22-assertion pop-out suite verifies distinct HWNDs and WebViews, per-ID reuse, minimize/restore, independent controls, Properties removal, progress after the main UI closes, and native script-close without stopping transfers. The full window harness reports 225 passing assertions and zero failures.

Screenshots and detailed results are retained in the `window-results` Actions artifact. Local browser screenshots use Linux font fallbacks; Windows captures use the actual system fonts. Process-crash recovery and close-during-controller-creation received code review and static validation; the smoke test does not inject those faults. On a native WebView process failure, affected progress windows close and can be reopened while transfers continue in the existing backend.

## Refresh, 2026-11-14

`popout-light.png` and `popout-dark.png` above are replaced with captures of the separate progress window of the
0.13.0 interface, taken in the real Windows window (WebView2 156, Segoe UI Variable and Bahnschrift present) by
`scripts/uitest/final_visuals.mjs` on a GitHub-hosted runner. The main-window and Options images in
`docs/screenshots/` come from the same run.
