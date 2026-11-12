# Pullwell frontend modernization

## Intent and scope

Make every existing user-facing surface feel coherent, modern, approachable, and efficient. Preserve the complete download manager, its keyboard workflows, browser integration, English/Urdu support, user preferences, and offline operation. The user explicitly authorized planning, verification, and implementation through completion on a new branch; routine design and implementation decisions are delegated to the implementer.

The application is a Go-hosted desktop webview with dependency-free HTML, CSS, and JavaScript. All Go files, the download engine, API contracts, native integration, release/version files, and extension background/core/video behavior are outside the write scope.

## Approaches considered

1. CSS-only refresh: inexpensive, but leaves the crowded command hierarchy, weak empty states, and navigation context unresolved.
2. Layered frontend modernization (selected by implementer): preserve existing handlers and stable DOM hooks, build reusable presentation helpers, reorganize the workspace, and apply a shared visual and motion system throughout the app and popup.
3. Framework rewrite: introduces build/runtime complexity and extensive regression risk without benefiting this embedded, offline desktop application.

## Design direction: a little momentum

Use a quiet, warm-neutral workspace with a teal primary action, readable system typography, softly rounded controls, consistent line icons, and restrained category colors. Light and dark themes are equally complete. The personality comes from file-shaped badges, the real segmented transfer line, thoughtful empty-state illustrations, and tactile feedback, never distracting animation or fabricated statistics.

Inspiration: Linear's consistent headers and receding navigation (March 12, 2026 design article); Microsoft Fluent 2's focused, brief, accessible motion. UI/UX Pro Max searches validated the flat productivity style, semantic colors, clear states, and reduced-motion requirements. Its marketing-page and scroll-reveal suggestions do not fit a desktop utility and are intentionally excluded. Keep system fonts and local assets; no external requests or runtime libraries.

## Workspace structure

- Retain the desktop menu bar and every toolbar command. Group actions by purpose and preserve labels, shortcuts, titles, disabled states, and overflow access.
- Sidebar: distinct Library, File types, and Queues groups with clear selected state, category-specific icons, stable counts, and a useful browser-connection entry.
- Content header: current view title, meaningful result count, search context, and a compact live transfer summary using actual API data. No redundant dashboard cards.
- Download list: easier file-name scanning, two-line filename/source treatment in comfortable density, compact mode, clear progress state, semantic progress values, preserved sorting/resizing/selection, and an adaptive narrow layout without losing access to columns.
- Intentional initial loading, no-download, no-search-results, and filtered-empty states with an appropriate next action. Preserve drag/drop and existing new-download/browser pairing paths.
- Status bar: retain transfer speed, global speed-limit control, selection count, destination, and sparkline with improved spacing and focus.

## Shared components and interactions

- Centralize presentation helpers for file icons, empty states, view labels, and responsive controls in `ui/components.js`; keep API and download behavior in their existing files.
- Improve all dialogs, confirmations, form controls, options sections, progress metrics, pairing codes, menus, toasts, and banners through shared styles.
- Give dialogs an accessible name and close control, accessible field names, sensible initial focus, focus containment, Escape dismissal, and focus restoration. Keep a dialog's intended action and confirmation semantics.
- Make options navigation a properly named tab system with selected state and keyboard navigation.
- Brief entrance, menu, state, and press transitions, driven by motion tokens. Polling must not replay entrances, replace focused controls, or interrupt input. Reduced motion disables nonessential animation.
- Extension popup uses the same palette, typography, control geometry, file-transfer identity, loading feedback, explicit labels, and clear paired/offline states. Keep extension messages and permissions unchanged.
- Use logical layout properties and review Urdu/RTL. Translate new fixed UI strings; preserve filenames, URLs, units, and numbers.

## Acceptance criteria

1. New branch with frontend/documentation/test changes only; no backend or release modifications.
2. Every existing command and dialog remains reachable and functional.
3. Shared, coherent light/dark visual system covers the complete app and extension popup.
4. Keyboard navigation, focus management, readable contrast, screen-reader naming, reduced motion, and RTL work.
5. Main workspace and dialogs work at 375, 768, 980, 1280, and 1440 px, at all existing text sizes and both row densities. The table can scroll within its own region when the user explicitly enables many columns.
6. No added runtime dependencies, remote fonts, or external asset requests.
7. Existing browser flow suites and new targeted interaction/accessibility checks pass; screenshots are inspected in both themes and narrow/wide layouts. Go tests/builds verify the unchanged host embeds the updated frontend.

## Review risks

Focus loss caused by one-second polling; sort/column preferences on narrow layouts; large text and translated labels; live progress announcements becoming noisy; dialog close actions and unsaved input; extension disconnected/permission states. Validate these specifically, with real browser interactions and the existing fixture server.
