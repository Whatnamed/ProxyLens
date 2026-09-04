# ADR 0008: Background Supervisor and Secure Runtime Configuration

- **Status**: Confirmed / Implemented
- **Date**: 2026-09-04
- **Deciders**: ProxyLens Core Team
- **Scope**: Phase 3E-2B1

## Context

Phase 3E-2A established a per-authority-DB Windows mutex for the Runtime writer
and let Tauri ensure-start that foreground Runtime. That was sufficient for a
desktop session, but it did not provide a process owner that could restart a
crashed Runtime after the UI process exited. Runtime configuration also needed
a durable non-secret Controller URL and a secure Secret path without putting
credentials in JSON, argv, logs, or a new HTTP control plane.

The solution must preserve the existing bypass-observer boundary: ProxyLens
may read Mihomo's External Controller, but must never modify Mihomo, TUN,
system proxy, routes, DNS, nodes, or rules.

## Decisions

### 1. One Supervisor per authority DB

`proxylens-supervisor` acquires a deterministic Windows named mutex derived
from the normalized authority DB path:

```text
Local\ProxyLens.Supervisor.v1.<sha256(normalized-db-path)>
```

The existing Runtime writer mutex remains separate and authoritative:

```text
Local\ProxyLens.Runtime.v1.<sha256(normalized-db-path)>
```

The Supervisor never replaces the Runtime writer authority. It probes the
Runtime mutex non-destructively, observes an existing Runtime, and lets a
racing Runtime candidate return `ALREADY_RUNNING`.

### 2. Supervisor owns process continuity only

The Supervisor starts `proxylens-runtime --db <path>`, parses the narrow
Runtime READY/ALREADY_RUNNING protocol, monitors the exact child PID, and
restarts it after process exit using bounded backoff (1s initial, 2x, 30s
maximum, reset after a stable run). It does not open SQLite, implement
Collector/Accounting logic, or read Mihomo directly.

After acquiring the per-DB Supervisor mutex, it first emits an ownership
confirmation with `runtimeState: "starting-retrying"` and its exact Supervisor
PID. Tauri may retain that exact candidate while Runtime startup is retrying;
the later `started` or `already-running` state reports Runtime readiness without
changing Supervisor ownership.

An exact `STOP\n` on Supervisor stdin cancels the Supervisor. If it owns the
Runtime child, it sends the same exact STOP, waits a bounded interval, and
terminates only that recorded child if necessary. An externally observed
Runtime is never stopped by Supervisor cancellation. EOF and all other stdin
lines are ignored; OS signal cancellation remains supported.

### 3. Tauri ensures Supervisor, not Runtime

Tauri startup is:

```text
resolve writable DB
  → ensure detached Supervisor
  → receive Supervisor READY / ALREADY_RUNNING
  → wait for DB
  → resolve existing-only DB for Query
  → start UI-owned read-only Query API
```

The Supervisor is spawned with the standard Windows process API and an
independent process group rather than the `tauri-plugin-shell` child registry;
that registry intentionally kills its children at Tauri exit. Query API
continues to use the shell sidecar lifecycle and is the only child stopped on
normal UI window destruction. Thus closing the UI stops Query but leaves
Supervisor/Runtime/Collector alive, and reopening reuses the same DB owner.

### 4. Non-secret runtime configuration

The canonical config is:

```text
%LOCALAPPDATA%\ProxyLens\config\runtime.json
```

`PROXYLENS_CONFIG_DIR` maps to `<dir>\runtime.json` for test/development
isolation. The v1 JSON contains only `schemaVersion` and `controllerUrl`.
Missing v1 config is valid; malformed or future schema fails closed; writes use
a same-directory temporary file and atomic replace. The config path is
independent of `PROXYLENS_DATA_DIR` and never contains Secret, subscription,
node credentials, traffic history, or Query tokens.

### 5. Secure Secret storage and precedence

Production Secret storage uses Windows Credential Manager Generic Credential
target `ProxyLens/MihomoController/v1` with `CRED_PERSIST_LOCAL_MACHINE` and
the narrow `CredWriteW`, `CredReadW`, `CredDeleteW`, and `CredFree` APIs. The
only test target override is accepted in explicit E2E mode and must be a
random `ProxyLens/Test/<UUID>` target; tests delete that exact target in
cleanup and never enumerate or touch the production target.

Runtime Secret precedence is:

```text
MIHOMO_SECRET → Windows Credential Manager → empty
```

`MIHOMO_SECRET` remains an explicit development override and is never
automatically persisted. Controller URL precedence outside E2E is:

```text
--controller → PROXYLENS_CONTROLLER_URL → runtime.json → product default
```

In E2E mode only an explicit CLI or environment URL is accepted, and it must
be an HTTP `127.0.0.1` URL with a random explicit port; conventional real
Controller ports are rejected. Missing override fails before any Controller
client is created.

### 6. Safe machine handshake

Supervisor READY/ALREADY_RUNNING and Runtime READY/ALREADY_RUNNING signals
contain only type, version, process identity, and safe Runtime state. They
never contain Secret, token, raw traffic, or full environment. An E2E-only
status file may mirror these safe PID/status records so a harness can observe
restart after the Tauri process has exited; it is not a production control
channel.

### 7. Deferred 2B2 scope

This ADR does not implement login/autostart, Startup shortcuts, Scheduled
Tasks, Windows Service, tray ownership, installer hooks, uninstall cleanup,
upgrade replacement, final Settings UI, installed-path acceptance, or Phase 4.
Automatic recovery after the Supervisor process itself exits without a UI
restart remains part of the installed autostart/ownership work in Phase 3E-2B2.
Real FLClash/Mihomo lifecycle and final real-data visual acceptance remain
deferred.

## Consequences

- Runtime remains a focused foreground executable; Supervisor supplies the
  independent process-continuity boundary needed for UI-independent operation.
- A Runtime crash becomes a bounded restart and preserves the same authority DB
  and Runtime writer mutex semantics.
- Query API remains read-only and UI-scoped; Tauri/Rust does not duplicate Go
  storage, accounting, or Controller semantics.
- Secure configuration is durable without placing Controller Secret in a
  plaintext file, process arguments, or logs.
- Login/autostart and installed lifecycle remain intentionally unclaimed until
  a separately scoped 3E-2B2 decision.

## Evidence

The implementation is covered by Go runtime/config/protocol/ownership tests,
the Windows-only subprocess Supervisor integration, Rust Supervisor parser and
DB path tests, UI/build tests, and:

```text
node tools/runtime/run-phase3e2b1-supervisor.mjs
```

The lifecycle smoke passed with a random-port mock Controller, temporary
DB/config directories, a random test-only Credential Manager target, exact
test-created PIDs, secure credential authentication, Tauri UI-close survival,
Runtime crash/restart, duplicate/different DB ownership, existing Runtime
takeover, and exact STOP cleanup.
