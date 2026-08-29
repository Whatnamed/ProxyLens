# ProxyLens — UI Design System v1

**Status:** Draft — first implementation baseline  
**Theme strategy:** Light + Dark  
**Canonical current direction:** Light-first visual baseline; Dark is a coherent adaptation  
**Primary product character:** Quiet Technical Audit Workspace

---

# 1. Design intent

ProxyLens should feel like a mature local technical instrument for reconstructing why network traffic happened.

The interface should communicate:

```text
clarity
evidence
causality
trust
precision
restraint
```

It should not feel like:

- a generic SaaS dashboard;
- a cyber-security command center;
- a consumer VPN;
- a network controller;
- a spreadsheet clone;
- a marketing surface.

The interface is read-only and evidence-oriented.

---

# 2. Design principles

## 2.1 Content over chrome

The user's active analytical task should be more visually prominent than:

- navigation;
- app chrome;
- inactive filters;
- ordinary icons;
- helper metadata.

Navigation should help orientation, then recede.

## 2.2 Neutral first, semantic color second

The majority of the interface is neutral.

Color exists for:

- meaningful route classification;
- evidence/trust state;
- selection/focus where needed;
- limited analytical emphasis.

Color is not decoration.

## 2.3 Structure should be felt, not over-drawn

Prefer:

- alignment;
- spacing;
- small tonal changes;
- typography;
- a few purposeful separators;

instead of:

- a border around every section;
- card grids;
- repeated vertical grid lines;
- strong shadows.

## 2.4 Dense, not noisy

ProxyLens contains technical data.

Do not make controls and rows large simply to look “clean.”

Density should come from:

- compact geometry;
- alignment;
- progressive disclosure;
- strong type hierarchy;
- restrained secondary metadata.

## 2.5 Grayscale must still work

A page should remain understandable when semantic colors are removed.

Color is an additional layer of meaning, not the only hierarchy mechanism.

## 2.6 Evidence should look like evidence

Raw identifiers, timestamps, IPs, rules and accounting details should be easy to scan and compare.

Use technical typography and layout, not decorative “forensic” motifs.

## 2.7 Calm healthy state

Normal operation should not look like an alert dashboard.

Fresh, covered and healthy states are visually quiet.

The UI escalates only when evidence quality or availability materially changes.

---

# 3. Reference DNA

## 3.1 Linear — hierarchy and restraint

Take inspiration from:

- content over sidebar/chrome;
- compact navigation;
- strong selected state;
- dense lists that remain easy to scan;
- minimal unnecessary icon/background treatment;
- soft separators;
- consistent toolbar/header behavior.

Do not clone Linear's visual identity.

## 3.2 Supabase — implementation discipline

Take inspiration from:

- semantic theme tokens;
- separate but related sidebar/app surface tokens;
- reusable patterns;
- accessible table semantics;
- content-driven page width;
- compact filter/action placement.

Do not clone Supabase green or Studio composition.

## 3.3 Claude Console — mood only

Take only:

- calm contrast;
- low saturation;
- non-harsh neutral surfaces.

Do not reuse its recognizable brand palette.

---

# 4. Theme architecture

## 4.1 Light is canonical for visual character

The Light theme currently best expresses the desired balance:

- neutral;
- editorial;
- high-density;
- technical but not harsh;
- semantic color used sparingly.

New components should first make sense in Light, then verify Dark mapping.

This does **not** mean Dark is secondary in quality.

Both themes are first-class.

## 4.2 Dark is not a redesign

Dark preserves:

- same layout;
- same density;
- same component geometry;
- same information hierarchy;
- same semantic-color budget;
- same interaction model.

Dark must not add:

- more colorful labels;
- larger tinted panels;
- glow;
- extra decoration;
- cyber styling.

## 4.3 One neutral hue family per theme

Dark surfaces must not mix obviously brown and blue-gray slabs.

Use one low-chroma graphite family with small lightness steps.

