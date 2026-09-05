# ADR 0010: Production-Scale Storage and Incremental Accounting

- **Status**: Confirmed / Implemented
- **Date**: 2026-09-05
- **Deciders**: ProxyLens Core Team
- **Scope**: Phase 3S

This ADR records the Phase 3S scale-closure architecture: the raw-event density
correction, the generation-based incremental accounting that replaces the
normal full-history rebuild, and the WAL checkpoint policy that supersedes the
per-tick TRUNCATE of commit `2bfd8c5`.

---

## 1. Context

Phase 3 Final Real Environment Acceptance exposed a production-scale failure
that short and mock workloads could not reveal. Two mechanisms amplified each
other:

1. **Raw event density.** The StateEngine emitted a full `ConnectionDelta`
   event for every active connection on every controller frame, even when
   `delta_upload == 0 && delta_download == 0`. Real measurement on the
   production authority DB:

   ```text
   event_journal                1,549,954
   connection_traffic           1,520,175
   ConnectionDelta              1,517,609
   zero-byte connection_traffic 1,484,090  (97.63%)
   real DB                      5,204,979,712 bytes
   WAL peak (pre-fix runtime)   ~25-26 GB
   ```

   Journal growth itself was bounded: avg 1,206 B/event, measured boundary
   growth 102k-400k events/hour, i.e. raw intake of roughly 150-600 MB/hour.
   The 25GB WAL cannot be explained by raw intake.

2. **Accounting write amplification.** The runtime scheduler checked freshness
   every 30s; any new journal event made accounting stale and triggered a full
   `RebuildAccounting` from sequence 1, writing a complete new versioned
   derived run. Canceled runs left partially committed rows forever (412,000
   orphan rows in production, 58.5% of all accounted rows). A 229,648-event
   rebuild already took 107.1s; cost grew linearly with history.

The earlier claim "raw journal grows ~1GB/hour / ~20GB/day" is rejected by
measurement and was an artifact of conflating WAL churn with journal intake.

## 2. Decisions

### 2.1 Raw density: suppress zero-byte deltas, keep sparse presence evidence

The engine still processes every controller frame in memory and always updates
in-memory liveness/counters. Durable emission follows one shared contract
(`emitDeltaOrPresence`) used identically by the steady-state and
reconnect-recovery paths:

- positive traffic deltas always emit a full `ConnectionDelta` (unchanged
  semantics, byte sums provably identical);
- a zero-byte frame emits **no** Delta; it emits a lightweight
  `ConnectionPresenceCheckpoint` (30s default interval per active connection,
  named constant) only when the connection has had no other durable evidence
  in the interval;
- Delta and presence are never emitted at the same instant;
- metadata/relay/health/regression events keep their own semantics and reset
  the presence cadence;
- `ConnectionDisappeared` carries the exact final known presence
  (`lastObservedAt` + counters) from engine memory, and the projector writes
  it deterministically so the final known presence cannot look stale.

Presence checkpoints update durable liveness facts only
(`connections.last_observed_at` + observed counters) and never create
`connection_traffic` rows.

Measured effect (scale acceptance, 50 connections / 4800 frames / 20 min):
**97.62% durable delta-row reduction, byte totals exactly equal to input.**

Historical raw rows are never deleted; the correction is forward-looking.

### 2.2 Accounting: one active generation, incremental ranges, atomic publish

Migration `008_incremental_accounting_v2.sql` (additive) introduces:

- `accounting_generations` — at most one `active` generation holding the
  published journal boundary and published byte totals;
- `accounting_conn_state_v2` — durable per-connection accounting summary
  (first/last observation, disappearance, dimensions, monitored totals,
  current class);
- `accounted_traffic_v2` / `relay_relations_v2` /
  `usage_hourly_dimensions_v2` (+ a distinct-connection evidence table) —
  one canonical derived row per nonzero-byte source event inside the active
  generation;
- `accounting_runs_v2` — per-run incremental metadata (mode, range,
  processed events).

