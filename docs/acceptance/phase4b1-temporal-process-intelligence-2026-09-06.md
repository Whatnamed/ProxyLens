# Phase 4B1 Temporal Process Intelligence Acceptance

- **Branch:** `feat/phase4b1-temporal-process-intelligence`
- **Baseline:** `main` / `origin/main` at `d862230b560eb9df60f5e26c943a883991ee70b6`
- **Implementation commit:** `50d578a` (`feat(intelligence): add temporal process comparison`)
- **Scope:** Phase 4B1 temporal process comparison only
- **Status:** PASS; Phase 4B2 and Phase 4C remain Deferred

## Contract

Review keeps the existing recent Time Range as its source. Quick ranges derive
the baseline by shifting local calendar days (`today`/`yesterday` by one day,
`7d` by seven days, `30d` by thirty days). A custom range derives the
immediately preceding equal interval. Both requested windows are clipped
inward to complete UTC-hour buckets; less than one complete hour is explicitly
unavailable. The API receives all four effective bounds and never guesses a
missing baseline or claims precision for a partial bucket.

The endpoint is:

```text
GET /api/v1/intelligence/process-changes
  ?baselineFrom=<rfc3339>&baselineTo=<rfc3339>
  &recentFrom=<rfc3339>&recentTo=<rfc3339>&limitPerKind=20
```

Each bound must be RFC3339, UTC-hour aligned, and part of a non-overlapping
window at least one hour long. `limitPerKind` defaults to 20 and is bounded to
1–50. The endpoint is GET-only, read-only, and uses the normal API auth/error
boundary.

## Coverage eligibility

Both baseline and recent windows must have:

```text
futureDurationMs == 0
outsideKnownScopeMs == 0
uncoveredDurationMs == 0
coverageRatio == 1.0
```

If either window fails the gate, the response remains HTTP 200 with an
explicit status (`baseline_*` or `recent_*`) and an empty `items` array. The
UI presents this as unavailable evidence, never as zero changes. The
supported statuses are `ready`, `baseline_outside_known_scope`,
`baseline_has_monitoring_gaps`, `baseline_future`,
`recent_outside_known_scope`, `recent_has_monitoring_gaps`, and
`recent_future`.

## Detectors and evidence

The only detectors are:

- `process_newly_observed_on_proxy`: recent process PROXY bytes are positive
  while baseline process PROXY bytes are zero. This is a comparison-period
  statement, not a first-ever claim.
- `process_proxy_growth`: both windows contain process PROXY bytes and recent
  `bytes/hour` is strictly greater than baseline `bytes/hour`.

Findings have stable `kind:process` IDs, PROXY/DIRECT/REJECT route evidence,
exact and interval-derived byte fields, and no connection count, score,
severity, risk, anomaly threshold, or inferred intent. Growth additionally
reports baseline rate, recent rate, delta rate, and growth ratio. Review
investigation carries only `process` and fixed `route=PROXY` to History.

## Storage authority and performance

The service resolves the same authority as Phase 4A: active v2 generation,
otherwise latest completed legacy run. It reads process hourly dimensions only,
uses one grouped query per window, and adds no writer, migration, index, or
raw-event scan. Legacy and v2 output bytes/evidence were equivalent in the
focused fixture.

Latest high-cardinality timing evidence from
`TestProcessChangesHighCardinalityTimingsAndQueryPlans`:

| Authority | Rows | Service read + detector phase | Query plan |
| --- | ---: | ---: | --- |
| legacy | 10,000 | 34.2 ms | `idx_hourly_dim (run_id, dimension_type, bucket_start)` |
| v2 | 10,000 | 30.2 ms | v2 primary key `(generation_id, bucket_start, ...)` |
| legacy | 100,000 | 415.5 ms | same existing hourly index |
| v2 | 100,000 | 412.5 ms | same existing v2 primary key |

The focused storage test completed in 4.40s for the 10k/100k legacy+v2
high-cardinality cases. These are bounded read-path diagnostics, not a
production accounting benchmark. The existing 1.5M E-drive scale acceptance
remains independent.

## Review and query-only Tauri evidence

The `review-temporal` fixture is synthetic, deterministic, and used through a
copied temporary DB. It includes positive/negative detector cases, a long
process identity, interval-derived evidence, and the Phase 4A finding cases.
The Review temporal section renders before the Phase 4A sections and preserves
the existing Review → History handoff.

Focused checks:

- Rust `cargo test --lib`: 27 passed;
- UI `npm.cmd test`: 100 passed;
- UI `npm.cmd run build`: passed;
- Node visual harness tests: 6 passed;
- `go test ./pkg/storage -run 'TestProcessChanges' -count=1 -v`: passed;
- `go test ./pkg/api -run 'TestProcessChangesAPIContractAndCoverageStatus' -count=1 -v`: passed;
- `go vet ./pkg/storage ./pkg/api`: passed;
- `git diff --check`: passed.

Final semantic matrix evidence (all `queryOnly=1 owner=0 runtime=0 controller=0`,
actual viewport exact, source/copy unchanged, `rows=9`,
`investigate=9`, `overflow=0`):

| Size | EN Light | EN Dark | 中文 Light | 中文 Dark |
| --- | --- | --- | --- | --- |
| 1280×800 | `phase4b1-temporal-evidence-1280-en-light-retry` | `phase4b1-temporal-final-1280x800-en-dark` | `phase4b1-temporal-final-1280x800-zh-CN-light` | `phase4b1-temporal-final-1280x800-zh-CN-dark` |
| 1600×1000 | `phase4b1-temporal-final-1600x1000-en-light` | `phase4b1-temporal-final-1600x1000-en-dark` | `phase4b1-temporal-final-1600x1000-zh-CN-light` | `phase4b1-temporal-final-1600x1000-zh-CN-dark-retry` |

The runner now waits up to 30 seconds for temporal query evidence after the
WebView state is ready, while still failing closed on timeout. The final retry
of the initially loading-racy 1600×1000 中文深色 case passed after this
bounded readiness guard.

## Safety and remaining scope

All Controller data was synthetic. No real `9090`/`7988` endpoint, FLClash,
Mihomo, TUN, system proxy, DNS, route, node, rule, production DB, or runtime
lifecycle was touched. Query-only Tauri did not start Supervisor, Runtime,
Collector, or a Controller. No Phase 4B2/4C work was started.

Deferred follow-ups remain historical target route-change comparison,
broader background-service/process classification, richer rule/final-proxy/
port dimensions, real FLClash/Mihomo validation, scoring, and any automatic
network/configuration action.
