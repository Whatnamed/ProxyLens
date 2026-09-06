# ProxyLens — UI Product & Interaction Framework v1

**Status:** Draft functional framework for Phase 3B/3C UI implementation  
**Scope:** Product structure, investigation flow, page responsibilities, context continuity, pagination and required states.  
**Not a visual design document.**

This document supplements the frozen repository UI/API/semantic documents. If it conflicts with current code, API contracts, `AGENTS.md`, or frozen architecture/semantic documents, the current repository source of truth wins.

---

## 1. Product interaction model

ProxyLens is a historical network/proxy audit and explainability tool.

The core user actions are:

```text
Scan
→ Overview

Review
→ deterministic evidence candidates

Investigate
→ History

Explain
→ Connection Inspector

Verify
→ Coverage
```

The UI must preserve the product's causal model:

```text
Process
→ Destination
→ Rule
→ Policy / Proxy Chain
→ Final Physical Egress
→ Bytes
```

Evidence trust, coverage, freshness and attribution quality are part of the product, not hidden backend diagnostics.

---

## 2. Top-level navigation

V1 top-level navigation:

```text
Overview
Review
History
Coverage
```

Do not add a V1 Settings page when there are no meaningful writable settings.

Connection Detail is contextual and should normally appear as an Inspector / drill-down rather than top-level navigation.

System Status is secondary and persistent/contextual rather than a primary page.

Review is a primary read-only workspace for Phase 4A and Phase 4B1. It is fixed
to PROXY scope, shares the global time range, and must not expose lifecycle,
Controller, node, rule-write, or system-network controls.

---

## 3. Global Audit Context

Relevant analysis surfaces share:

### Time Range

Supported quick ranges follow frozen local-day semantics:

```text
Today
Yesterday
7d
30d
Custom
```

The UI displays local time while API/storage semantics remain UTC `[from,to)`.

### Route Focus

```text
PROXY
DIRECT
REJECT
ALL
```

Recommended default focus:

```text
PROXY
```

Route context should persist between Overview and History.

Coverage is about monitoring completeness and should not be incorrectly scoped as a Route metric.

---

## 4. Overview

Overview is an analytical briefing and investigation starting point.

It should answer quickly:

```text
What happened?
Who generated it?
Why was it routed this way?
Where did it go?
Through what egress?
Can the evidence be trusted?
```

Recommended conceptual order:

### A. Scope

- current Time Range;
- Route Focus;
- clear snapshot/freshness context where relevant.

### B. Traffic Summary

- Proxy;
- Direct;
- Reject;
- Upload;
- Download;
- Total.

Do not default to giant KPI-card grids.

### C. Evidence Trust

Surface the important evidence-quality signals:

- Monitoring Coverage;
- Accounting Freshness;
- Missing Attribution;
- Ambiguous Relay;
- Sampling Residual where relevant.

Do not collapse them into one invented black-box score.

### D. Who / Why / Where / Through What

Primary ranking blocks:

- Top Processes;
- Top Rules;
- Top Hosts;
- Top Final Proxies.

Protocol/network distribution is secondary.

### E. Drill-down

Where the current API supports it, aggregate entries should lead to filtered History:

```text
Top Process
→ History filtered by Process

Top Host
→ History filtered by Host
```

Do not pretend unsupported aggregate dimensions have full drill-down support.

---

## 5. Review

Review is the evidence-native handoff between Overview and History for Phase
4A, Phase 4B1, and the narrow Phase 4B2A host-route transition contract. It first
presents a temporal comparison over complete hourly buckets, then four
deterministic Phase 4A sections: MATCH fallback, broad `NETWORK,udp`, IP-only
proxy targets, and large physical proxy connections. Temporal rows explain
newly observed PROXY processes, strictly higher PROXY bytes/hour, or recorded
hosts that gained PROXY after a DIRECT-only baseline; each row shows the
structured facts that caused inclusion, exact versus interval-derived evidence
where applicable, and a calm `Investigate in History` action.

The host detector requires baseline DIRECT bytes greater than zero, baseline
PROXY bytes equal to zero, and recent PROXY bytes greater than zero for the
exact recorded host. Recent DIRECT bytes may remain; the UI labels that mixed
state and does not claim that all traffic switched. Host findings are ordered
by recent PROXY bytes and investigate with the exact recorded host.

Temporal comparison uses the existing Review time range. Quick ranges shift by
local calendar days, custom ranges compare the immediately preceding equal
interval, and both sides are clipped inward to complete UTC-hour buckets. If
either side has future time, outside-known-scope time, monitoring gaps, no full
hour, or unpublished accounting evidence inside the effective window, Review
shows an explicit unavailable state rather than inventing a zero-change result.
Lag after `recentTo` does not block the comparison. The backend requires all
four comparison boundaries and fails closed on either monitoring or
window-scoped accounting incompleteness.

