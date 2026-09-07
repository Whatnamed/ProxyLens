# Phase 4E-C Production UI Continuation & Audit Intelligence Core V1 Closure

**Date:** 2026-09-07
**Baseline:** `main` / `origin/main` at `5288bc1ea15c3822b62ee602e17fd4a9f6f0bdcf`
**Mode:** approved production current-user NSIS upgrade, read-only DB/API verification,
focused installed WebView2/CDP review
**Result:** **PASS — Phase 4 Audit Intelligence COMPLETED — CORE V1**

## Scope and reused evidence

This is the final Phase 4E continuation. It does not reopen temporal semantics or
feature development. Previously accepted evidence remains authoritative:

- four fully completed UTC hours: `08–09`, `09–10`, `10–11`, `11–12`;
- three ready adjacent temporal pairs with P1/P2/P3 counts:
  - P1 `1/2/0`;
  - P2 `0/3/0`;
  - P3 `2/3/0`;
- API versus active-v2 SQLite oracle mismatch: `0`;
- five unique real growth findings, with three of five below 1 MiB baseline;
- host-transition evidence: `0`; no material Phase 4B2B justification;
- no SQL oracle or full temporal matrix was rerun in this continuation.

## Production preflight

The exact ProxyLens-owned production state was inspected before the upgrade:

- `\ProxyLens\Background Supervisor` was registered, enabled and Running;
- task principal was the current user with `Interactive` / `Limited` execution;
- exactly one installed Supervisor and one installed Runtime were present; desktop and
  Query API were closed;
- task readback contained one LogonTrigger and one TimeTrigger, both with indefinite
  `PT1M` repetition, `StopAtDurationEnd=false`, and `MultipleInstances=IgnoreNew`;
- schema migration max was `9`, active incremental-v2 generation count was `1`, and
  the read-only preflight `PRAGMA quick_check` returned `ok`;
- preflight journal/published boundaries were recorded for continuity; disk free space
  exceeded the scale-aware safety floor;
- `runtime.json` schema was `2`, `autostartEnabled=true`, and persisted Controller
  metadata was present; the credential baseline was `credentialStored=false` and was
  preserved. The raw Secret was never read or printed.

## Build and upgrade

Normal production build commands passed:

```text
npm.cmd run build
npm.cmd run tauri:build
```

The normal current-user NSIS artifact SHA-256 was:

```text
80A45EDCBF8FB4DD4EA2B8BA0A62B6CC6C1661D3C23A62926036002CFB2AFCC6
```

The installer exited `0` and completed the production hooks without abort:

```text
unregister exact task → graceful Supervisor/owned Runtime stop
→ replace files → preserve config/credential state → reconcile owner
```

No manual process kill or manual binary copy was used.

Sidecar identity was exact after installation:

| Binary | Built SHA-256 | Installed SHA-256 | Result |
| --- | --- | --- | --- |
| `proxylens-supervisor.exe` | `C29461CF5BA60FE587CF7260B3EED50844F0E0C26B8F5457A5A6866A6EFD3651` | same | PASS |
| `proxylens-runtime.exe` | `AE8FF352F8E83A982846857E344FEF2A0EEDEA3ABCCB662E76AD5430DA7895D8` | same | PASS |
| `proxylens-query-api.exe` | `E3D78DFBA4756A824F6D79D5818B2736A5A51BA0C51869D27869E1DBBA8EF1A3` | same | PASS |

The installed desktop file had the same length as the direct release executable and
only three bytes differed: the direct unbundled Tauri marker `UNK` was normalized to
`NSS`, which the local Tauri source defines as `BundleType::Nsis`. No other bytes
differed. This is recorded as a packaging-marker note, not an application-content
or renderer failure.

The reviewed CSP remained:

```text
connect-src 'self' ipc: http://ipc.localhost http://127.0.0.1:* http://localhost:*
```

The stale `/vite.svg` reference remained absent.

## Post-upgrade owner and data health

- exact Task remained enabled and Running under current-user `Interactive` / `Limited`
  with the same Logon/Time indefinite `PT1M` contract and `IgnoreNew`;
- exactly one Supervisor and one Runtime remained; no desktop or Query API remained
  after the final close;
- post-upgrade read-only `quick_check` returned `ok`, schema remained `9`, the active
  v2 generation remained unchanged, and the Collector session was `running` with a
  fresh heartbeat;
- journal sequence advanced after the upgrade, v2 publication resumed, and published
  raw/accounted upload and download totals remained equal;
- the brief owner quiescence created the expected Monitoring Gap. No attempt was made
  to hide or backfill it by bypassing the lifecycle.

## Installed WebView2 renderer and Review spot

The installed desktop was launched normally with only a temporary CDP debugging port
for focused observation. It reported a Query API-ready sidecar and rendered:

- URL: `http://tauri.localhost/`;
- `document.readyState=complete`;
- `#root` present with rendered application shell;
- Overview / Review / History / Coverage navigation present;
- no captured CDP runtime/security errors, no IPC CSP rejection, and no material
  horizontal overflow.

The already accepted P3 pair was reproduced through the existing Review controls. The
UI displayed the local-time equivalent of the UTC recent window
`2026-09-07T11:00:00Z → 2026-09-07T12:00:00Z`, with the preceding baseline window
`2026-09-07T10:00:00Z → 2026-09-07T11:00:00Z`.

Observed Review counts were:

```text
new / growth / host = 2 / 3 / 0
```

One real process-growth finding was opened with the existing History action. The drill
preserved a non-empty process filter, fixed `PROXY` route, fresh History snapshot and
page 1; unrelated host and network filters remained empty. No second drill was opened.

## UI close and final state

The desktop was closed through its normal window close path. After close:

- desktop absent;
- Query API absent;
- exact Supervisor and Runtime still present;
- collection/accounting continued;
- config, credential presence baseline, authority DB and Task owner remained intact.

Final decision:

```text
Phase 4 Audit Intelligence = COMPLETED — CORE V1
Phase 4B2B / Phase 4C2 = DEFERRED / LATER
Next = V1 Release Candidate Closure
```

The visible background Supervisor/Runtime console window remains a non-blocking V1 RC
item. It was not changed in this acceptance.

## Safety boundary

This acceptance did not request or connect to a real Controller, and did not start,
stop, restart or modify FLClash/Mihomo. It did not change TUN, system proxy, DNS,
routes, firewall, nodes or rules. Production DB checks used SQLite read-only mode and
`query_only=ON`; the only production lifecycle mutation was the exact approved NSIS
owner quiesce/reconcile. No production credential value was read or printed, and no
Secret entered argv, config, logs or this report.