Light should similarly use a coherent soft neutral family.

---

# 5. Color economy

## 5.1 Reserved semantic roles

Route:

```text
route-proxy
route-direct
route-reject
```

Evidence/system:

```text
status-fresh
status-stale
status-gap
status-offline

evidence-exact
evidence-estimated
evidence-ambiguous
evidence-missing
```

Selection is separate:

```text
accent-selection
```

Do not use route colors for arbitrary charts.

Do not use stale/estimated colors to represent Upload/Egress.

## 5.2 Preferred color footprint

From most preferred to least preferred:

1. small status dot;
2. icon;
3. compact text/badge;
4. thin indicator/timeline segment;
5. subtle soft fill where state truly belongs to an entire region.

Avoid full saturated cards.

## 5.3 History row color budget

A normal History row should usually expose:

```text
0–1 dominant semantic accents
```

Route is normally the one.

If evidence has a meaningful exception, add one small secondary indicator.

Do not color:

- process;
- destination;
- rule;
- egress;
- traffic;

all at once.

## 5.4 Analytical charts

If a chart needs series distinction unrelated to route/evidence semantics, use a separate neutral analytical scale.

Never overload route/status semantic colors.

---

# 6. Typography

## 6.1 Typeface families

English / Latin narrative:

```text
Public Sans
```

Simplified Chinese narrative:

```text
IBM Plex Sans SC
```

Technical:

```text
JetBrains Mono
```

These families are bundled as deterministic WOFF2 assets. The UI uses only
normal 400 Regular and 500 Medium weights at this stage. System fonts are a
last-resort fallback only; they are not part of the intended visual baseline.

The `html[lang="zh-CN"]` typography mapping selects IBM Plex Sans SC for the
Chinese interface, while English uses Public Sans. JetBrains Mono remains
locale-independent for technical/evidence values.

The visual system preserves:

```text
Sans narrative layer
+
Mono evidence layer
```

## 6.2 Sans usage

Use the narrative Sans family for:

- navigation;
- page titles;
- section titles;
- explanatory text;
- system messages;
- controls;
- ordinary user-facing labels.

## 6.3 Mono usage

Use Mono where it improves technical reading:

- timestamp;
- IP;
- port;
- byte value;
- duration where tabular comparison matters;
- connection/session/epoch ID;
- rule expression/payload;
- raw accounting events;
- protocol/network tokens;
- paths/identifiers when technical evidence is primary.

Process/host may remain Sans in narrative views and use Mono in dense evidence/table contexts if testing shows it improves scanning.

## 6.4 Mono is not decoration

Do not convert every subtitle or navigation item to Mono simply to look technical.

## 6.5 Number alignment

For:

- bytes;
- counts;
- duration;
- timestamps;

use Mono or tabular numerals so columns align.

---

# 7. Density

Use a compact desktop density.

Working targets:

```text
Base spacing unit        4px
Compact control          28–32px
History row              40–44px
Top-level page padding   24–32px
Inspector width          380–440px
```

These are implementation targets, not immutable dimensions.

Validate them in the actual Tauri window.

Avoid 44–52px tall generic SaaS controls unless a specific interaction needs them.

---

# 8. Shape and borders

## 8.1 Radius

Working language:

```text
2px  hairline/technical small detail
4px  default controls, chips, rows where radius is needed
6px  menus/popovers
8px  larger temporary overlays
```

Avoid pervasive 12–20px card rounding.

## 8.2 Borders

Use 1px neutral borders.

Prefer horizontal structure to full boxed regions.

Avoid vertical grid lines in tables.

## 8.3 Elevation

Core workspace uses:

```text
tonal surface
+
border
```

not shadows.

Shadows are reserved for temporary floating UI such as:

- menu;
- popover;
- tooltip;
- modal when necessary.

---

# 9. Motion

Motion explains state change.

It does not decorate.

Working timing:

```text
fast       100–140ms
standard   160–200ms
panel      180–220ms
```

