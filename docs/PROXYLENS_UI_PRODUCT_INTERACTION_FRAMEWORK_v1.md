# ProxyLens — UI Product & Interaction Framework v1

- **Status**: Draft — Phase 3B Product Framework
- **Date**: 2026-08-28
- **Scope**: Product structure, interaction model, investigation flow, pagination/filter behavior, and cross-surface navigation
- **Visual Design**: Intentionally unspecified
- **Implementation Baseline**: `feat/phase-3b-pre-ui-readiness` @ `0382e44`

---

## 1. Purpose

This document defines the product and interaction framework for the first formal ProxyLens desktop UI.

It intentionally does **not** define:

- visual style;
- light / dark appearance;
- color palette;
- typography;
- spacing scale;
- radius;
- concrete card or table styling;
- navigation visual treatment;
- iconography;
- animation style;
- exact grid layout;
- specific component library.

Those decisions belong to the visual-design / implementation stage.

This document exists to keep Phase 3B implementation focused on the actual product workflow instead of embedding a large amount of product reasoning directly into an implementation prompt.

---

## 2. Relationship to Existing Frozen UI Documents

This document supplements, rather than replaces, the existing frozen contracts:

- `docs/ui-information-architecture-v1.md`
  - defines the functional UI surfaces and required information blocks;
- `docs/ui-semantic-presentation-v1.md`
  - defines non-negotiable display semantics such as timezone, exact vs estimated, coverage semantics, and route classification;
- `docs/ui-api-contract-v1.md`
  - defines the read-only Query API contract available to the frontend;
- `docs/ui-api-coverage-matrix-v1.md`
  - maps current UI needs to existing Query API endpoints.

If this document conflicts with a frozen semantic or API contract, the frozen contract wins.

The role of this document is to define **how the existing capabilities should behave as one coherent product**.

---

## 3. Product Model

ProxyLens is not primarily a traffic meter or a generic network dashboard.

Its first-order job is to help the user investigate and explain historical proxy behavior.

The product should support four core user actions:

1. **Scan**
   - Quickly understand what proxy traffic happened in the selected time range.

2. **Investigate**
   - Narrow the aggregate picture down to the actual historical connections that produced it.

3. **Explain**
   - Inspect one connection and understand its complete routing cause-and-effect chain.

4. **Verify**
   - Confirm whether the data in the selected time range is sufficiently complete and fresh to trust.

These actions map to four product surfaces:

```text
Scan        -> Overview
Investigate -> History
Explain     -> Connection Inspector
Verify      -> Coverage
```

Future Audit Intelligence may add a fifth action (`Review`) later, but it is not part of the current Phase 3B/3C product framework.

---

## 4. Primary Information Architecture

The V1 desktop application should expose only three top-level destinations:

```text
Overview
History
Coverage
```

The following are **not** top-level destinations:

- Connection Detail
  - opened contextually from History as an Inspector / drill-down surface;
- System Status
  - exposed through persistent application chrome / status affordance;
- Settings
  - not present in V1 because ProxyLens remains read-only and currently has no meaningful configuration surface.

The concrete visual form of navigation is intentionally unspecified.

It may be implemented as sidebar navigation, a compact navigation rail, top navigation, tabs, or another desktop-appropriate pattern as long as the functional hierarchy above is preserved.

---

## 5. Global Audit Context

ProxyLens should behave like one continuous investigation workspace rather than three disconnected pages.

Two pieces of state form the shared audit context:

```text
Time Range
Route Focus
```

### 5.1 Time Range

Supported shortcuts remain:

- Today
- Yesterday
- Last 7 Days
- Last 30 Days
- Custom range

Time semantics must follow `ui-semantic-presentation-v1.md`.

The selected time range should be preserved when moving among Overview, History, and Coverage where relevant.

### 5.2 Route Focus

Supported values:

```text
PROXY
DIRECT
REJECT
ALL
```

Default focus:

```text
PROXY
```

Route focus should be preserved between Overview and History.

Coverage is independent of route classification and therefore does not inherit route filtering.

---

## 6. Overview

### 6.1 Role

Overview is an **audit entry surface**, not a generic analytics dashboard.

Its purpose is to answer, at a glance:

- how much proxy traffic occurred;
- which processes generated it;
- where it went;
- why it was routed that way;
- which physical egress nodes were used;
- whether the evidence is complete and fresh enough to trust.