Review must not invent score, severity, intent, current node state, or a
recommendation. Investigation transfers only supported process/host/IP/network/
exact-rule filters, fixes History route focus to PROXY, and creates a new
History snapshot. Temporal process investigation carries only `process`, while
host transition investigation seeds the History `host` filter with the exact
recorded host value; both use the fixed `PROXY` route and do not carry a growth
score or create an anomaly classification. The existing History host search is
contextual across `host` and `sniff_host` (`LIKE` substring semantics), so
History rows/bytes are investigative context rather than a promise to reproduce
the temporal hourly host total exactly.

The Review API reads the reconciled accounting authority through the normal
read-only Query API. It does not contact Mihomo or start/stop any runtime.

## 6. History

History is the primary evidence browser.

Use a flat chronological connection list/table.

Do not transform it into a nested process/domain/server tree unless future evidence proves that is more useful.

### 5.1 Current query dimensions

Use only dimensions supported by current Query API unless a narrowly-scoped frontend wrapper is missing for an already-existing endpoint:

- Time;
- Route;
- Process;
- Host;
- Destination IP;
- Network;
- exact Rule (Phase 4A Review investigation only).

Do not invent backend filters for:

- Rule Payload;
- Final Proxy;
- Destination Port;
- Attribution Class;

unless the task explicitly authorizes a future read-only API extension.

### 5.2 Default ordering

Current backend authority is newest-first.

Do not present arbitrary client-side column sorting as authoritative historical sorting.

### 5.3 Default row semantics

A normal row should communicate:

```text
Time
Process
Destination
Network
Route
Rule + Payload
Egress
Traffic
Evidence exception (only when meaningful)
```

Destination display:

```text
Host preferred
IP fallback
Port secondary
```

Traffic:

```text
Total primary
Upload / Download secondary
```

Evidence should remain quiet when normal and visible when abnormal.

---

## 6. Time Model: Live Analysis Range vs Frozen History Snapshot

ProxyLens balances live traffic awareness with stable, auditable evidence review by decoupling analytical surfaces from historical pagination:

### 6.1 Live Analysis Range (Overview & Coverage)

Overview and Coverage provide live analytical briefings over the current observation period:

- For rolling ranges (`today`, `7d`, `30d`), the upper boundary `to = now` advances on a low-frequency tick (~30s).
- Closed ranges (`yesterday`, fixed `custom`) remain strictly fixed over their defined `[from, to)` interval.
- Background re-queries preserve previous analytical data (`keepLiveTickOnly`) ONLY during monotonic live ticks (`to >= prevTo`) under the exact same semantic scope (identical `from`, identical `routeFocus`). Switching Route Focus, switching Time Windows, or editing custom intervals immediately enters an explicit loading transition to ensure stale evidence is never displayed under a newly active scope.
- Changing the time range in Overview or Coverage updates the live analysis range and invalidates any previous History snapshot, so entering History later freezes freshly at that entry instant.

### 6.2 Stable History Snapshot (History)

Offset pagination and causal investigation can drift if the Collector keeps appending new connections.

When entering History, the UI freezes an authoritative upper boundary `to = now`:

```text
History entry (from Overview/Coverage)
→ freeze `to = now` at entry moment
→ create stable History snapshot

filter/pagination/Inspector
→ preserve frozen snapshot `to`

returning to History (unchanged range)
→ preserve snapshot, active filters, page, and inspected row

changing range inside History
→ immediately freeze new snapshot `to = now`
→ reset to page 1

explicit Refresh
→ advance `to = now`
→ return to page 1
```

This preserves frozen `[from, to)` semantics and makes paginated audits reproducible.

---

## 7. History pagination

Current API supports:

- default limit 50;
- max 200;
- offset;
- `hasMore`.

Do not invent a total result count.

Recommended controls:

```text
Previous
Page N
Next
```

Optional page-size controls:

```text
50
100
200
```

It is acceptable to show a range such as:

```text
Showing 101–150
```

Do not show:

```text
Page 3 of 287
12,843 total results
```

unless the backend actually supplies an authoritative total.

Prefer explicit pagination over infinite scrolling for audit/history review.

---

## 8. Connection Inspector

The Inspector is a contextual evidence dossier for the selected historical connection.

Desktop preference:

```text
History list | Connection Inspector
```

The Inspector must preserve History context.

### 8.1 Information order

#### A. Causal Path

```text
Process
→ Destination
→ Rule + Payload
→ Top Policy Group
→ Intermediate Proxy Chain
→ Final Physical Egress
```

This is central to ProxyLens.

#### B. Traffic Accounting

- Upload;
- Download;
- Total;
- Raw vs accounted where meaningful;
- Exact vs interval-derived / estimated.

#### C. Evidence Quality

- attribution class;
- quality flags;
- ambiguous relay;
- preexisting at session start;
- possible unobserved tail;
- missing fields.

