# ProxyLens — UI Components & Product Patterns v1

**Status:** Draft implementation specification  
**Goal:** Define only components/patterns that current product surfaces actually need.

---

# 1. Component philosophy

Do not create a large abstract component framework before real pages prove the need.

Prefer:

```text
small primitive
→ composed product pattern
→ actual page
```

A component becomes shared when repeated semantics justify it.

---

# 2. Base controls

## 2.1 Button

Variants:

```text
primary
secondary
quiet
danger (rare)
```

Geometry:

- compact;
- ~28–32px height;
- 4px default radius;
- no large pill default.

States:

- default;
- hover;
- pressed;
- focus-visible;
- disabled;
- loading where applicable.

Primary actions should be rare in a read-only product.

Do not create a large brand-colored CTA hierarchy across every page.

---

## 2.2 IconButton

Use for compact utility actions:

- refresh;
- copy;
- expand/collapse;
- close Inspector;
- overflow.

Default icon is monochrome.

Tooltip required for ambiguous icon-only actions.

---

## 2.3 Select / Dropdown

Use for bounded choices such as:

- Network;
- page size;
- supported route control where a segmented control is not appropriate.

Compact.

Application selects use a controlled listbox/menu composition rather than the
browser-native select popup when the surface must match the product shell.
Network and page-size menus share the same trigger height, raised surface,
border, radius, selected/hover/focus states and temporary elevation.

Menu uses raised surface, border and temporary elevation.

When open, the listbox itself is the only tab stop inside the option set and
owns the active option through `aria-activedescendant`. Arrow Up/Down and
Home/End move the highlighted option; Enter/Space commits it; Escape restores
focus to the trigger. Leaving the component by focus or pointer closes the
temporary menu.

---

## 2.4 FilterChip

Represents an active filter.

Must communicate:

- filter dimension;
- current value;
- removable state if applicable.

Example:

```text
Process: chrome.exe   ×
Host: api.github.com  ×
```

Do not turn all filter chips into strongly colored pills.

Neutral by default.

---

## 2.5 RouteControl

For:

```text
PROXY
DIRECT
REJECT
ALL
```

Use a compact segmented control or equivalent.

Selection should be primarily structural/neutral.

Small route semantic color may reinforce the selected route but should not fill the whole toolbar with color.

---

## 2.6 TimeRangeControl

Supports:

```text
Today
Yesterday
7d
30d
Custom
```

Must reflect local-time semantics defined by frozen contract.

Custom range UI should expose clear start/end context.

The custom range editor uses a composed date/time picker: a compact trigger,
tokenized calendar popover and 24-hour time input. It must not expose the
browser-native datetime picker, default blue calendar selection or an
un-themed system panel.

The calendar uses `grid → row → gridcell` semantics and keeps exactly one day
cell in the tab sequence. Arrow keys move by day or week, including across
month boundaries; Home/End move to the visible row boundary. Focus leaving the
picker closes the popover, while the selected date remains a separate state
from the keyboard focus date.

Do not silently convert to ambiguous rolling-day semantics.

---

## 2.7 Tooltip

Use for:

- technical abbreviation;
- truncated technical field;
- icon-only action;
- semantic explanation.

Tooltip is explanatory, not a replacement for core labels.

---

## 2.8 Popover

Use for compact contextual choices/details.

Menus, listboxes and date pickers are one overlay family: the same raised
surface, neutral border, 6px radius, restrained shadow and focus-ring language.
Temporary overlays close on outside pointer interaction, Escape and focus
leaving the component.

Popover should not become a substitute for the persistent Connection Inspector.

---

## 2.9 LocalePreference

Language is a global UI preference, not a page filter. Keep its entry point in
the sidebar footer utility area alongside Theme, use a compact segmented
control, apply changes immediately and persist the choice locally. Translate
interface copy and locale-aware date/time presentation; preserve process names,
domains, addresses, protocols, route values and other raw technical evidence.

---

## 2.10 Inline alignment

All compact primitives share one vertical text-box model:

- fixed-height controls, segmented items, select options, chips, badges, status
  indicators and legend items are optically centered;
- key/value columns and evidence timelines align their key/timestamp and
  primary content to the first baseline;
