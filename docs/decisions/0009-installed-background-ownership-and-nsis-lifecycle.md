# ADR 0009: Installed Background Ownership and Upgrade-safe NSIS Lifecycle

- **Status**: Confirmed / Implemented
- **Date**: 2026-09-04
- **Deciders**: ProxyLens Core Team
- **Scope**: Phase 3E-2B2A

This ADR records the Windows V1 installed-lifecycle boundary built on ADR 0008.
It does not claim Windows Service, tray, MSI, updater, or real FLClash/Mihomo
acceptance.

---

## 1. Context

Phase 3E-2B1 established `proxylens-supervisor` as the per-authority-DB owner
for Runtime process continuity, while Tauri remained the only process that
started that owner. An installed application therefore had no login-resident
outer owner and no installed upgrade/uninstall lifecycle.

The 3E-2B2A goal is a bounded Windows V1 installed contract that preserves the
existing data authority and does not put ProxyLens into Mihomo's network path.

## 2. Decisions

### 2.1 Windows outer owner

Windows Task Scheduler is the V1 outer owner. The production task identity is
fixed at:

```text
\ProxyLens\Background Supervisor
```

It runs the installed `proxylens-supervisor.exe` under the current user's
interactive token, with limited privilege, no stored password, and no SYSTEM
or highest-privilege elevation. The task uses a current-user logon trigger,
`StartWhenAvailable`, `MultipleInstances=IgnoreNew`, no battery shutdown
policy, unlimited execution time, and bounded restart-on-failure (10 attempts
with a one-minute interval in the current implementation).

Task Scheduler access is native and narrow: lifecycle operations address only
the exact folder/task path and never enumerate or delete arbitrary tasks.

### 2.2 Ownership layers

The scheduled task owns process residence; `proxylens-supervisor` owns the
per-authority-DB Supervisor mutex and starts/observes/restarts
`proxylens-runtime`; Runtime's named mutex remains the writer-race authority.
The Supervisor does not open SQLite business data or communicate with Mihomo.

The exact local stop event is derived from the normalized authority DB path:

```text
Local\ProxyLens.Supervisor.Stop.v1.<sha256(normalized-db-path)>
```

It is local-user/session scoped, hides the raw path, resets stale state when a
new owner starts, and enters the same graceful shutdown path as `STOP\n`.
Planned upgrade/uninstall stops are therefore not treated as crashes.

### 2.3 Lifecycle CLI and Tauri behavior

`proxylens-supervisor` exposes only the narrow `control status/stop` and
`install status/register/unregister/run/ensure-owner` primitives needed by the
installer and desktop shell. `control stop` probes the exact mutex, signals the
exact event, and waits with a bound; it never kills by process name.

Installed Tauri first asks the Go lifecycle CLI whether the exact production
owner is registered and enabled, then ensures/runs that owner and waits for
Supervisor presence. A developer checkout with no registered owner retains the
existing direct Supervisor bootstrap. A lifecycle failure in an installed
owner path does not silently bypass ownership; if the DB already exists, the
existing read-only Query fallback remains available.

Tauri continues to own only the Query API sidecar. Closing the UI does not stop
the installed Supervisor, Runtime, or Collector, and reopening the UI reuses
the same authority DB ownership.

### 2.4 Configuration and upgrade/uninstall policy

`runtime.json` is schema v2 with `autostartEnabled`, defaulting to true.
Schema v1 loads losslessly with autostart enabled and is written as v2 on the
next save. Disabling autostart unregisters the exact task but does not stop a
currently running owner; opening the UI may still direct-start an owner for the
current session without changing the preference.

The canonical Windows installer is Tauri NSIS with `installMode=currentUser`.
Hooks perform the following exact lifecycle:

```text
PREINSTALL   unregister old task → graceful stop old Supervisor → replace files
POSTINSTALL  reconcile task action → ensure owner when enabled
PREUNINSTALL unregister task → graceful stop owner → remove program files
```

Upgrade replaces desktop/Query/Runtime/Supervisor binaries while preserving
the authority DB, raw history, runtime config, Controller URL, autostart
preference, and Windows Credential Manager credential. Uninstall removes the
installed program files, exact task, and owner processes, but preserves the
user data/config/credential. An explicit remove-all-data flow is outside this
ADR.

## 3. Consequences

- Installed users receive current-user login residence and bounded Supervisor
  crash recovery without a service or elevated install.
- The Runtime/Collector/Accounting ownership and read-only Query boundary from
  ADR 0006/0008 remain unchanged.
- Task registration is a durable OS owner configuration, not a Secret or Query
  token transport; credentials never enter task arguments, JSON, logs, or
  handshake output.
- Settings/autostart UI, tray integration, Service/MSI/updater lifecycle, and
  final visual or real-data acceptance remain Phase 3E-2B2B/Deferred.

## 4. Verification

- Non-elevated feasibility gate passed using one random
  `\ProxyLens-Test\<UUID>` task and a harmless temporary executable, followed
  by exact cleanup.
- Go lifecycle tests passed for identity restrictions, exact Task Scheduler
  register/status/action/unregister, per-DB presence/stop event, config v2
  migration, and Supervisor lifecycle behavior.
- The task-owner harness passed with a random test task and harmless fixture;
  production task identity was not used or enumerated.
- The isolated NSIS harness passed fresh Package A install, Package B upgrade,
  UI-close owner persistence, disabled preference preservation, and uninstall
  with DB/config/credential retention. Package identity, task identity,
  credential target, DB/config roots, and Controller were all temporary or
  random.
- When the isolated harness explicitly enables its E2E scheduling aid, it may
  add a short-lived test-only repetition trigger so the harmless fixture's
  relaunch is observable within the test bound. Production registration keeps
  only the logon trigger and bounded restart-on-failure settings.

## 5. Safety boundary

This implementation and its acceptance harness do not start, stop, restart,
kill, or modify FLClash/Mihomo; do not change TUN, system proxy, routes, DNS,
firewall, nodes, or rules; and do not connect to real `127.0.0.1:9090` or
`127.0.0.1:7988`. Test process termination is limited to known fixture PIDs,
and test Task Scheduler / Credential Manager identities are random and exact.