#### D. Lifecycle

- first observed;
- last observed;
- active/ended;
- end reason;
- session/epoch identifiers when useful.

#### E. Accounting Event Timeline

Show the evolution of evidence where available:

- process;
- host;
- rule;
- route;
- policy;
- final proxy;
- precision.

#### F. Raw Traffic Frames

Advanced/secondary.

Raw evidence should be accessible without overwhelming the default Inspector.

---

## 9. Coverage

Coverage explains monitoring completeness and evidence boundaries.

It is not a generic server-health dashboard.

### 9.1 Summary

Communicate:

- Covered;
- Uncovered / known gaps;
- Outside Known Scope;
- ratio only where mathematically meaningful.

### 9.2 Timeline semantics

Distinguish:

```text
Covered
Controller Gap
Collector Offline Gap
Outside Monitored History
Future (Post-Now Interval)
```

These states must not collapse into one generic red failure.

- `Future` represents the unoccurred portion of the requested window when `windowEnd > now`. It is rendered with neutral diagonal hatching, clearly distinguished from failure gaps, and its legend item is displayed conditionally only when `futureDurationMs > 0`.
- Estimated gap physical traffic: Controller gaps calculate window-level estimated physical bytes with a mandatory `[estimated]` evidence chip.

### 9.3 Gap list and bidirectional selection

For known gaps show, where available:

- source/type;
- start;
- end;
- duration;
- reason;
- estimated physical bytes / precision if supported.

Interaction model:

- Timeline and Gap rows support true bidirectional persistent selection based on stable data identity (`source(s) + startedAt + endedAt`), guaranteeing that selections never drift or jump to unrelated gaps during live 30s re-queries or list shifts:
  - Clicking a timeline gap segment selects it, highlights the corresponding table row, and smoothly scrolls it into view.
  - Clicking a gap table row selects it and highlights the corresponding timeline segment.
  - Clicking an already-selected segment or row deselects it.
- Timeline uses a semantic container (`role="region"`, `aria-label`) with interactive gap segments (`role="button"`, `tabindex="0"`, `aria-pressed`, Enter/Space support).
- Hover, Focus (`:focus-visible`), and Selected (`.pl-timeline__seg--selected`) states are visually and semantically distinct.

### 9.4 Investigation entry

Selecting a gap may expose:

```text
Inspect around this gap
```

which opens History around the gap boundaries.

Do not imply that ProxyLens can reconstruct missing connection evidence inside the gap.

---

## 10. System Status

System status is secondary and contextual.

Healthy state should be quiet.

The detailed system status is rendered as a lightweight, quiet surface dialog with full focus management (auto-focus, Tab focus trap, Esc closure, and focus return to trigger button).

System status presents Meta API authority facts only—no speculative advice or predictions:

- Query API;
- Database state and schema compatibility;
- Collector session, last heartbeat, and heartbeat interval;
- Accounting Engine run ID, algorithm version, and freshness lag events count.

Important distinction:

```text
isFresh = false
≠
system failure
```

---

## 11. Cross-surface continuity

Preserve investigation context where practical:

```text
Overview
→ History
→ Inspector
```

should preserve relevant:

- Time Range;
- Route Focus;
- filters;
- frozen History snapshot;
- page where appropriate.

Coverage gap investigation should carry a useful time window into History.

Returning from Inspector should not unexpectedly reset History context.

---

## 12. Required product states

The official UI must intentionally handle:

- Loading;
- Empty database/history;
- No filtered results;
- No completed accounting run;
- API unavailable;
- Database unavailable/incompatible;
- Collector healthy;
- Collector stale;
- Collector offline;
- Accounting fresh;
- Accounting stale;
- Monitoring gap;
- Outside monitored history;
- Missing attribution;
- Ambiguous relay;
- Sampling residual;
- Estimated / partial evidence.

Do not make every non-ideal state an Error.

---

## 13. Explicit V1 non-goals

Do not add UI for:

- editing Mihomo rules;
- switching nodes;
- killing connections;
- firewall controls;
- TUN/system proxy toggles;
- bandwidth limits;
- quota/billing controls;
- cloud sync;
- packet/payload inspection;
- world maps;
- generic geo visualization;
- settings with no real writable backing.

---

## 14. What is fixed vs visually open

### Fixed

- functional hierarchy;
- page responsibilities;
- context model;
- History pagination semantics;
- stable History snapshot;
- Connection Inspector role;
- Coverage semantics;
- required states;
- read-only product boundary.

### Visual implementation is governed by

```text
docs/design/PROXYLENS_UI_DESIGN_SYSTEM_v1.md
docs/design/PROXYLENS_UI_TOKEN_REFERENCE_v1.md
docs/design/PROXYLENS_UI_COMPONENTS_AND_PATTERNS_v1.md
```

The Coding Agent may solve exact layout details within those boundaries, but should not redesign the product model.
