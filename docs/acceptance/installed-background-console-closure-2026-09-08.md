# Installed Background Console Closure — 2026-09-08

## Outcome

Core V1 installed background residence now has a creation-time no-console
contract. The production Task Scheduler action is the GUI-subsystem
`proxylens-supervisor-host.exe`; the console `proxylens-supervisor.exe` remains
the lifecycle/configuration/handshake CLI. The two entry points share the same
Supervisor application, so this change does not create a second ownership
authority.

The Runtime remains a console-subsystem executable, but the Supervisor creates
it with the Windows `DETACHED_PROCESS` flag while preserving the explicit pipes
used by the READY and graceful STOP protocols. Microsoft documents that
`CREATE_NO_WINDOW` is ignored when combined with `DETACHED_PROCESS`, so the
flags are not combined; a focused Windows comparison additionally showed that
`CREATE_NO_WINDOW` alone did not satisfy this host's no-console descendant
contract, while `DETACHED_PROCESS` alone did. Go `HideWindow` remains only a
harmless fallback. Runtime restart, Task Scheduler periodic recovery, UI-close
owner survival, and the read-only Query API boundary are unchanged.

## Root cause

The former production Task action launched the console-subsystem Supervisor
directly. On Windows, console allocation or Windows Terminal attachment can
happen at process creation, before application code can run. The previous
`GetConsoleWindow`/`ShowWindow(SW_HIDE)` path therefore could not guarantee an
invisible installed owner. The installed binary already contained that
post-creation hide path; the remaining defect was the process creation and
Task action boundary, not merely a stale binary.

## Implementation closure

- Added `proxylens-supervisor-host.exe`, built with the Windows GUI subsystem.
  It discards terminal streams and receives lifecycle stop through the exact
  per-DB local stop event.
- Moved the reusable Supervisor daemon path into `pkg/supervisorapp` so the
  console CLI and scheduled GUI host cannot drift in mutex, Runtime, READY,
  STOP, or bounded-restart behavior.
- Registered only the GUI host as the production Task Scheduler action. The
  current-user, Interactive, Limited, `IgnoreNew`, LogonTrigger plus indefinite
  `PT1M` repetition contract remains unchanged.
- Added the creation-time Windows `DETACHED_PROCESS` Runtime process flag and
  retained `HideWindow` only as a fallback; included the host in
  sidecar build, Tauri bundle, installed-layout, and lifecycle reconciliation.
- Removed the obsolete post-creation console hide helper.

## Isolated validation

The dedicated `run-installed-background-console-regression.mjs` passed with
one random `\ProxyLens-Test\<UUID>` task, temporary DB roots, a random loopback
mock Controller, and exact PID cleanup. It verified:

- Task action PE subsystem `GUI/2`; console CLI and Runtime PE subsystem
  `CUI/3`;
- no new `conhost.exe`/Windows Terminal descendant and no changed existing
  Windows Terminal title;
- first Task Scheduler activation without a manual `Run()` assist;
- exact Runtime PID termination followed by Supervisor Runtime replacement;
- exact Supervisor host PID termination followed only by the next Task
  Scheduler `PT1M` repetition producing the replacement host;
- direct GUI-host Runtime launch, Runtime replacement, exact graceful stop,
  preserved CLI READY handshake, and preserved `--version` output;
- exact unregister/PID/temp-root cleanup.

The existing installed lifecycle and settings-product harnesses were updated to
discover the host in the installed layout and passed with their existing
isolated task/fixture contracts. No real Controller or Mihomo endpoint was
used.

## Automated checks

- `go test ./...` — PASS
- `go vet ./...` — PASS
- Windows Runtime process configuration regression — `DETACHED_PROCESS` only,
  no `CREATE_NO_WINDOW`, `HideWindow=true` — PASS
- `npm.cmd test` — 103 tests passed
- `npm.cmd run build` — PASS
- `npm.cmd run sidecar:build` — PASS; host included
- `npm.cmd run tauri:build` — PASS
- `node tools/runtime/run-installed-background-console-regression.mjs` — PASS
- `node tools/runtime/run-phase3e2b2a-installed-lifecycle.mjs` — PASS
- `node tools/runtime/run-phase3e2b2b-settings-product.mjs` — PASS
- `git diff --check` and modified Node syntax checks — PASS

## Installed current-user upgrade evidence

A normal current-user NSIS upgrade completed with exit code `0` through the
existing unregister/graceful-stop/replace/reconcile lifecycle. After upgrade:

- the exact production task was enabled and running with action
  `proxylens-supervisor-host.exe`, current-user `Interactive`, `Limited`, and
  `MultipleInstances=IgnoreNew`;
- installed host/CLI/Runtime/Query binaries matched the just-built sidecar
  outputs, with host subsystem `GUI/2` and CLI/Runtime subsystem `CUI/3`;
- `runtime.json` content/hash and credential presence were preserved;
- the installed process tree was host → Runtime with no `conhost.exe` or new
  Windows Terminal; the installed desktop opened, rendered, and closed;
- after desktop close, the Task Scheduler host and Runtime remained alive;
- a read-only Query API spot check returned schema 9 and active-v2 metadata.

This closure did not rerun the live `quick_check` against the approximately
11.7-GB production DB. The read-only sample reported existing accounting lag
(`isFresh=false`); that is recorded as observed state, not recast as a fresh
accounting result and not attributed to this console fix.

## Safety boundary and remaining scope

The implementation and validation did not connect to real `127.0.0.1:9090` or
`127.0.0.1:7988`, and did not start, stop, restart, kill, or modify
FLClash/Mihomo. No TUN, system proxy, DNS, route, firewall, node, rule, or
Mihomo configuration was changed. The only production lifecycle mutation was
the authorized normal current-user ProxyLens NSIS upgrade; no broad process or
Task cleanup was used.

Background terminal polish beyond the creation-time ownership fix, icon/startup
polish, public signing/tag/GitHub Release, and external installer publication
remain outside this closure.