Normal runtime behavior: each 30s tick checks freshness (published boundary vs
journal max) and processes at most a bounded, frame-aligned journal range in
**one transaction that both writes the derived state and advances the
published boundary**. Cancellation or failure rolls the whole chunk back:
no partial garbage, no early boundary, idempotent retries. Graceful shutdown
performs one final bounded flush after the collector writer has stopped, so a
clean stop leaves zero accounting lag (session-end disappearances included)
instead of deferring the last interval to the next start.

Full-history work is limited to the explicit, resumable, chunked seed that
activates a new generation (fresh databases seed automatically; an existing
database seeds once via `collector storage seed-v2`), plus explicit repair or
algorithm migration (`--force` supersedes the active generation).

Relay reconciliation semantics are shared verbatim with the legacy rebuild
(one extracted classification contract, `classifyConnectionGroup`), so v2 and
legacy provably agree. Incremental runs reclassify only affected
(session, epoch) groups and apply bounded corrections when a connection's
class changes (byte re-write + hourly aggregate adjustment + distinct-connection
evidence maintenance).

Two exactness properties make incrementality safe without weakening legacy
semantics:

- a connection with no trailing `ConnectionDisappeared` event was still
  present in the boundary frame, so its relay overlap window uses the boundary
  frame time — identical to legacy per-frame evidence;
- a trailing disappearance carries the first-absent observation time — also
  identical to legacy.

### 2.3 Derived compaction (documented divergence)

v2 derived storage records only nonzero-byte traffic evidence
(`raw_upload > 0 || raw_download > 0`). Zero-byte raw rows remain in the
journal; they are simply not duplicated into derived storage. Consequence:
`usage_hourly_dimensions_v2.connection_count` counts connections contributing
nonzero accounted bytes, while the legacy materialization counted all observed
connections including zero-byte-only ones. Byte fields are exactly equivalent.
This matches the query-layer convention (`GetTopDimensions` already skipped
zero-byte rows) and the legacy hourly table has no Query/UI consumer. The
equivalence test asserts every byte field equals legacy and explains every
connection-count gap as exactly the zero-byte-only population.

### 2.4 WAL policy: observable checkpoints instead of per-tick TRUNCATE

`storage.CheckpointWAL(ctx, db, mode)` returns a structured
`WALCheckpointResult{Busy, LogFrames, CheckpointedFrames}`; busy/incomplete is
health evidence, never treated as success or error.

- Steady state: SQLite autocheckpoint plus a best-effort **PASSIVE** checkpoint
  per accounting tick with telemetry (busy/frames/WAL bytes). Collection and
  accounting correctness never depend on checkpoint success.
- `TRUNCATE` is reserved for safe maintenance/shutdown boundaries: accounting
  stopped, collector writer stopped/joined, fresh non-canceled context, result
  checked and logged; a busy Query reader is reported, never killed.

This supersedes commit `2bfd8c5`'s per-tick TRUNCATE design (which remains in
published history and is not rewritten).

### 2.5 Query fallback

Until an active v2 generation exists, analytics and the Query API read the
latest completed legacy run exactly as before. After activation they read the
active generation's v2 tables (identical response schema). Legacy tables and
runs are not deleted in Phase 3S.

### 2.6 Same-epoch re-observation reopens the projected row

The 30-minute soak surfaced a real projection contract defect that density
churn had never exercised: `connections` rows live under a
`(session_id, epoch_id, connection_id)` uniqueness, `Disappeared` intentionally
keeps the row in a terminal state (projection history), but `ConnectionNew`
was a plain INSERT. A connection ID observed again within the same epoch —
controller snapshot flapping in production, deliberate ID-reuse churn in the
soak — violated the uniqueness and killed the collector.

Contract now: the second `New` reopens the existing row (state back to
`active`, disappearance/observation-end facts cleared, liveness/counters/
baseline/metadata refreshed, original `first_observed_at` preserved). The
journal keeps both the `Disappeared` and the second `New` as raw evidence —
nothing is rewritten; `RebuildProjections` replay hits the identical conflict
path, so the projection stays deterministic. `ConnectionNew` also now binds
the event's baseline counters exactly like `ConnectionBootstrap` instead of
hard-coding zeros.

