# Core V1 post-RC audit closure — 2026-09-08

## Baseline and decision

Reviewed `PROXYLENS_CORE_V1_POST_RC_FULL_AUDIT_2026-09-07.md` from the local,
ignored `tmp/prompts` directory against clean `main` at
`2969474c0dc1ed964a0570a9f50709aea1a71217`, the same HEAD audited by the report.
Each finding was checked before edits. The confirmed Connection Detail authority
defect and History tie-order issue are fixed; current documentation is aligned.
The replacement unsigned internal `0.7.0` RC passed a normal current-user upgrade
and focused installed Inspector acceptance. The September 7 artifact and its
historical acceptance report remain unchanged.

## Item-by-item closure

| Original finding | Current evidence | Closure |
| --- | --- | --- |
| P1 Connection Detail reads legacy v1 | `QueryService.ListAccountedTrafficForConnection` hardcoded `accounted_traffic`; the new active-generation regression failed on the original HEAD. | **Fixed.** Reuse `AnalyticsService.resolveAccountingScope` and its table/key selection. Active v2 wins globally; only no active generation permits completed legacy fallback. Events and summary use the same selected rows. |
| P2 unbounded detail timelines | Both detail and raw traffic queries return all connection rows. A bounded, read-only scan of the approximately 8.06 GB live DB found maximum v2 connection cardinality 12,044 rows; the next four were 7,647 / 7,501 / 4,901 / 4,793. | **Intentionally retained, user confirmed.** Largest SQL read took 0.562 s; positional-row JSON sample was 5,380,731 bytes (not the HTTP DTO size or a React rendering benchmark). Follow up with server-side pagination plus an independent full summary. No silent cap or partial-summary substitution. |
| P2 malformed/non-text WebSocket Coverage | `client.go` skips non-text messages; JSON failure emits CollectorHealth and continues without GapOpened. Transport/read failures use a separate gap path. | **Intentionally retained as non-blocking hardening.** Code path confirmed; occurrence in production has not been established. A future fix must define bad-frame gap opening/recovery and counter attribution together, with transport/state/Coverage tests. No isolated health-to-gap patch or claim that healthy Coverage proves complete byte capture. |
| P2 raw history growth | No retention deletion; DiskGuard preserves raw authority and stops collection below max(1 GiB, 15% DB size). The 175–190 MiB/hour figure is a historical code comment, not a newly measured rate. | **Intentionally retained.** Archive/retention/export and disk-runway visibility need an explicit product/data-policy decision; no deletion, migration, VACUUM or storage redesign. |
| P3/P2 History tie ordering | Original `ORDER BY first_observed_at DESC` lacked a composite tie-breaker. The regression failed on the old query and passed after the fix. | **Fixed.** Add ascending session/epoch/connection identity after descending observation time. Test crosses page boundaries, sessions and epochs with tied timestamps. Frozen range and offset/hasMore behavior unchanged; this does not add a database snapshot across HTTP requests. |
| P2/P3 automated merge gate | Local `.github` absent. Live GitHub branch API reports `main.protected=false`, required-check enforcement off. | **Intentionally retained.** No CI or branch-policy change in this query fix. Local checks and installed evidence remain the acceptance basis; no remote CI success claimed. |
| PRODUCT temporal overclaim | Delivered process/host detectors compare explicit baseline/recent windows, not all history or a causal rule change. | **Fixed** current product text; no detector algorithm/threshold change. |
| PRODUCT background catalog overclaim | Embedded `knowledge/background-processes-v1.json` and matching code support three Defender entries with process/path requirements. | **Fixed** product scope; no catalog expansion or trust inference. |
| API contract legacy/schema drift | Detail described latest completed run; schema examples said 7 while migrations/runtime report 9. | **Fixed** API/IA/coverage/semantic documents. `runId` compatibility means generation ID for v2, run ID for legacy. Query API's existing `0.7.0-phase3a` metadata string remains accurately represented; package version is 0.7.0. |
| ARCHITECTURE real acceptance Deferred | Two current statements conflicted with R1.1 / 4E-C accepted evidence. | **Fixed**, preserving deferred Service/tray/updater/configuration scope. |
| STATUS stale review/resume/numbering | Top accepted RC conflicted with pending-review and collection-resume instructions at the bottom. | **Fixed** current status and next steps; older phase-specific stopped-collection evidence remains historical. |
| README pre-Core / first-ever wording | Summary overstated first-ever process behavior. | **Fixed** Core V1 target label and window-comparison summary. |

No original finding was classified as disproven or already fixed at the starting
HEAD. No required fix remains blocked. The four intentionally retained items
above remain real limitations or policy work, not completed implementations.

