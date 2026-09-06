# ProxyLens Audit Intelligence v1

- **Status**: Phase 4A foundation, Phase 4B1 temporal process intelligence, and Phase 4B2A host-route transition complete; Phase 4B2B/4C deferred
- **Scope**: deterministic, read-only Review workspace over reconciled accounting
- **Authority**: the active v2 accounting generation, otherwise the latest completed legacy run

## 1. Product boundary

Review is a secondary investigation workspace. It is not a score, alert feed,
automatic rule recommender, or network-control surface. It has one fixed route
scope: `PROXY`. Every item contains structured detector facts, accounted bytes,
connection count, and exact/interval evidence split where applicable.

The workspace has four deterministic sections:

1. `MATCH` fallback proxy traffic;
2. broad UDP traffic recorded with the canonical `NETWORK,udp` rule;
3. proxy targets with destination IP evidence but no `host` or `sniff_host`;
4. physical proxy connections strictly above `100 * 1024 * 1024` bytes.

No severity, confidence score, inferred intent, current node state, or live
Controller call is part of this contract.

## 2. Detector contract

All detectors read the selected reconciled accounting table once for the
requested `[from,to)` window. Exact rows use `observed_at` membership. Interval
rows use the existing `IntervalAllocator`, so clipped bytes are reported as
estimated and never silently presented as exact.

`MATCH` findings require `route=PROXY`, normalized `rule=MATCH`, and positive
accounted bytes. They aggregate by process and the first available target in
the order `host → sniff_host → destination_ip → missing`.

Rule comparison uses a normalized copy only. The stored occurrence-time `rule`
and `rulePayload` remain raw evidence in the finding response; `DomainSuffix`
must not become `DOMAINSUFFIX` merely because detector comparison is
case-insensitive.

Broad UDP findings require both `route=PROXY`, normalized `rule=NETWORK,udp`,
and `network=udp`; other UDP traffic is not included.

IP-only findings require `route=PROXY`, positive accounted bytes, an available
destination IP, and both host fields empty.

Large-connection findings aggregate only the physical key
`(session_id, epoch_id, connection_id)` and include the named threshold in the
response. The comparison is strictly greater than 100 MiB.

Finding IDs are stable hashes of detector identity, not the full mutable display
subject: MATCH uses process/target plus canonical `MATCH`, broad UDP uses
process/target plus canonical `NETWORK,udp`, IP-only uses process/destination
IP, and large connections use `(session_id, epoch_id, connection_id)`. Bytes,
counts, representative metadata, locale, and the live time-window `to` are not
identity inputs, so metadata enrichment or a live-window advance does not
manufacture a new identity.

## 3. Query API

```text
GET /api/v1/intelligence/findings?from=<rfc3339>&to=<rfc3339>&limitPerKind=20
```

`from` and `to` are required, must form a non-empty range, and use the normal
Bearer/CORS/read-only API boundary. `limitPerKind` defaults to 20 and is
bounded to 1–50. The response carries `route=PROXY`, `accountingVersion`,
`countsByKind`, and deterministic finding items. No endpoint writes SQLite,
starts a Runtime, or contacts a Controller.

### 3.1 Temporal comparison (Phase 4B1 / Phase 4B2A)

Phase 4B1 adds one deterministic, read-only comparison over process hourly
dimensions:

```text
GET /api/v1/intelligence/process-changes
  ?baselineFrom=<rfc3339>&baselineTo=<rfc3339>
  &recentFrom=<rfc3339>&recentTo=<rfc3339>&limitPerKind=20
```

All four boundaries are required, UTC-hour aligned, at least one hour long,
and chronologically ordered so `baselineTo <= recentFrom`. Adjacent windows
and same-shaped windows separated by a gap are allowed; a baseline that ends
after the recent window begins is rejected. The UI derives the previous equal
local-calendar-day window for quick ranges and the immediately preceding equal
interval for a custom range, then clips both windows inward to complete
UTC-hour buckets. The API never guesses a missing baseline or silently
compares a partial hour.

Both windows must pass two independent readiness layers. First, monitoring
coverage must be complete: `futureDurationMs`, `outsideKnownScopeMs`, and
`uncoveredDurationMs` must be zero and `coverageRatio` must be `1`. Second,
the selected accounting authority must have published every raw journal row
whose sequence is after its authority boundary and whose `observed_at` falls
inside that effective window. The active v2 boundary is
`accounting_generations.published_journal_sequence`; the legacy boundary is
`accounting_runs.source_journal_sequence_max`. A legacy run without the latter
is not treated as complete.

If either layer fails, the endpoint returns HTTP 200 with an explicit status
such as `baseline_has_monitoring_gaps`, `recent_outside_known_scope`,
`baseline_accounting_incomplete`, `recent_accounting_incomplete`, or
`accounting_boundary_unavailable`, and no findings. A journal lag occurring
only after `recentTo` does not block the comparison. This is a valid
unavailable state, not a zero-result claim and not a requirement that global
accounting be fully fresh.