### 6.2 Information Priority

Overview should present information in this conceptual order:

1. **Scope**
   - current time range;
   - current route focus.

2. **Traffic Summary**
   - Proxy;
   - Direct;
   - Reject;
   - Upload / Download / Total where available.

3. **Evidence Trust**
   - monitoring coverage;
   - accounting freshness;
   - missing attribution;
   - ambiguous relay;
   - sampling residual;
   - other explainable evidence-quality conditions.

4. **Who / Why / Where / Through What**
   - Top Processes;
   - Top Rules;
   - Top Hosts;
   - Top Final Proxies;
   - Protocol / Network breakdown as a secondary dimension.

The concrete layout, visual hierarchy, use of cards/tables/bars, and arrangement of these blocks remain open to visual design.

### 6.3 Important Principle

A traffic total without trust context is incomplete.

For example:

```text
3.2 GiB PROXY
```

is much more meaningful when accompanied by evidence such as:

```text
Coverage 99.8%
Fresh
Missing Attribution 0.2%
```

Coverage and freshness therefore must not be buried in an unrelated settings or system page.

### 6.4 Drill-down Behavior

Overview must not be a dead-end display.

Where the backend supports it, aggregate items should open History with the current audit context preserved and a corresponding filter applied.

Examples:

```text
Top Process: chrome.exe
-> History
-> same time range
-> same route
-> process = chrome.exe
```

```text
Top Host: github.com
-> History
-> same time range
-> same route
-> host = github.com
```

The product principle is:

```text
aggregate -> underlying evidence
```

rather than:

```text
aggregate -> larger decorative chart
```

Current backend limitations for Rule / Final Proxy drill-down are documented later in this file.

---

## 7. History

### 7.1 Role

History is the authoritative historical connection investigation surface.

It should expose actual connection records rather than hide them behind nested aggregation trees.

Default model:

```text
flat connection list/table
newest first
```

Overview owns aggregation; History owns evidence.

### 7.2 Query Model

History should preserve the current audit context:

- Time Range;
- Route Focus.

Additional filters should map clearly to real backend capabilities.

Current supported filters:

- Process;
- Host / Domain;
- Destination IP;
- Network (TCP / UDP).

These should be presented as explicit filters rather than a misleading universal full-text search.

A token/chip-style filter model is recommended conceptually:

```text
Process: chrome.exe
Host: github.com
Network: UDP
```

The exact visual implementation remains open.

### 7.3 History Columns / Row Semantics

The default connection representation should prioritize:

- Time;
- Process;
- Destination;
- Network;
- Route;
- Rule;
- Egress;
- Traffic;
- Evidence quality when abnormal.

Recommended semantic behavior:

- Process:
  - show process name;
  - missing attribution must be explicitly labeled rather than shown as generic Unknown.

- Destination:
  - prefer Host when available;
  - fall back to Destination IP;
  - Destination Port is secondary but visible.

- Rule:
  - Rule + Rule Payload.

- Egress:
  - physical final proxy for PROXY;
  - DIRECT where appropriate.

- Traffic:
  - Total is primary;
  - Upload / Download are secondary but available.

- Evidence:
  - surface only meaningful quality flags / incomplete evidence states.

The UI should not expose every backend field as a default table column.

### 7.4 Sorting

Current backend ordering is authoritative:

```text
first_observed_at DESC
```

V1 should therefore behave as:

```text
Newest First
```

Do not expose client-side sorting that only reorders the currently loaded page while implying global history sorting.

Additional sort controls should only be introduced when the Query API supports corresponding authoritative ordering.

---

## 8. History Pagination

The current Query API uses:

```text
limit
offset
hasMore
```

and does not provide total count.

Therefore V1 should use explicit page-style navigation rather than infinite scrolling.

Recommended default:

```text
50 rows / page
```

Optional page sizes:

```text
50
100
200
```

Navigation can expose:

```text
Previous
Page N
Next
```

and the currently displayed row interval, such as:

```text
Showing 101-150
```

Do not display unsupported values such as:

```text
Page 3 of 287
12,843 total results
```

unless a future backend contract provides total count.

### 8.1 Why Explicit Pagination

History is an investigation tool, not a content feed.

Users need to:

- preserve location;
- compare adjacent records;
- open a connection and return;
- move deliberately through older evidence.

Explicit pagination supports this better than infinite scrolling.