### 2.7 Writer transactions begin IMMEDIATE

The production C: revalidation exposed the last scale defect: accounting
chunk transactions used SQLite's default DEFERRED begin, so the (fast) read
phase ran lock-free and the first write attempted a lock upgrade. With a live
collector committing every ~250 ms, a 50K-event chunk read phase (0.2–0.5 s
on the 5.2 GB authority DB) lost that race deterministically and died with
the non-retryable `SQLITE_BUSY_SNAPSHOT` after exhausting retries — the seed
made zero progress while every unit test (tiny, uncontended DBs) passed.

All writer-DSN transactions now begin `IMMEDIATE` (`_txlock=immediate`): the
write lock is taken up front, the read phase cannot be invalidated, and a
chunk holds the lock for roughly 1–5 s per 30 s tick on the production DB.
Ingestion is never killed by maintenance: the sink emit budget was raised
5s → 30s so an emit waits inside `busy_timeout` during a chunk (the bounded
collector queue buffers ~50 s), and the deadline only bounds a genuine hang.
The small-database tests could never catch the snapshot defect because their
read phases always won the upgrade race.

## 3. Validation evidence (E: drive, 2026-09-05)

- Legacy-v2 equivalence on a deterministic closed dataset (relay confirmation
  across an incremental boundary, interval allocation, class/route/dimension
  sums, hourly bytes): exact match modulo the documented compaction.
- Real production DB E-copy migration + seed: 1,549,954 journal events, 61
  chunks, 456.6s, peak heap 140MB, **WAL peak 8.5MB**, DB growth +32MB,
  quick_check ok before/after, journal rows unchanged, published raw byte sums
  identical to direct measurement (844,658,319 / 765,561,334), v2 derived rows
  36,085 (= nonzero-byte events).
- Constant-cost: identical 5,000-event batch → 0.34s on 1.57M-event history
  vs 0.42s on 50K-event history (no history scaling). This required forcing
  the range queries onto the journal-sequence index (`INDEXED BY`), because
  the planner otherwise chose `idx_journal_type_obs` and scanned the entire
  history (12s for an empty range at 1.5M scale).
- Crash/cancel: seed resume, canceled publish (zero unpublished rows),
  checkpoint under an active reader (busy reported, no hang), failed-generation
  cleanup (raw untouched).
- 30-minute continuous soak (collector + incremental accounting + read-only
  query workload), final PASS after the re-observation fix and the shutdown
  flush: FinalLag=0, LegacyRuns=0, QueueOverloadEvents=0, max incremental run
  0.083s, WAL peak 4.6MB, 60 incremental runs over 16,176 journal rows with
  ID-reuse churn exercising same-epoch re-observation throughout.
- Production C: authority DB short revalidation (2026-09-05 19:16–20:22 local,
  ~66 min incl. seed): migration 008 applied on open; the runtime auto-seeded
  the 1.55M-event journal (63 completed seed chunks) and activated
  generation `gen-1788607002668706000-1`, then ran 21 completed incremental
  chunks against live traffic with zero failed runs. Final state: published
  boundary == journal max (**lag 0** after the shutdown flush), accounted
  totals == raw totals (927,714,947 / 1,209,704,764), `quick_check ok`,
  **WAL 0 bytes** after the shutdown TRUNCATE, DB +205MB (v2 derived rows +
  the revalidation window's raw evidence), `accounted_traffic_v2` 68,596
  nonzero-byte rows. FLClash / FlClashCore PIDs unchanged throughout; the
  read-only Query API served v2-scoped summary/top queries; collection was
  stopped again afterwards (autostart disabled, no Task Scheduler owner).

## 4. Consequences

- Historical growth no longer makes any future accounting cycle progressively
  more expensive; per-tick cost is proportional to new/affected evidence.
- WAL is bounded by construction (chunked transactions) and observable
  (structured checkpoint telemetry).
- Failed/canceled accounting work cannot leak partial rows; staging exists
  only inside open transactions.
- Raw authority is never rewritten or deleted; the real production DB upgrade
  path is: E-copy migration + seed → validation → normal product migration on
  C: during a short, supervised revalidation.
