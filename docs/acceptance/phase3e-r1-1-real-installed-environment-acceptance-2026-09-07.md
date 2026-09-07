# Phase 3E-R1.1 Real Installed Environment Acceptance — 2026-09-07

## Result

**PASS.** This continuation accepts the installed Windows ownership boundary that
was blocked in the original R1 run. The original blocked branch and its private
evidence remain unchanged:

- `validation/phase3e-r1-real-installed-acceptance` at `07c6df678445ae6e0c5cae1d54007d7399f20399`
- R1.1 branch: `validation/phase3e-r1-1-real-installed-continuation`
- R1.1 base: `62960c7637656479d5f21337c793a5855edfac52`

R1.1 did not rerun the accepted R1 Gate 1/2, migration, Phase 3S, Phase 4,
or full UI matrix. It revalidated only the two repaired installed-ownership
boundaries and the narrow production continuation gates.

## Historical blocked state and safe recovery

The exact production task `\ProxyLens\Background Supervisor` was rechecked
before any mutation. The task was enabled and Ready, while the installed
Supervisor was absent and one Runtime process owned the canonical authority DB.
The old control status was `supervisorRunning=false, runtimeRunning=true`.

The exact old task was unregistered first. The recorded orphan Runtime identity
was then rechecked and only that exact recorded ProxyLens PID `8212` was
terminated.
No process-name cleanup was used, and no FLClash/Mihomo process was touched.
The follow-up control status became `supervisorRunning=false,
runtimeRunning=false`.

Afterward, three 30-second quiescence samples showed no ProxyLens writer and
stable DB/WAL size and DB mtime. No manual checkpoint was performed.

## Fresh pre-R1.1 safety point

The source DB and WAL were copied without overwriting the earlier R1 backup:

`E:\ProxyLensValidation\phase3e-r1-1-2026-09-07\production-backup`

At copy time:

- DB: `6,150,705,152` bytes; WAL: `119,995,032` bytes;
- schema version: 9; journal count/max: `1,818,256` / `1,818,256`;
- active v2 published sequence: `1,816,112`;
- DB SHA-256: `9D4D964EA0405A5F9F56D07D1A9D40313FD2750564EFC94CCE24AAC8C409C53D`;
- WAL SHA-256: `AF7A57C9ED5902C9DD9639BE54CEAF18D07B704B748F3A3207C21E19F7C5D99D`.

Source and backup hashes matched.

## Current package and installed owner

The current-main production NSIS package was rebuilt from the R1.1 base and
installed as a current-user upgrade. Installed Supervisor, Runtime and Query
API hashes matched the build outputs. Installed versions were:

- Supervisor `v0.7.0-phase3e2b1`;
- Runtime `v0.7.0-phase3e2a`.

The exact production task read back as:

| Contract | Observed |
| --- | --- |
| principal | current user, `Interactive`, `Limited` |
| triggers | one `LogonTrigger` and one `TimeTrigger` |
| repetition | both `PT1M`, no duration, `StopAtDurationEnd=false` |
| overlap | `MultipleInstances=IgnoreNew` |
| availability | `StartWhenAvailable=true` |
| restart policy | no `RestartOnFailure` XML element |
| action | installed `proxylens-supervisor.exe`, no arguments |
| secret exposure | no Secret/token/password in task arguments |

Task readback and `install status` reported the task enabled, Supervisor
running and Runtime running. Config metadata reported `autostartEnabled=true`,
persisted Controller configuration present, and no stored/effective Secret;
the raw Secret was never read or printed.

## Stabilization

Twenty samples at 30-second cadence covered 570 seconds of sampling. The owner
process start at `15:20:37` and final sample at `15:31:50` provided more than
ten minutes of wall-clock observation.

- DB growth: `52,961,280` bytes;
- journal advance: `15,213` rows;
- accounting lag moved from 522 to 114 events during the sample window;
- exactly one Supervisor and one Runtime throughout;
- latest session remained `running` with fresh heartbeats;
- raw and accounted upload/download totals stayed equal;
- no duplicate owner, reconnect loop or disk guard was observed.

After the recovery checks, a read-only metadata pass reported schema 9 and
`quickCheck=ok`. The final read-only sample still showed one active generation,
fresh heartbeat, and equal raw/accounted totals.

## UI, Runtime and Supervisor recovery

The installed desktop was opened and closed twice through its normal window
close path. Each open created a Query API process whose loopback `/healthz`
returned 200. Each close removed the Query API while the same Supervisor and
Runtime remained alive. Reopen reused the existing owner and did not create a
duplicate writer. This was a narrow lifecycle recheck; the accepted full UI
interaction matrix was not repeated.

For the Runtime recovery check, only the revalidated exact Runtime PID `30664`
was terminated once. The original Supervisor `29412` stayed alive and started
Runtime PID `16068`; exactly one Runtime remained, the session heartbeat resumed,
and accounting continued.

For the repaired Supervisor recovery check, only the revalidated exact
Supervisor PID `29412` was terminated once. The original Runtime PID `16068`
remained alive. The next Task Scheduler repetition started Supervisor PID
`4868` after 10.8 seconds, without `install run`, a second schedule, or a manual
fixture. The task remained healthy and exactly one Supervisor plus exactly one
Runtime were present. This is the production-equivalent `LogonTrigger + indefinite PT1M
TimeTrigger` recovery path; `RestartOnFailure` is not used as the crash contract.

## Data, privacy and safety boundary

- DB schema 9, active incremental-v2 authority and read-only `quick_check` remained healthy;
- Runtime command lines contained only the authority DB path;
- task action contained no arguments and no Secret;
- no Secret was placed in output, logs, task arguments, JSON or SQLite;
- R1.1 made no request to the real Controller and did not access port `7988`;
- no FLClash/Mihomo process, configuration, TUN, system proxy, DNS, route,
  firewall, node or rule was started, stopped, killed or modified;
- only exact recorded ProxyLens orphan/Runtime/Supervisor PIDs were terminated,
  and the two desktop windows were closed normally.

## Final installed state

The installed ProxyLens owner was left running for continued real coverage:

- current-user production task enabled and running;
- one Supervisor (PID `4868`) and one Runtime (PID `16068`) active;
- Runtime/Collector session running and accounting advancing;
- UI closed, with no Query API or desktop process left behind;
- production DB and configuration preserved;
- no uninstall, task unregister, or post-acceptance collection stop was performed.

P0/P1 blockers: **0**. Phase 4E was not started. The next work remains outside
this acceptance and must not weaken the no-network-mutation boundary.