Preferred easing:

```text
cubic-bezier(0.2, 0.7, 0.2, 1)
```

Use motion for:

- hover/focus;
- opening contextual overlays;
- Inspector content transition if needed;
- small disclosure transitions.

Avoid:

- looping ambient animations;
- glowing pulse;
- background movement;
- unnecessary row slide/fade on every update.

Respect reduced-motion preferences.

---

# 10. Iconography

Icons are:

- simple;
- monochrome by default;
- consistent stroke/fill language;
- secondary to text.

Do not place every icon inside a colored square.

Color an icon only when its semantic state needs color.

Avoid:

- shield-heavy cybersecurity clichés;
- radar;
- crosshair;
- skull/bug threat motifs;
- globe/world-map motifs as generic networking decoration.

---

# 11. Navigation

V1:

```text
Overview
History
Coverage
```

The navigation area should be quieter than the workspace.

Inactive items:

- muted text;
- muted monochrome icons.

Active item:

- subtle neutral selected surface;
- slightly stronger foreground;
- optional thin indicator.

Do not add:

- New Investigation;
- Settings;
- Documentation;

as V1 top-level destinations without product authorization.

---

# 12. Page composition

Use a workspace model, not a dashboard-card model.

Preferred structure:

```text
App Shell
├─ Navigation
└─ Workspace
   ├─ Context / compact toolbar
   └─ Content
```

For History:

```text
Navigation
|
History Workspace
|---------------------------|
Connection Table | Inspector
```

For Overview, use editorial sections and ranking rows.

For Coverage, use a temporal evidence layout.

---

# 13. Selection

Selection is one of the strongest interaction states.

Selected row should:

- remain clearly visible in Light/Dark;
- connect naturally to Inspector;
- use neutral surface change first;
- optionally use a thin selection accent.

Do not use a large saturated background.

Hover must be visibly weaker than selection.

Keyboard focus must be visible independently from selection.

---

# 14. Status hierarchy

Visual escalation:

```text
Normal / Fresh / Exact
→ quiet

Stale / Estimated / Partial
→ visible but restrained

Gap / Ambiguous / Missing Attribution
→ explicit semantic indicator

Unavailable / Incompatible
→ strongest treatment
```

Important:

```text
Estimated
≠ dangerous

isFresh=false
≠ system failure

Outside Monitored History
≠ monitoring failure
```

---

# 15. Product vocabulary

Use product/API vocabulary.

Do not invent security/forensic claims.

Forbidden unless later explicitly supported:

- Trust Score;
- malicious classification;
- threat score;
- cryptographic executable verification;
- Hash match as evidence authority;
- forensic chain-of-custody claims;
- government/institutional operator personas.

ProxyLens audits proxy/network routing evidence; it is not a malware analysis product.

---

# 16. Light / Dark parity acceptance

A component is not done until:

- Light is coherent;
- Dark uses the same geometry;
- semantic color footprint remains comparable;
- contrast remains accessible;
- no theme-specific product behavior exists;
- no theme requires different information hierarchy.

---

# 17. Explicit visual non-goals

Do not use:

- generic SaaS KPI card grids;
- excessive rounded cards;
- neon cyber styling;
- glowing graphs;
- gradients as primary structure;
- world maps;
- topology diagrams for decoration;
- glassmorphism for core workspace;
- heavy shadows;
- saturated row backgrounds;
- colored text everywhere;
- terminal-only visual identity;
- spreadsheet chrome as the product identity.

---

# 18. What is intentionally not frozen yet

The following may be tuned after the first real implementation:

- exact neutral hex values;
- exact semantic hue values;
- sidebar width;
- Inspector width;
- row height within the target range;
- exact type sizes/weights;
- exact border contrast;
- exact spacing between Overview sections.

Do not treat first-pass values as branding law before visual QA.

The design system must follow the real validated UI, not become a reason to preserve a poor first pass.