---

## 9. Stable History Snapshot

Collector data can continue growing while the user is investigating history.

Because current pagination is offset-based, newly inserted records could otherwise shift later pages.

To keep an investigation stable:

1. when History is opened, resolve a concrete upper bound for the query;
2. for a live shortcut such as Today, freeze `to` at the current time;
3. preserve that `to` while filtering, paging, and opening connection details;
4. expose a Refresh action to advance the snapshot to the new current time;
5. Refresh returns the investigation to the first page.

Conceptually:

```text
History snapshot as of 16:42:18
```

This does not require cursor pagination and remains compatible with the existing `[from, to)` Query API semantics.

---

## 10. Connection Inspector

### 10.1 Role

Connection Detail should be treated as an **Inspector**, not as a top-level destination.

Opening a connection should preserve the surrounding History context.

On desktop-sized windows, a split-pane / side-inspector model is preferred conceptually because it supports rapid comparison among multiple adjacent connections.

On narrower windows, the same content may use an overlay or full-detail surface.

Exact layout is a visual-design decision.

### 10.2 Information Order

The Inspector should organize evidence in the following conceptual order.

#### A. Causal Path

This is the most important section.

The user should be able to read:

```text
Process
-> Destination (Host / IP : Port)
-> Rule + Rule Payload
-> Top Policy Group
-> Intermediate Proxy Chain
-> Final Physical Egress
```

The routing chain must follow the frozen ProxyLens chain semantics.

#### B. Traffic Accounting

Expose:

- Upload;
- Download;
- Total;
- Raw vs Accounted;
- Exact vs interval-derived / estimated where relevant.

#### C. Evidence Quality

Expose relevant evidence such as:

- attribution class;
- quality flags;
- ambiguous relay;
- preexisting-at-session-start;
- possible unobserved tail;
- other incomplete-evidence conditions.

#### D. Lifecycle

Expose:

- first observed;
- last observed;
- current / ended state;
- observation end reason;
- session / epoch identity where useful.

#### E. Accounting Event Timeline

Expose `accountingEvents[]` as the historical audit trail.

The user should be able to understand meaningful evolution in:

- process attribution;
- host;
- rule;
- route;
- policy group;
- final proxy;
- accounting precision / classification.

#### F. Raw Traffic Frames

Raw traffic samples are advanced evidence.

They should remain accessible, but they do not need to dominate the default Inspector view.

A collapsed section, advanced section, or secondary tab is appropriate.

---

## 11. Coverage

### 11.1 Role

Coverage is a **data trust / evidence completeness** surface.

It is not merely a system-health dashboard.

### 11.2 Required Sections

#### A. Coverage Summary

For the selected time range:

- covered duration;
- uncovered duration;
- outside known monitored history;
- coverage ratio when meaningful.

If coverage ratio is undefined because the selected period is outside known monitored history, display that semantic state rather than `0%`.

#### B. Coverage Timeline

The selected range should conceptually distinguish:

```text
Covered
Controller Gap
Collector Offline Gap
Outside Monitored History
```

The exact visual form of this timeline remains open.

#### C. Gap List

Each monitoring gap should expose:

- source;
- start;
- end;
- duration;
- reason;
- estimated physical bytes when available;
- precision / estimated semantics.

Selecting a gap should highlight the corresponding timeline interval where practical.

### 11.3 Cross-surface Investigation

Coverage may provide an action such as:

```text
Inspect around this gap
```

which opens History around the gap boundary so the user can inspect evidence immediately before and after the interruption.

The UI must never imply that individual connections inside the unobserved gap can be reconstructed when the underlying evidence does not exist.

---

## 12. System Status

System Status should not consume a top-level page.

A persistent status affordance should expose concise operational state:

- Collector health;
- Accounting freshness;
- current-range coverage trust where useful.

Detailed status may be opened through a popover / panel / secondary surface and include:

- Query API state;
- DB state;
- schema version;
- Collector Session;
- heartbeat;
- latest Accounting Run;
- lagEvents;
- freshness state.

Normal status should remain quiet.

Operational problems should become prominent only when they materially affect evidence or usability.

`isFresh = false` must not be presented as equivalent to a system failure.

---

## 13. Cross-surface Navigation Rules

The application should preserve investigation continuity.

Examples:

```text
Overview
Top Process: chrome.exe
-> History(process=chrome.exe)
-> select connection
-> Inspector
```