The compatibility process endpoint exposes the original Phase 4B1 detectors. The
generic Review bundle is available at:

```text
GET /api/v1/intelligence/temporal-findings
  ?baselineFrom=<rfc3339>&baselineTo=<rfc3339>
  &recentFrom=<rfc3339>&recentTo=<rfc3339>&limitPerKind=20
```

It uses the same prepared comparison context and returns `processItems` and
`hostItems` together. The process detector semantics are:

- `process_newly_observed_on_proxy`: recent PROXY bytes are positive while
  baseline PROXY bytes are zero;
- `process_proxy_growth`: both periods have PROXY bytes and recent
  `bytes/hour` is strictly greater than baseline `bytes/hour`.

The Phase 4B2A host detector is
`host_gained_proxy_after_direct_baseline`: the exact recorded `host` must be
non-empty, baseline DIRECT bytes must be positive, baseline PROXY bytes must be
zero, and recent PROXY bytes must be positive. Recent DIRECT bytes may remain;
when they do, the UI states that this is mixed recent routing rather than a
claim that all traffic switched. Host findings are sorted by recent PROXY bytes
descending and host ascending, and preserve exact versus interval-derived route
evidence. Their stable IDs use detector kind plus the exact recorded host; no
sniffing, IP inference, current node state, or canonical target substitution is
performed.

The response reports process/host identity, PROXY/DIRECT/REJECT route evidence,
exact versus interval-derived bytes, and for growth the baseline rate, recent
rate, delta rate, and `growthRatio`. Its exact formula is
`(recentProxyBytesPerHour - baselineProxyBytesPerHour) /
baselineProxyBytesPerHour`; `0.5` means `+50%`. It deliberately has no anomaly score,
severity, risk, threshold, or inferred intent. IDs are stable detector-kind
plus process identities. The read path uses the selected active v2 or latest
completed legacy hourly authority through one grouped query per window; it
does not add a writer, migration, connection-count semantics, or raw-event
scan.

History adds one narrow exact `rule` filter for Review investigation. Rule
payload, final proxy, port filtering, sorting changes, and arbitrary detector
query languages remain out of scope.

## 4. UI and investigation flow

The top-level order is `Overview → Review → History → Coverage`. Review shares
the existing time range state and locale/theme contracts, but does not expose a
general Route Control: its scope is visibly and statically `PROXY`.

Each finding explains only detector-relevant facts: MATCH shows route/rule/
target/bytes/precision/connections; broad UDP additionally shows network; IP-only
shows the IP and absent host evidence; large shows process/target/bytes/
threshold/physical connection evidence. `Investigate in History` transfers only
detector-relevant read-only filters, sets History route focus to `PROXY`, resets
pagination, and creates a fresh History snapshot. Review never edits rules,
Controller config, nodes, system proxy, TUN, DNS, or routes.

The temporal comparison is shown before the Phase 4A detector sections. It
uses the existing Review time range, reports its effective baseline/recent
windows and coverage state, and offers the same read-only History handoff with
`route=PROXY` plus either the process filter or a History host filter seeded
with the exact recorded-host value. The existing History host search remains
contextual across both `host` and `sniff_host` using its substring semantics;
it is not an exact SQL host match. The temporal finding remains the
authoritative historical comparison, while History is an investigation context:
its rows and bytes are not required to equal the hourly host finding totals.
Host investigation clears unrelated History filters, resets pagination, and
creates a fresh snapshot. It does not add a second navigation surface or a
general-purpose time-series explorer.

## 5. Fixtures and acceptance

The `review`, `review-temporal`, and `review-route-shift` fixtures are
synthetic and deterministic. The route-shift fixture includes positive and
negative direct-to-PROXY host cases, mixed recent routing, long recorded host
identities, interval-derived bytes, process comparison cases, and the Phase 4A
examples. They are used only through copied temporary DBs in query-only Tauri
visual QA.

The Phase 4B1 visual gate covers the temporal Review at 1280×800 and
1600×1000 in EN/ZH × Light/Dark. The runner records requested size, actual CDP
CSS viewport, locale/theme/page state, fixture SHA before/after, temporal row
evidence, and explicit `owner=0 runtime=0 controller=0` evidence. The
canonical 1280×800 and 1600×1000 matrix remains query-only; 1440×900 is kept
as a geometry review size.

The focused storage timing fixture records 10k and 100k high-cardinality
read timings, with 100k reads completing in roughly 0.4–0.5 seconds per
authority in the current environment and indexed query plans. Legacy/v2
results are equivalent. This is diagnostic evidence for the bounded read
path, not a production accounting benchmark; the existing 1.5M E-drive scale
acceptance remains independent.

## 6. Deferred boundary

Phase 4B2A is limited to the delivered recorded-host direct-to-PROXY
comparison. Phase 4B2B/4C are not started. Future candidates such as broader
first-seen/background-service semantics, rule suggestions, final-proxy
analysis, scoring, and real FLClash/Mihomo validation require separate evidence
and contracts. They must not be inferred from this foundation.
