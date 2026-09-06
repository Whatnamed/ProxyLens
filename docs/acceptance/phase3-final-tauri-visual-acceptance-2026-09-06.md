# Phase 3 Final Tauri Visual Acceptance

**Date:** 2026-09-06  
**Base:** `main@824a645de60c3ea32d81c58b5440164f277b05e0`  
**Branch:** `feat/phase3-final-tauri-acceptance`  
**Mode:** real Tauri/WebView2 query-only visual QA; no production lifecycle

## Environment

- Windows 11 Home; WebView2 `152.0.4191.66`; display scale 144 DPI (150%).
- Node `v22.23.2`; npm `10.9.8`; Go `go1.26.7 windows/amd64`; Rust `1.98.0`; Tauri CLI `2.11.4`.
- Five fixtures used one UTC anchor recorded at `tmp/ui-fixtures-metadata.json`; scaled profile contains 100,000 events.
- Query API used only random loopback ephemeral ports. No real Controller, production DB, Supervisor, Runtime, Collector, FLClash or Mihomo lifecycle was used.

## Harness and fixture gates

- Both `PROXYLENS_E2E_MODE=1` and `PROXYLENS_VISUAL_QA_QUERY_ONLY=1` were required.
- Runner copied each source DB to an ignored run directory, set explicit `PROXYLENS_DB_PATH`, cleared Controller/Secret and owner/task authority variables, and verified source/copy SHA256 before and after.
- Every final smoke emitted `queryOnly=1 owner=0 runtime=0 controller=0`; Query probe was `meta=1 summary=1 connections=1` for non-empty profiles and the expected `meta=0 summary=0 connections=0` for `empty`.
- The Visual QA gate is a pure three-state contract: normal product and existing E2E without the Visual QA flag remain `Disabled`; the complete pair is `Active`; any requested but incomplete Visual QA flag is `RefusedIncomplete` and cannot fall through to product bootstrap. Incomplete-gate smoke observed `PROXYLENS_VISUAL_QA_REFUSED` with no Supervisor, Runtime, Controller or Query bootstrap markers.
- Active query-only Settings commands are fail-closed before any Supervisor command is invoked. The runner-side semantic policy also rejects missing/incomplete gates, relative or missing fixture DBs, the canonical production DB, inherited Controller authority and inherited Secret authority.
- Fixed-size runs now record both `requestedSize` and CDP-observed `actualViewport`; a mismatch fails the run. The targeted 1280x800 run observed `1280x800`, and the targeted 1600x1000 run observed `1600x1000`; `max` remains observation-only.

## Matrix result

| Area | Evidence | Result |
|---|---|---|
| Fixture smoke | healthy 1280×800 and 1600×1000; gaps/stale/empty/scaled 1600×1000 | PASS |
| Geometry | 1280×800, 1440×900, 1600×1000; maximized 1707×898 CSS viewport | PASS; no page-level overflow |
| Canonical matrix | At both 1280×800 and 1600×1000: 3 surfaces × 2 themes × 2 locales = 12 states each; page titles, no-overflow, font checks and selected Inspector assertions | PASS |
| Canonical surfaces | Overview; History with selected row + Inspector; Coverage | PASS |
| Theme / locale | Light / Dark and EN / `zh-CN`; actual WebView2 toggles | PASS |
| Gaps | Coverage `Inspect around gap` → History custom ±15 minute context | PASS |
| Scaled interaction | 100k fixture: History load, pagination, process filter, route filter, Inspector, Coverage, theme redraw | PASS; no visible freeze/starvation |
| Typography | CDP `CSS.getPlatformFontsForNode`: Manrope Medium, Sarasa UI SC SemiBold, JetBrains Mono Regular; all formal faces loaded | PASS |
| Grayscale | Temporary grayscale copy of canonical Overview spot check | PASS; grouping/selection remains legible |
| Targeted interaction closure | Real Tauri/WebView2 recheck at 1600x1000: History/Inspector boundaries, frozen snapshot refresh, Select keyboard actions, DatePicker focus, System Status focus return, locale persistence, native-control absence and overlay bounds | PASS |

## Evidence-based fix

The only P1-style layout issue found in real Tauri inspection was the History content flex item
retaining an automatic minimum width, which could squeeze the Inspector at the final viewport.
Adding `min-width: 0` to `.pl-history` and `.pl-history__table-wrap` restored the intended
`History main + 410px Inspector` geometry. The targeted real WebView2 recheck reported a 982px
History main area, 410px Inspector, and 1600px body width.

## Targeted review closure evidence

- History selection opened the Inspector on a real fixture row. The first-row `Previous` action was disabled; `ArrowUp` stayed at the boundary; `Next` moved to the next connection; selecting the last row followed by `ArrowDown` stayed at the last connection. Closing the Inspector returned to `History` with no Inspector; selection clearing is the explicit close-button contract, so the History context and frozen snapshot remained intact.
- Refresh advanced the frozen snapshot in the same run (`Snapshot to 06/09/2026, 13:35:40` -> `Snapshot to 06/09/2026, 13:37:21`). The page-size listbox reported `100 / page` after `ArrowDown`, `50 / page` after `Home`, and `200 / page` after `End`; `Enter`, `Space` and `Tab` closed it.
- Custom range DatePicker opened through its explicit start trigger. Real WebView2 exercised `ArrowRight`, `Home`, `End` and `Tab` (the latter moved into the DatePicker time control); the existing canonical interaction evidence covers roving focus across month boundaries and focus-out closure.
- System Status opened with focus inside the dialog, remained inside after `Tab`, and `Escape` closed it with focus returned to `.pl-sidebar__status-btn`.
- Immediate `EN` -> `zh-CN` switching changed navigation/page copy without changing technical host/IP evidence; reload preserved `zh-CN`. The canonical locale matrix continues to cover locale-aware date presentation versus unchanged raw technical values.
- The rendered Inspector evidence retained the Causal Path / Accounting Events first-line marker alignment contract. The focused run also re-opened the Inspector and found no native `<select>` or `input[type="datetime-local"]`; the page-size overlay bounds were `896,867` -> `1009,960` inside the `1600x1000` viewport.

## Validation status

- Rust `cargo fmt --check` and `cargo test`: PASS (27 tests).
- UI `npm.cmd test`: PASS (90 tests / 19 suites); `npm.cmd run build`: PASS.
- Node visual harness tests: PASS (6 tests); positive 1280x800 and negative incomplete-gate Tauri smokes both passed their intended gates; scaled Query sanity: PASS.
- `go vet ./pkg/api ./pkg/storage ./cmd/proxylens-ui-fixture`: PASS.
- `go test ./pkg/api ./cmd/proxylens-ui-fixture`: PASS; the single allowed targeted storage command `go test ./pkg/storage -run '^TestIncrementalConstantCost$' -count=1 -v` timed out after 600.079s in the then-current environment. A subsequent independent follow-up added stage timing and root-caused the timeout to the test fixture leaving its synthetic session running, which made the final frame ineligible for seed completion; it did not change storage/accounting implementation. The ordinary 10k/100k guard now completes in about 12.6s, with final 2k incremental timings of 0.138s and 0.303s (2.2x); the 1.5M-row proof remains in the E-drive scale acceptance.
- `git diff --check`: PASS for this closure.

## Boundary and decision

All P0/P1 visual blockers are closed. Design System v1 is **Frozen** against the accepted
synthetic query-only Tauri surface. Real FLClash/Mihomo lifecycle, real Controller, real-data
visual acceptance, Test Connection, tray, Windows Service, MSI/updater and Phase 4 remain out
of scope and deferred.
