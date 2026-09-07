# Phase 4E Installed Desktop Blank-Render P1 Closure — 2026-09-07

## Scope

This targeted closure diagnoses and fixes the installed Tauri renderer boundary only.
It does not reopen Phase 4E temporal validation, modify Collector/Accounting, or
change Supervisor/Runtime ownership.

## Root cause

The installed WebView used the expected `http://tauri.localhost/` document origin.
The packaged frontend and `#root` were present after startup, but the production
CSP omitted Tauri's IPC origin. WebView2 therefore rejected the frontend's
`invoke()` requests to `http://ipc.localhost/`, including
`get_query_api_session` and `get_settings_e2e_mode`. The early white-window
capture was timing-sensitive; the waited CDP capture separated that observation
from the actual IPC/CSP failure.

The production frontend build also retained a stale `/vite.svg` favicon reference
although that file was not packaged. It was removed as an asset-boundary cleanup.

## Targeted fix

`ui/src-tauri/tauri.conf.json` now allows the official IPC origin in `connect-src`:

```text
connect-src 'self' ipc: http://ipc.localhost http://127.0.0.1:* http://localhost:*
```

`ui/index.html` no longer references the missing Vite favicon.

## Evidence

- Binary comparison: the unchanged production-installed desktop was
  `887ffb275fa0d7872b22a63ea78b4156c191dbda81f79e680d6b8584a4cb6482`; the fixed
  release output was `98795c4a3bfccedc888360767f8f140e04bc41aa807e0d897b8cbb2d76e2f788`;
  the fresh normal-product NSIS artifact was
  `39a1e0b05a1b8b7da8ee6595344bfa0dae05e258517c1f23e2b77656ad2540a8`.
- `ui/dist/index.html`: `#root` present; hashed JS/CSS references exist; no
  `127.0.0.1:1420`, Vite HMR, or dev-server literal; no missing asset reference.
- Direct fixed release binary: `http://tauri.localhost/`, `readyState=complete`,
  `#root` present (`rootHTMLLength=27763`), 24 resources, zero captured CDP
  console/security/runtime events, normal exit code 0.
- Fresh isolated NSIS package: installed under a random temporary product identity;
  query-only WebView rendered `http://tauri.localhost/` at `1280x800`,
  `readyState=complete`, `#root` present, and the WebView probe reported
  `meta=1 summary=1 connections=1`.
- The same isolated installed package opened Review against a temporary,
  current-anchor synthetic fixture: temporal section `temporalRows=10`, Phase 4A
  sections remained visible, and document horizontal overflow was false.
- Isolated package task/install identity, desktop PID, and temporary DB were kept
  outside production identity and cleaned exactly; the synthetic fixture is retained
  only under the private validation evidence directory. No production Task Scheduler
  task, DB, config, credential, Supervisor, or Runtime was changed.

## Production boundary

The current-user production installation was not upgraded in this targeted closure;
the existing production Supervisor/Runtime remained untouched and running. The
fixed source and a fresh isolated installed package are verified. A future approved
production NSIS upgrade is the deployment step for replacing the currently installed
old binary.

## Validation

- `npm.cmd run build` — PASS.
- `npm.cmd run tauri:build` — PASS; release executable and NSIS bundle produced.
- Direct release CDP render — PASS.
- Isolated NSIS installed render and Review spot — PASS.
- `git diff --check` — PASS (only the existing JSON line-ending warning).

No real Controller (`9090`/`7988`), FLClash/Mihomo, TUN, system proxy, DNS,
routes, rules, nodes, production DB, or production network lifecycle was touched.
