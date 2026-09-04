# ADR 0007: Windows Runtime Ownership, Single-Instance, and Tauri Ensure-Start

- **Status**: Confirmed / Implemented
- **Date**: 2026-09-04
- **Scope**: Phase 3E-2A

## Context

Phase 3E-1 supplied a reusable Go Runtime Core and a bundled Runtime binary,
but did not define who starts it for the desktop application or how two UI
launches avoid creating two writers for the same authority database. The
solution must preserve the bypass-observer boundary: ProxyLens may observe
Mihomo through its read-only External Controller, but must not become part of
the network path or change Mihomo/system state.

## Decision

### 1. One Runtime writer per authority DB

`proxylens-runtime` acquires ownership after resolving its writable authority
DB path and before opening the writer DB. On Windows the identity is a
case-normalized absolute path hashed into a deterministic named mutex:

```text
Local\ProxyLens.Runtime.v1.<sha256(normalized-db-path)>
```

The raw database path is never part of the object name. The named mutex is
crash-safe at the OS process boundary; its native handle is held by a locked
OS-thread owner until the Runtime closes. A small process-local guard also
prevents recursive acquisition by two library callers in one process.

The same authority DB returns the typed `ErrRuntimeAlreadyRunning` result;
different authority DBs may run concurrently. There is no PID-file-only
ownership approximation and no broad process cleanup.

### 2. Runtime startup handshake

The Runtime emits one machine-readable JSON line after local writer DB,
Collector, and Accounting startup has crossed its readiness boundary:

```json
{"type":"proxylens-runtime-ready","runtimeVersion":"0.7.0-phase3e2a","pid":1234}
```

A duplicate candidate emits a clean, zero-error-exit signal and does not open
the DB or create a Collector session:

```json
{"type":"proxylens-runtime-already-running","runtimeVersion":"0.7.0-phase3e2a"}
```

Neither signal contains a secret, token, traffic history, or raw configuration.
Controller reachability is not part of `READY`; the Collector retains its
read-only retry semantics. `MIHOMO_SECRET` is inherited only as an interim
in-memory compatibility path into `CollectorOptions.Secret`; it is not written
to argv, logs, SQLite, or a plaintext file. Runtime controller resolution is
`--controller` > `PROXYLENS_CONTROLLER_URL` > the existing product default.

The reusable `CollectorRunner` remains stricter than the CLI boundary:
`ControllerURL` must be explicit and non-empty, so library callers cannot
silently inherit the real-controller default.

### 3. Tauri ownership split and startup ordering

Tauri uses the official bundled `proxylens-runtime` sidecar resolver and
performs this order:

```text
resolve writable Runtime DB path
  → ensure-start Runtime
  → wait for READY or ALREADY_RUNNING and DB availability
  → resolve existing-only Query DB path
  → start read-only Query API sidecar
  → load React UI
```

The Runtime child is stored separately from the UI-owned Query child. Normal
window destruction sends `STOP` and kills only the Query API. It does not stop
the Runtime, so a later Tauri launch receives `AlreadyRunning` and reuses the
same authority DB. If Runtime bootstrap fails while a valid existing DB is
available, Tauri still attempts the read-only Query sidecar and reports the
bootstrap failure as factual status.

### 4. Explicitly deferred scope

This ADR does not introduce login/autostart, Windows Service, tray ownership,
installer or upgrade ownership, final secure secret provisioning, or a
continuous whole-process crash supervisor. Those belong to Phase 3E-2B.
Real FLClash/Mihomo lifecycle validation and final real-data visual acceptance
remain deferred.

## Evidence

The implementation is covered by Go ownership/config/runtime tests, Rust
handshake/path tests, existing UI tests, and:

```text
node tools/runtime/run-phase3e2a-lifecycle.mjs
```

The lifecycle smoke uses only a random loopback mock Controller, temporary
ProxyLens data directories, exact test-process PIDs, and read-only Controller
requests (`GET /version` and `GET/WS /connections`).
