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
History
Coverage
```

Do not add a V1 Settings page when there are no meaningful writable settings.

Connection Detail is contextual and should normally appear as an Inspector / drill-down rather than top-level navigation.

System Status is secondary and persistent/contextual rather than a primary page.

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

## 5. History

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
- Network.

Do not invent backend filters for:

- Rule;
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

## 6. Stable History Snapshot

Offset pagination can drift if the Collector keeps adding new connections.

When entering History, resolve a concrete upper boundary `to`.

For a live range such as Today:

```text
History entry
→ freeze `to = now`

filter/pagination/Inspector
→ preserve that `to`

explicit Refresh
→ advance `to`
→ return to page 1
```

This preserves the frozen `[from,to)` semantics and makes pagination auditable.

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
```

These states must not collapse into one generic red failure.

### 9.3 Gap list

For known gaps show, where available:

- source/type;
- start;
- end;
- duration;
- reason;
- estimated physical bytes / precision if supported.

### 9.4 Investigation entry

Selecting a gap may expose:

```text
Inspect around this gap
```

which opens History around the gap boundaries.

Do not imply that ProxyLens can reconstruct missing connection evidence inside the gap.

---

## 10. System Status

System status is secondary.

Healthy state should be quiet.

Relevant signals may include:

- Query API;
- Database;
- schema compatibility;
- Collector session/liveness;
- heartbeat;
- latest accounting run;
- freshness lag events.

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
