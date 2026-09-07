# ProxyLens V1 Core Release Candidate — 2026-09-07

## Decision

**ProxyLens V1 Core Release Candidate = ACCEPTED**

Product package version: **0.7.0**

This is an internal release-candidate acceptance. It is not a public `v1.0.0`
release, Git tag, GitHub Release, or external installer publication.

## Source and build

- Source branch: `release/v1-core-rc`
- Source SHA: `246c0fd8f2f243393fce8b2b38eaa983e8540223`
- Product identity: `ProxyLens` / `com.proxylens.desktop`
- Installer mode: current-user NSIS
- Target: Windows 11 x64, `x86_64-pc-windows-msvc`, build 26200
- Commands: `npm.cmd run build`; `npm.cmd run tauri:build`
- Node `v22.23.2`; npm `10.9.8`; Go `go1.26.7 windows/amd64`
- rustc `1.98.0 (88d9e12ae 2026-08-18)`; cargo `1.98.0 (797e8a9bc 2026-08-05)`
- Tauri CLI `2.11.4`

Both required production builds passed. No production code or package version
change was made for this RC.

## Immutable RC artifact

Artifact directory:

```text
E:\ProxyLensRelease\v1-core-rc\2026-09-07\
```

It contains exactly `ProxyLens_0.7.0_x64-setup.exe`, `SHA256SUMS.txt`, and
`BUILD_INFO.txt`. Installer SHA-256:

```text
4725ABC38612A3F0CE6A4CA250CE855D8552980431081A515C3A30F7F50D0C97
```

Build-output SHA-256 values:

| Binary | SHA-256 |
| --- | --- |
| `proxylens-desktop.exe` | `3AA35410031F0AA3DF8A03426C0B6F855F278A9952A185C8500914A1EB4A8E70` |
| `proxylens-query-api.exe` | `7A7CCD76487352DCF0CDC1AFAA1986CA99BC1FE248B5305BABC88F126DD80133` |
| `proxylens-runtime.exe` | `72A629C55BCD27528E9D3537D7313842051DE4E85E1A59FC1A31F5ABBE0630F1` |
| `proxylens-supervisor.exe` | `276DC7649F4E256A4D0252F7A2DC312F6EF7E7987537EDE65D825322323F0EF4` |

The installed Query API, Runtime and Supervisor matched these build outputs
exactly. The installed desktop had the same length and exactly three byte
differences from the direct build: the local Tauri NSIS packaging marker changed
from `UNK` to `NSS`. This is the expected NSIS normalization, not an application
or renderer difference. The installed desktop SHA-256 was
`88ADAF6BE3935BA729AA73CA7E9A239E0F3B32682B26DAA2FFB5E0EBD955B35D`.

The installer is unsigned. This is recorded as an **unsigned internal RC**, not
an internal acceptance blocker.

## Console-window ownership evidence

The exact installed Supervisor process tree was mapped with HWND → PID → parent
ancestry before and after the UI smoke. The owner chain contained only the
Supervisor, its Runtime child, and a dedicated `conhost.exe`; the number of
visible top-level windows belonging to that chain was `0`. The previously
observed title containing `proxylens-supervisor.exe` belonged to a shared
`WindowsTerminal.exe` developer/Agent shell, not to the installed owner chain.

Result: **console RC item closed by evidence**. No launcher or console code change
was needed.

## Production preflight and upgrade

The exact production task `\ProxyLens\Background Supervisor` was registered,
enabled and running under the current user with `Interactive` / `Limited`
execution and `MultipleInstances=IgnoreNew`. It had one LogonTrigger and one
TimeTrigger; both used indefinite `PT1M` repetition with
`StopAtDurationEnd=false`. Exactly one Supervisor and one Runtime were present;
the desktop and Query API were closed before upgrade.

The fixed RC installer exited `0` and completed the existing fail-closed
current-user lifecycle:

```text
unregister exact task → graceful Supervisor/owned Runtime quiesce
→ replace files → preserve config/credential state → reconcile owner
```

No manual Runtime/Supervisor kill, manual binary copy, or lifecycle bypass was
used. The expected short Monitoring Gap from owner quiescence was accepted and
not hidden or backfilled.

## Installed owner, database and accounting result

After upgrade:

- exactly one Supervisor and one Runtime remained;
- the exact Task remained enabled/running with the same current-user trigger and
  repetition contract;
- schema migration max was `9`;
- read-only `PRAGMA quick_check` returned `ok` after upgrade;
- the active v2 generation count was `1`, with status `active` and algorithm
  `incremental-accounting-v2`;
- Query API read-only meta reported `READY`, schema `9`, and v2 accounting;
- Collector heartbeat and journal sequence continued advancing;
- after the read-only integrity sample completed, published v2 boundaries advanced
  through multiple completed incremental runs (`2281822 → 2284752 → 2285428`);
- a ten-second idle resource sample showed no runaway child: Supervisor/Runtime
  aggregate CPU increased by approximately `0.047s`, and the DB grew by about
  `266KB` during the sample.

The integrity sample used read-only SQLite access. It did not change the
production database.

## Configuration and credential preservation

The preserved non-secret config remained schema `2`, with `autostartEnabled=true`
and persisted Controller metadata present. The `runtime.json` SHA-256 remained:

```text
119417D2B6B585DB2341466965B3250699BC147B03F93281968FE8300D52764A
```

The credential-presence baseline remained `credentialStored=false`. No raw
Controller Secret was read, printed, exported, or added to the artifact, logs,
config, database, argv, or report.

## Installed desktop smoke

The installed desktop was launched once with a temporary WebView2 CDP port for
focused observation. The real page reported:

- URL `http://tauri.localhost/`;
- `document.readyState=complete`;
- `#root` present and rendered;
- app viewport `1100×750`, no material horizontal overflow;
- Overview, Review, History, Coverage and Settings opened successfully;
- one Review finding followed the existing Review → History drill, preserving
  PROXY context and a non-empty investigation filter;
- History pagination remained present and the drill had no horizontal overflow;
- Settings showed the installed non-secret state; the Secret/password input was
  empty and no Apply action was invoked;
- no native `<select>` or `datetime-local` control was present in the checked
  surfaces.

The desktop was then closed through its normal window close path. After close,
the desktop and Query API were absent while the exact Supervisor, Runtime, Task
owner and collection remained active.

## Package and privacy audit

The installed product directory contained only the expected desktop, Query API,
Runtime, Supervisor, uninstaller and bundled font license files. The immutable
RC directory contained only the installer, checksum manifest and build info.
No production DB, WAL/SHM file, `runtime.json`, credential export, Secret,
private Phase 4E evidence or test fixture was copied into the RC artifact or Git
changes. The reviewed IPC CSP remained present and no stale `/vite.svg` reference
was found in the production dist check.

## Scope and deferred work

Core V1 remains frozen and accepted. Phase 4B2B and Phase 4C2 remain
`DEFERRED / LATER`. History virtualization, expanded accessibility integration,
tray, Windows Service, MSI/updater, Controller discovery/configuration,
automatic rules/network actions, catalog expansion and UI redesign remain out of
scope for this RC.

P0/P1: **0**

No new P2/P3 release blocker was found. Public signing, tagging, GitHub Release
and external installer publication remain later distribution work.

## Safety boundary

This RC acceptance did not connect to a real Controller and did not start, stop,
restart or modify FLClash/Mihomo. It did not modify TUN, system proxy, DNS,
routes, firewall, nodes or rules. No next phase was started.