## Implementation and regression evidence

- No accounting writer, schema, migration, raw authority, network configuration,
  detector, React rendering or Rust query semantics changed.
- Before the first completed accounting publication, an existing connection still
  returns HTTP 200 with no events/summary. Database errors continue to propagate.
- Authenticated read-only API regression covers no accounting, legacy fallback,
  full DTO legacy/v2 equivalence for nonzero evidence, v2 incremental updates,
  recent v2-only connections, active-v2 empty rows without per-connection legacy
  fallback, and fallback when the generation is superseded.
- Existing composite-identity API isolation now runs against both legacy and
  v2-only databases, including rejection of crossed session/epoch identities.
- Both new authority and pagination regressions were executed against the original
  HEAD query: they failed for the expected reasons. Restoring the fix passed them.
- `go test ./...` and `go vet ./...` passed, including the final rerun after test
  changes; Go storage first full run completed in 89.376 s.
- `npm.cmd test`: 103 passed, zero failed; `npm.cmd run build`: passed.
- `node --test tools/discovery/test/*.test.mjs tools/ui-acceptance/test/*.test.mjs`:
  24 passed, zero failed.
- `npm.cmd run tauri:build`: passed, rebuilding the frontend and all three Go
  sidecars and producing the current-user NSIS installer. Only the existing MSVC
  import-library linker warning appeared.
- `git diff --check`: passed. No private DB, identities, screenshot, credentials,
  measurement output or installed configuration is included in Git.

Phase 3S scale, the Phase 4E temporal oracle, and the full visual matrix were not
rerun: writer/storage/detector/visual implementations are unchanged. The actual
installed query path and focused Inspector smoke below cover the changed behavior.

## Replacement internal artifact and installed verification

Source code: `00fe991`; documentation follow-up source reference:
`0b704806e9aa5b0a0dce674969f1fe88492dfce4`. Build completed before those commits
were recorded; executable sources match them. Only documentation was pending.

Artifact directory: `E:\ProxyLensRelease\v1-core-post-rc\2026-09-08\`.
It contains the installer, `SHA256SUMS.txt` and `BUILD_INFO.txt`.

Installer: `ProxyLens_0.7.0_x64-setup.exe`, 42,918,988 bytes.
SHA-256: `D8B0D832EE7DA682892A0F6D047578F68CC47535A7AABAC198FA820EC301F584`.

Before upgrade the exact task was running under the current user with Limited
privilege and the expected installed Supervisor action. Exactly one Supervisor
and Runtime were running; no desktop or Query API was open. The frozen installer
exited 0 via existing unregister → graceful stop → replace → preserve → reconcile
hooks. No manual runtime kill or binary replacement bypass was used. The expected
short collection gap was not concealed.

All three installed sidecar hashes match the new build. `runtime.json` hash is
unchanged; no credential was read/exported. The task resumed with one Supervisor
and Runtime. The installed Query API returned schema 9 / READY and active v2.

Two separate recent-connection checks passed:

1. Installed Query API on live read-only SQLite: a terminal connection first seen
   after v2 activation had zero legacy rows and two v2 events. Event count, selected
   authority and all four raw/accounted upload/download sums matched independent
   SQL exactly; detail HTTP request took 0.047 s.
2. Actual installed WebView2 History → Inspector: another recent v2-only connection
   rendered eight events. Event DOM count and full summary matched SQL; accounting
   heading and precise totals were present, with no page error or page-level
   horizontal overflow. Chinese/Dark Inspector screenshot was inspected locally.
   Internal History table scrolling remains supported and was not reinterpreted
   as page overflow.

Computer Use initialization reported unavailable, so the actual installed WebView2
was inspected through Playwright/CDP. The temporary debugger argument applied only
to this desktop launch. Normal window close removed desktop and Query API while
the same post-upgrade Supervisor/Runtime PIDs remained alive. FLClash/Core PIDs
and network configuration were not changed by this task.

The artifact remains unsigned/internal. No tag, GitHub Release, public installer
publication, CI enablement or branch-protection mutation was performed.

## Verification limit

An additional live read-only whole-database `PRAGMA quick_check` did not complete
within its 90-second progress-handler deadline and was interrupted. This is an
incomplete integrity check, not an `ok` result and not evidence of corruption.
No writer/schema/migration changed, and the scoped query/installed checks above
passed. The historical September 7 integrity result must not be presented as a
fresh integrity result for this run. A longer maintenance-time integrity scan is
outside this query-fix acceptance; no production stop was introduced for it.