- a Status marker is grouped with its primary label; Causal Path uses the first
  line `marker | key | primary value`, with path/IP secondary content below the
  value column; Accounting Events uses `marker | timestamp | primary event
  content`, with later evidence details in a separate secondary row. The
  key/timestamp never defines marker position: the dot follows the primary
  first-line optical center, never the wrapped block's total height; connector
  segments remain behind the dot.

Compact labels use the control/token leading role rather than body leading. A
supported `text-box` trim is optional progressive enhancement for single-line
primary wrappers only, with a normal leading/flex/grid fallback. Marker
placement comes from the explicit primary-first-line layout; do not introduce
locale-, font-, string- or page-specific offsets, padding or transforms to
correct text placement.

---

# 3. Semantic indicators

## 3.1 RouteBadge

Supported:

```text
PROXY
DIRECT
REJECT
```

Design:

- compact;
- small semantic accent;
- readable text;
- not a large filled capsule.

Route badge is normally the dominant color accent in a History row.

---

## 3.2 StatusIndicator

For:

```text
Fresh
Stale
Gap
Offline
```

Default form:

```text
small dot/icon + neutral/semantic short label
```

Healthy/fresh should be quiet.

Do not render every healthy status as a green card.

---

## 3.3 EvidenceIndicator

Examples:

```text
Exact
Estimated
Ambiguous Relay
Missing Attribution
Preexisting
Possible Unobserved Tail
```

Use:

- short label;
- icon;
- restrained semantic accent;
- tooltip/details where explanation is required.

Exact can often remain neutral.

Estimated is caution/precision metadata, not an error.

---

# 4. App Shell

## 4.1 Sidebar

Contains:

```text
ProxyLens identity
Overview
History
Coverage
secondary system status / utility if needed
```

Rules:

- quieter than workspace;
- monochrome icons;
- active item uses neutral selected surface;
- no huge brand/color block;
- no unnecessary nav groups in V1.

A collapsed/icon mode may be considered only if it improves actual desktop use; do not implement merely because a library supports it.

---

## 4.2 AuditContextBar

Shared compact context surface for:

- Time Range;
- Route Focus where applicable;
- History snapshot status;
- refresh when relevant.

It should feel like workspace context, not a dashboard header.

Keep controls near the data they affect.

---

# 5. Overview patterns

## 5.1 TrafficSummary

Purpose:

```text
how much traffic happened in selected scope?
```

Use:

- typographic hierarchy;
- compact aligned values;
- neutral color by default.

Upload/Download are not semantic status categories.

Do not map them to Fresh/Stale or Direct/Reject colors.

---

## 5.2 EvidenceTrustSummary

Surfaces:

- Coverage;
- Freshness;
- missing attribution;
- ambiguous relay;
- residual where relevant.

Do not invent a single Trust Score.

Use a small number of explicit evidence facts.

---

## 5.3 RankingList

Used for:

- Top Processes;
- Top Hosts;
- Top Rules;
- Top Final Proxies.

A row may contain:

- rank/index;
- primary label;
- optional secondary context;
- traffic value;
- optional percentage/bar;
- drill-down affordance if supported.

Visual rules:

- neutral text;
- minimal separators;
- no card per row;
- no rainbow category colors;
- aligned numeric values.

If a bar is useful, prefer a single analytical accent/neutral scale rather than semantic route colors.

---

# 6. History patterns

## 6.1 HistoryToolbar

Contains only controls relevant to History:

- explicit filters;
- snapshot/refresh;
- optional page size;
- compact result range.

Avoid a universal search field if the backend does not provide universal semantic search.

---

## 6.2 HistoryTable

Primary columns:

```text
Time
Process
Destination
Network
Route
Rule
Egress
Traffic
Evidence
```

Exact widths may adapt to viewport.

Recommended priorities:

### Time

Compact Mono.

### Process

Readable, truncatable.

Path not required in aggregate row.

### Destination

Host first.

IP/port secondary or fallback.

### Network

Compact technical token.

### Route

RouteBadge.

### Rule

Rule + payload where space permits.

Truncate with tooltip when needed.

### Egress

Final proxy or DIRECT/REJECT semantic equivalent.

Keep text neutral.

### Traffic

Total primary.

Upload/down may appear secondarily.

Mono/tabular.

### Evidence

Normally empty/quiet.

Show only meaningful exception.

---

## 6.3 HistoryRow

Default:

- neutral;
- compact;
- no colored background.

Hover:

- slight neutral surface shift.

Selected:

- stronger neutral surface;
- optional thin selection accent;
- must visually connect to Inspector.