```text
Overview
Coverage warning
-> Coverage
-> select gap
-> Inspect around gap
-> History
```

Back / close behavior should return the user to the previous investigation state rather than resetting filters, page, time range, or route focus.

A connection selected from History should not destroy the underlying list context.

---

## 14. Required Product States

The formal UI must account for at least:

- initial loading;
- empty database / empty history;
- no matching filtered results;
- no completed accounting run;
- Query API unavailable;
- database unavailable;
- incompatible schema;
- Collector running;
- Collector stale / offline;
- accounting fresh;
- accounting stale;
- monitoring gap;
- outside monitored history;
- missing attribution;
- ambiguous relay;
- sampling residual;
- estimated / interval-derived data;
- partial / incomplete evidence.

These states are product states, not merely developer error messages.

---

## 15. Current Backend Gaps Relevant to Product Design

The following are known limitations of the current Query API.

They should not trigger speculative backend redesign during Phase 3B unless explicitly approved.

### 15.1 No Timeseries Analytics Endpoint

There is currently no dedicated UI endpoint such as:

```text
GET /api/v1/analytics/timeseries
```

A time-based investigation graph may be valuable in the future, but it is deferred until formal product design proves it necessary.

Do not invent a decorative trend chart merely because dashboards commonly contain one.

### 15.2 History Cannot Currently Filter by Some Overview Dimensions

Current History filtering does not include:

- Rule;
- Rule Payload;
- Final Proxy;
- Destination Port;
- Attribution Class.

This means some Overview aggregates cannot yet drill down directly to their underlying History rows.

These are legitimate future Phase 3C read-only API extension candidates.

Likely priority:

1. Rule / Rule Payload;
2. Attribution Class;
3. Final Proxy;
4. Destination Port.

### 15.3 No Authoritative Global Sorting Options

Do not expose UI sorting for Traffic, Process, Host, or other columns until the backend supports corresponding authoritative ordering.

### 15.4 No Total History Count

Do not fabricate total pages or result counts.

---

## 16. Explicit Non-goals

The current UI should not add:

- Mihomo proxy selection;
- node switching;
- latency testing;
- connection termination;
- firewall allow / deny controls;
- rule editing;
- TUN controls;
- system proxy controls;
- bandwidth limits;
- quotas;
- package billing configuration;
- world map visualization;
- configuration editor;
- generic settings page;
- cloud telemetry;
- content inspection or packet payload display.

ProxyLens V1 remains a read-only audit and observability tool.

---

## 17. Phase 3B Group-B Experiment Boundary

For the Group-B HY4 experiment, the following are considered product-framework decisions and should be respected:

- top-level product model;
- Overview / History / Coverage hierarchy;
- Connection Detail as contextual Inspector;
- System Status as secondary persistent status surface;
- shared Time Range and Route audit context;
- Overview aggregate -> History evidence drill-down;
- explicit History filters;
- newest-first authoritative history;
- offset-aware explicit pagination;
- stable History snapshot / refresh behavior;
- Coverage as evidence-trust workflow;
- preserved investigation context across navigation;
- complete product state handling.

The following remain intentionally open for HY4 to design autonomously:

- navigation visual form;
- overall visual language;
- light / dark theme;
- typography;
- palette;
- spacing;
- component styling;
- exact grid;
- Overview layout;
- ranking visualization;
- table styling;
- Inspector width / presentation;
- Coverage timeline visual form;
- icons;
- motion;
- micro-interaction treatment;
- design system details.

This separation is intentional.

Group B tests whether a model can turn a clearly defined product and interaction framework into a strong UX/UI and production frontend without being given a visual design.

---

## 18. Acceptance Principle

The Phase 3B frontend should be judged by whether it enables a user to complete the following investigation naturally:

```text
Something used unexpected proxy traffic
        ↓
See the aggregate pattern
        ↓
Identify process / host / rule / egress
        ↓
Open the underlying historical connections
        ↓
Inspect one connection's causal routing chain
        ↓
Verify accounting and evidence quality
        ↓
Confirm whether the selected time range is trustworthy
```

A visually polished interface that cannot support this flow is not a successful ProxyLens UI.

A dense technical interface that exposes every backend field without clear prioritization is also not successful.

The product must preserve ProxyLens's core principle:

```text
Audit / Explainability > Traffic Statistics
```
