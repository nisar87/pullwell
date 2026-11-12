# Pullwell Frontend Modernization Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task by task. Steps use checkboxes for tracking.

**Goal:** Modernize every frontend surface with a practical, approachable, responsive design and useful, accessible motion.

**Architecture:** Preserve the existing dependency-free application and API behavior. Add a small presentation component module and improve the shared HTML/CSS system, then strengthen frontend-only interactions and popup presentation.

**Tech stack:** HTML, CSS, vanilla JavaScript, Go embedded assets; existing Chrome DevTools Protocol UI suites and Playwright for targeted browser verification.

**Spec:** `docs/superpowers/specs/2026-11-12-frontend-modernization-design.md`

## Global constraints

- Branch: `feature/frontend-modernization`, base `4b4450a`.
- Never modify Go/backend files, engine, API contracts, native integration, release/version files, or extension background/core/video behavior.
- Preserve all existing DOM action hooks, API messages, shortcuts, localization, preferences, and offline operation.
- No runtime dependencies or remote fonts/assets.
- Existing untracked `.agents/` belongs to the user and must remain untouched.

## Review focus

- One-second data refresh preserves rows, focus, selection, scroll, and animation stability.
- Narrow windows and 130% text preserve every action and dialog control.
- Urdu uses logical layout and translates new UI copy.
- Modal focus is contained and restored; fields and icon controls have accessible names.
- Extension pairing, failure, disabled, and permission states preserve existing messages and behavior.
- Independent review additions: isolate global shortcuts from focused buttons/modals; preserve sidebar/status focus through count changes; protect user filenames and queue names from translation; scroll oversized menus and progress sheets; derive keyboard paging from actual row height; parse FAIL lines from legacy browser suites because their exit codes alone are insufficient.

## Task 1: Baseline and independent design check

- [x] Inventory all app and extension surfaces, existing tests, design docs, and relevant skills.
- [x] Create the requested branch and document the design and plan.
- [x] Run baseline Go and browser suites with test data isolated under `/tmp`.
- [x] Obtain a bounded independent review of the design and preservation risks; resolve material gaps.

## Task 2: Workspace and shared components

**Files:** `ui/index.html`, `ui/app.css`, new `ui/components.js`, presentation portions of `ui/app.js`.

**Interfaces:** Components consume existing download items, view, query, and selection. They produce escaped HTML and accessible metadata; existing handlers continue to own download operations.

- [x] Add targeted browser assertions for current-view context, loading/empty/search recovery, narrow layout, and meaningful progress semantics; record baseline failures.
- [x] Implement semantic tokens, desktop shell, responsive command groups, sidebar icons, current-view header, comfortable/compact file rows, live transfer context, empty states, and motion.
- [x] Verify all existing menu, table, selection, keyboard, and transfer tests; inspect screenshots in light/dark and narrow/wide layouts.

## Task 3: Dialog, settings, and accessibility polish

**Files:** `ui/app.js`, `ui/downloads-more.js` only where presentation requires, `ui/app.css`, `ui/i18n/ur.js`, targeted browser checks.

- [x] Test modal Tab/Shift+Tab containment, Escape and focus restoration, field names, options tab state, and reduced motion.
- [x] Improve shared dialog components, form hierarchy, progress sheet, settings navigation, menus, pairing, banners, and toast presentation.
- [x] Add new Urdu strings and review RTL, large text, and density combinations.
- [x] Run the progress/options/features/flows/localization browser suites.

## Task 4: Browser extension popup

**Files:** `extension/src/popup.html`, `extension/src/popup.css`, `extension/src/popup.js` for presentation-only changes, `extension/src/i18n.js` if new text needs translation.

- [x] Modernize all paired/unpaired/offline popup views and field labeling with the same design system.
- [x] Preserve all message payloads, storage, permissions, and media behavior.
- [x] Verify extension core tests, extension packaging, popup states, and extension browser checks.

## Task 5: Final review and evidence

- [x] Run JS syntax checks, `go vet ./...`, `go test ./...`, Windows cross-build, extension tests/build, and all relevant browser suites.
- [x] Inspect representative screenshots, narrow layouts, theme/density/text-size combinations, and runtime error logs.
- [x] Independent frontend review; resolve concrete findings and rerun affected checks.
- [x] Audit the final diff to prove no backend/release changes; record results and remaining platform limitations.

## Run manifest

Scope class S1: one frontend system plus the independent extension popup. Main session is the sole writer and integrator. One read-only reviewer may inspect design and final work; no nested delegation. Concurrency cap two, at most three review rounds. Reviewers use AUTO model inherited from the parent because the runtime requires omitting model overrides unless requested by the user; reasoning high. Effective sandbox is inherited, restricted to read-only by task instructions, with local shell/read tools and no external mutations. Review target is the base commit plus the explicitly documented working diff. All builds, browser dependencies, fixtures, downloads, and evidence are isolated under `/tmp`.

## Completion evidence

See [the verification report](../reviews/2026-11-12-frontend-modernization.md) for final results, screenshots, resolved review findings, and the Linux/Windows runtime limitations. All planned frontend implementation work is complete; the known system-folder check remains explicitly reported as an environment limitation.