Abnormal evidence:

- one small secondary indicator.

Do not color multiple cells according to route.

---

## 6.4 Pagination

Supported semantics:

```text
Previous
Page N
Next
50 / 100 / 200
```

Use `hasMore`.

Do not fabricate total pages/results.

---

# 7. Connection Inspector patterns

## 7.1 Inspector shell

Inspector is persistent/contextual within History.

It is not a floating marketing-style card.

Use:

- one clear boundary from table;
- workspace-compatible surface;
- internal sections separated by spacing/dividers.

Suggested width:

```text
~380–440px
```

Tunable during visual QA.

---

## 7.2 InspectorHeader

Show:

- primary process/destination context;
- Route;
- close/copy actions where useful;
- compact identity metadata.

Avoid a large hero header.

---

## 7.3 CausalPath

Core order:

```text
Process
→ Destination
→ Rule + Payload
→ Top Policy Group
→ Proxy Chain
→ Final Physical Egress
```

Visual style:

- compact linear reasoning trace;
- neutral nodes/labels;
- restrained connectors;
- semantic color only where route/evidence state actually matters.

Do not:

- assign a different color to every stage;
- use large node cards;
- make a network-topology graph;
- use animated flow/glow.

---

## 7.4 AccountingSummary

Show:

- Upload;
- Download;
- Total;
- precision/source metadata;
- raw/accounted distinction where available.

Values neutral.

Estimated/interval-derived state can be marked by a small evidence indicator.

---

## 7.5 EvidenceQuality

Explicitly present:

- attribution class;
- quality flags;
- ambiguous relay;
- preexisting;
- possible unobserved tail;
- missing evidence.

Long explanatory text stays neutral.

Only the semantic state label/icon carries color.

---

## 7.6 Lifecycle

Show:

- first observed;
- last observed;
- active/ended;
- end reason;
- session/epoch where useful.

Use compact key/value or aligned technical list.

---

## 7.7 AccountingEventTimeline

This is a structured evidence timeline, not a social activity feed.

Each event may show:

- timestamp;
- changed/observed evidence;
- route/rule/policy/final proxy;
- precision.

Use restrained line/markers.

Default markers neutral.

Only meaningful state transitions require semantic color.

---

## 7.8 RawTrafficFrames

Advanced disclosure.

Collapsed by default unless real use proves otherwise.

Use dense Mono presentation.

Avoid turning raw evidence into visually dominant content.

---

# 8. Coverage patterns

## 8.1 CoverageSummary

Explicit facts:

- covered duration;
- known uncovered/gap duration;
- outside monitored history;
- ratio where meaningful.

No invented trust score.

---

## 8.2 CoverageTimeline

Segments:

```text
Covered
Controller Gap
Collector Offline
Outside Monitored History
```

Recommended semantics:

- Covered: neutral/subdued positive;
- Controller Gap: restrained warning;
- Collector Offline: strongest gap/offline treatment;
- Outside History: neutral/hatch/structural distinction.

The timeline should still be understandable by label/pattern without color.

---

## 8.3 GapList

Rows remain neutral.

Color only the gap-type indicator.

Show:

- type;
- start;
- end;
- duration;
- reason;
- estimated bytes/precision when real.

Action:

```text
Inspect around this gap
```

opens relevant surrounding History window.

---

# 9. System status pattern

System status is secondary chrome/context.

Healthy:

- quiet;
- no prominent green banner.

Stale:

- subtle visible indicator.

Unavailable/incompatible:

- prominent enough to explain why content cannot load.

Do not confuse:

```text
stale accounting
with
Query API failure
```

---

# 10. Empty/loading/error states

## Loading

Prefer:

- stable layout skeleton;
- subtle progress;
- no excessive shimmer.

## Empty database/history

Explain:

- no recorded history / no completed run;
- what that means.

Do not imply an error.

## No filtered results

Preserve filters.

Offer clear filter reset.

## API unavailable / DB incompatible

Use strong explicit error surface with diagnostic facts.

Do not hide behind generic “Something went wrong.”

---

# 11. Accessibility

Components must not rely on color alone.

Interactive controls must support:

- keyboard focus;
- clear focus-visible treatment;
- reasonable hit targets;
- semantic HTML where applicable;
- table semantics;
- labels/tooltips for icon-only controls.

Dense desktop UI does not excuse inaccessible interaction.
