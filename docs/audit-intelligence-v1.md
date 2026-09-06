# ProxyLens Audit Intelligence v1

- **Status**: Phase 4A foundation and semantic closure complete; Phase 4B/4C deferred
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

## 5. Fixtures and acceptance

The `review` fixture is synthetic and deterministic. It includes positive and
negative cases for all four detector families, an interval-derived example,
and a large-connection threshold example. It is used only through a copied
temporary DB in query-only Tauri visual QA.

The Phase 4A visual gate covers Review at 1280×800 and 1600×1000 in EN/ZH ×
Light/Dark. The runner records requested size, actual CDP CSS viewport,
locale/theme/page state, fixture SHA before/after, and explicit
`owner=0 runtime=0 controller=0` evidence.

The focused storage timing fixture records 10k and 100k row construction plus
read timings. It is diagnostic evidence for the bounded read path, not a
production accounting benchmark; the existing 1.5M E-drive scale acceptance
remains independent.

## 6. Deferred boundary

Phase 4B/4C are not started. Future candidates such as historical route-change
comparisons, first-seen background services, rule suggestions, final-proxy
analysis, scoring, and real FLClash/Mihomo validation require separate
evidence and contracts. They must not be inferred from this foundation.
