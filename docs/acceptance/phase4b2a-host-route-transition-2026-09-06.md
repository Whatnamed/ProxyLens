# Phase 4B2A Host Route Transition Acceptance

- **Branch:** `feat/phase4b2a-host-route-transition`
- **Baseline:** `main` / `origin/main` at `2e25076b02ae4b0e546fea9b04a98836a3bd30a6`
- **Scope:** generic temporal findings bundle and the narrow recorded-host
  DIRECT-to-PROXY detector; Phase 4B2B/4C remain Deferred
- **Status:** PASS

## Contract

Phase 4B2A reuses the Phase 4B1 temporal preparation contract. One request
validates the four explicit UTC-hour bounds, resolves one active v2 or latest
completed legacy accounting authority, checks monitoring coverage for both
effective windows, and checks that the authority has published every raw
journal event after its boundary whose `observed_at` falls inside either
window. Lag after `recentTo` is allowed. A non-ready comparison returns HTTP
200 with an explicit status and empty process/host findings.

The new read-only endpoint is:

```text
GET /api/v1/intelligence/temporal-findings
  ?baselineFrom=<rfc3339>&baselineTo=<rfc3339>
  &recentFrom=<rfc3339>&recentTo=<rfc3339>&limitPerKind=20
```

The existing `GET /api/v1/intelligence/process-changes` endpoint remains a
compatible process-only surface using the same preparation contract.

## Host detector

`host_gained_proxy_after_direct_baseline` is deliberately narrower than a
general route-change detector. It requires:

- a non-empty exact recorded `host` from the event-time evidence;
- baseline DIRECT bytes greater than zero;
- baseline PROXY bytes equal to zero;
- recent PROXY bytes greater than zero.

Recent DIRECT and REJECT bytes remain in the response. The UI explicitly says
when recent routing is mixed and does not claim that all traffic switched.
Findings are sorted by recent PROXY bytes descending and exact host ascending;
IDs use detector kind plus exact recorded host. Exact and interval-derived
evidence is preserved. No sniffing, IP substitution, current node state, or
canonical target inference is performed.

## Storage and API evidence

- `go test ./pkg/storage ./pkg/api`: PASS (`93.863s`; temporary test DBs only).
- exact targeted patterns `go test ./pkg/storage -run 'Test.*(ProcessChanges|Temporal|HostRoute|AuditIntelligence)' -count=1 -v` and `go test ./pkg/api -run 'Test.*(Temporal|ProcessChanges|Intelligence)' -count=1 -v`: PASS (`23.843s` and `0.221s`).
- `go vet ./pkg/storage ./pkg/api`: PASS.
- `go test ./cmd/proxylens-ui-fixture` and `go vet ./cmd/proxylens-ui-fixture`: PASS.
- Generic API contract tests cover auth, GET-only, required/unaligned bounds,
  invalid limits, reversed chronology with HTTP 400, per-kind limits/counts,
  process findings, and host findings.
- Storage tests cover direct-to-PROXY positives, recent mixed direct/proxy,
  baseline-already-PROXY and other negative cases, long hosts,
  interval-derived evidence, stable IDs, legacy/v2 equivalence, and query
  plans. No writer, migration, or index was added.

High-cardinality focused timing and existing query-plan evidence:

| Authority | Fixture | Baseline read | Recent read | Process detector | Host detector | Service total | Query plan |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| legacy | 10,000 | 20.835 ms | 34.206 ms | 3.843 ms | 0 ms | 58.581 ms | existing `idx_hourly_dim` |
| v2 | 10,000 | 18.922 ms | 38.278 ms | 9.126 ms | 0 ms | 62.198 ms | existing v2 primary key |
| legacy | 100,000 | 212.156 ms | 511.282 ms | 102.469 ms | 2.619 ms | 801.585 ms | existing `idx_hourly_dim` |
| v2 | 100,000 | 216.569 ms | 515.960 ms | 88.468 ms | 3.190 ms | 812.699 ms | existing v2 primary key |

Legacy and v2 results were equivalent. The 1.5M E-drive scale acceptance and
Phase 3S remain independent; this phase does not alter accounting publication.

## UI and Tauri evidence

- `npm.cmd test`: PASS, 102 tests.
- `npm.cmd run build`: PASS.
- Node visual harness tests: PASS, 6 tests.
- `node --check` passed for the visual runner and fixture generator.
- The `review-route-shift` synthetic fixture is copied into a temporary DB for
  every run. Each run confirmed `queryOnly=1 owner=0 runtime=0 controller=0`,
  exact actual CSS viewport, unchanged source/copy SHA, process findings,
  host findings, mixed-route wording, interval-derived evidence, Phase 4A
  sections, and no horizontal overflow.

Final query-only Tauri matrix:

| Size | EN Light | EN Dark | 中文 Light | 中文 Dark |
| --- | --- | --- | --- | --- |
| 1280×800 | `phase4b2a-route-shift-1280x800-en-light` | `phase4b2a-route-shift-1280x800-en-dark` | `phase4b2a-route-shift-1280x800-zh-CN-light` | `phase4b2a-route-shift-1280x800-zh-CN-dark` |
| 1600×1000 | `phase4b2a-route-shift-1600x1000-en-light` | `phase4b2a-route-shift-1600x1000-en-dark` | `phase4b2a-route-shift-1600x1000-zh-CN-light` | `phase4b2a-route-shift-1600x1000-zh-CN-dark` |

Every run reported `rows=13`, `hostRows=4`, `overflow=0`, exact requested /
actual viewport, and source/copy unchanged. The host History drill was also
executed in every matrix run; the focused 1280×800 EN Light report recorded:
`history=1 route=PROXY host=1 page=1 freshSnapshot=1 unrelatedFilters=0`.

## Safety and remaining scope

All Controller data was synthetic. No real `127.0.0.1:9090` or `127.0.0.1:7988`
endpoint, FLClash, Mihomo, TUN, system proxy, DNS, route, node, rule,
production DB, Supervisor, Runtime, Collector, or production lifecycle was
touched. No Phase 4B2B/4C work was started.

Deferred follow-ups remain broader first-seen/background-service intelligence,
rule/final-proxy/port dimensions, scoring, real FLClash/Mihomo validation, and
any automatic network/configuration action.
