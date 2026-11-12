# Download Pop-out Windows Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task by task. Steps use checkboxes for tracking.

**Goal:** Open independent, live, practical progress windows for individual downloads.

**Architecture:** Share the existing progress renderer between its modal and a standalone query-route view. A bounded Windows UI-host addition creates WebView2 progress windows; browser mode uses named popups. All transfers and application APIs remain unchanged.

**Tech stack:** Existing HTML/CSS/JavaScript, Windows Go UI host, Playwright/CDP and Go tests.

**Spec:** `docs/superpowers/specs/2026-11-12-download-popouts-design.md`

## Global constraints

- Continue `feature/frontend-modernization` at base `4b4450a` with existing modernization changes uncommitted and preserved.
- No download engine, server, API, extension, release, persistence, or native file-launching changes.
- Native edits are restricted to progress-window UI lifecycle and route validation.
- No runtime dependencies. Keep the current visual/motion system and English/Urdu support.

## Review focus

- Repeated open and multiple downloads must not create accidental duplicates or share controls.
- Closing the opener or progress window must never stop transfers; pop-outs must not take pending add requests.
- Properties and non-resumable confirmation must preserve the progress view and correct item ID.
- Removed items, failures and offline refresh must disable stale actions and present useful recovery.
- Native local-route validation, asynchronous close, COM handler lifetime, and fixed callback allocation must be safe.

## Task 1: Browser progress view and actions

**Files:** `ui/app.js`, `ui/index.html`, `ui/app.css`, new `ui/progress-window.js`, localization sources/generated app dictionary, new `scripts/uitest/popout_check.mjs`.

**Interfaces:** `ProgressWindow.id`, `ProgressWindow.open(id)` and standalone initialization/render hooks; existing `progressBox`, `paintProgress`, `dlAct`, `dlProps` remain the transfer/control owners.

- [x] Add browser assertions for separate windows, per-ID reuse, independent live controls, opener closure, safe close, removed/offline states, and pending-request isolation; run against the current app and observe missing-feature failures.
- [x] Extract shared progress markup/binding, implement the standalone view and safe named popup manager, and add modal/context/menu entry points.
- [x] Add standalone styles, accessible semantics, localized copy and title handling; verify 420 px/short windows, themes, RTL and reduced motion.
- [x] Run new browser checks plus existing progress and modernization suites.

## Task 2: Native Windows progress windows

**Files:** `webview_windows.go`, optional new `progress_windows.go`, optional portable UI-route helper/test; `tray_windows.go` only if needed for UI cleanup.

**Interfaces:** route `/?progress=<id>` on the exact current application origin. Native handling deduplicates by ID, uses the existing WebView2 environment, handles `window.close`, and leaves normal external links unchanged.

- [x] Write route/lifecycle tests where portable, verify red, then implement the bounded Windows UI host changes in an isolated `/tmp` source copy.
- [x] Validate fixed callback allocation, handler retention/release, close-during-create, crash, focus, independent resize and shutdown.
- [x] Run focused/full Go tests and Windows cross-build; integrate only the authorized native UI files.

## Task 3: Integration, review and evidence

- [x] Run syntax checks, Go checks/cross-build, pop-out/legacy progress/modernization browser checks and accessibility scans.
- [x] Obtain an independent review of frontend and native lifecycle changes; resolve material findings and rerun affected checks.
- [x] Save review screenshots and a concise verification report; audit the diff against the backend boundary.

## Run manifest

S1: one UI feature with an uncertain desktop host integration boundary. Main session owns frontend implementation, tests, integration and evidence. One native UI worker owns only native progress-host files in an isolated `/tmp/pullwell-popout-native` source copy; it cannot modify the shared repository. One fresh read-only reviewer checks final changes. Concurrency cap two, maximum three review rounds, no nested delegation. Worker/reviewer use AUTO inherited model because the runtime requires omitting model overrides absent explicit user direction; reasoning high. Tools and sandbox inherited, no external mutations. The pinned review target is base `4b4450a` plus documented prior modernization work and this feature's diff. Temporary dependencies, downloads and browser evidence stay under `/tmp`.
