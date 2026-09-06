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

## Evidence-based fix

The only P1-style layout issue found in real Tauri inspection was the History content flex item
retaining an automatic minimum width, which could squeeze the Inspector at the final viewport.
Adding `min-width: 0` to `.pl-history` and `.pl-history__table-wrap` restored the intended
`History main + 410px Inspector` geometry. The targeted real WebView2 recheck reported a 982px
History main area, 410px Inspector, and 1600px body width.

## Validation status

- Rust `cargo fmt --check` and `cargo test`: PASS (21 tests).
- UI `npm.cmd test`: PASS (90 tests / 19 suites); `npm.cmd run build`: PASS.
- Node visual harness tests: PASS; scaled Query sanity: PASS.
- `go vet ./pkg/api ./pkg/storage ./cmd/proxylens-ui-fixture`: PASS.
- `go test ./pkg/api ./pkg/storage ./cmd/proxylens-ui-fixture`: API and fixture command passed, but the existing storage `TestIncrementalConstantCost` exceeded the Go testing 10-minute timeout. No storage implementation was changed in this phase; this remains a non-blocking pre-existing gate note rather than a visual acceptance failure.
- `git diff --check`: required final closeout gate.

## Boundary and decision

All P0/P1 visual blockers are closed. Design System v1 is **Frozen** against the accepted
synthetic query-only Tauri surface. Real FLClash/Mihomo lifecycle, real Controller, real-data
visual acceptance, Test Connection, tray, Windows Service, MSI/updater and Phase 4 remain out
of scope and deferred.
