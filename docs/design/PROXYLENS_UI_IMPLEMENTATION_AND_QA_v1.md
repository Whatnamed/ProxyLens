# ProxyLens — UI Implementation & Visual QA v1

**Status:** Draft implementation guidance

---

# 1. Design implementation strategy

Do not start by hand-styling full pages with local colors.

Build:

```text
Theme tokens
→ typography
→ small primitives
→ App Shell
→ product patterns
→ pages
```

The first implementation is allowed to evolve the Draft Design System when real visual evidence shows a better solution.

Update docs and code together.

---

# 2. Suggested frontend organization

Adapt to the current repo rather than force this exact tree.

Conceptual target:

```text
ui/src/
├─ styles/
│  ├─ tokens.css
│  ├─ themes.css
│  ├─ typography.css
│  └─ globals.css
├─ components/
│  ├─ ui/
│  └─ audit/
└─ features/
   ├─ overview/
   ├─ history/
   └─ coverage/
```

Avoid creating multiple design-token sources of truth.

---

# 3. Component-library strategy

It is acceptable to use accessible primitives from:

- existing project dependencies;
- shadcn/Radix-style primitives;
- a small compatible library already justified by the project.

Do not import a large third-party dashboard theme.

Do not copy proprietary Linear/Claude components.

Supabase is a useful implementation reference, not a theme dependency that ProxyLens must resemble.

---

# 4. First visual stress test: History + Inspector

Implement first:

```text
App Shell
+
History context/filter bar
+
History table
+
selected row
+
Connection Inspector
+
Light/Dark
```

Why first:

This surface simultaneously validates:

- density;
- color economy;
- Sans/Mono;
- selection;
- data table;
- long technical strings;
- semantic states;
- split layout;
- Inspector;
- Light/Dark surface hierarchy;
- pagination.

If History + Inspector works, Overview/Coverage become easier.

---

# 5. Second: Overview

Then build Overview using the same primitives.

Do not invent a separate dashboard visual language.

Check that:

- aggregate rankings feel like the same product as History;
- semantic color remains restrained;
- top-level numbers do not become giant card grid;
- Coverage/Freshness are visible but not alarmist.

---

# 6. Third: Coverage

Coverage validates:

- time/evidence semantics;
- timeline treatment;
- warning hierarchy;
- outside-history semantics;
- gap investigation continuity.

---

# 7. Real data only

Use:

```text
React
→ Go Local Query API
→ read-only SQLite
```

and repository synthetic fixtures.

Do not use static fake JSON as the final implementation path.

Mock data may be used transiently during component construction only if it is quickly replaced and does not become a second source of truth.

---

# 8. No speculative backend work

Do not modify frozen Collector/Storage/Accounting/Tauri architecture for UI convenience.

Do not add chart/time-series APIs because a dashboard “normally has charts.”

If an already-existing Query API endpoint lacks a small frontend wrapper, adding the wrapper is allowed.

Any true backend contract extension should be separately justified.

---

# 9. Required visual QA sizes

Review at least:

```text
1280 × 800
1440 × 900
1600 × 1000
```

Also inspect a maximized high-resolution desktop window.

Validate:

- truncation;
- horizontal pressure;
- Inspector width;
- table readability;
- sidebar dominance;
- popover positioning;
- calendar and listbox overlays use the shared surface/border/radius/shadow family;
- no browser-native select or datetime picker remains on official product surfaces.

---

# 10. Required data/state fixtures

Use available synthetic profiles:

```text
healthy
gaps
stale
empty
scaled
```

At minimum visually inspect:

### Healthy
normal default hierarchy.

### Gaps
Coverage and semantic warnings.

### Stale
freshness treatment without generic error styling.

### Empty
empty database/history behavior.

### Scaled
table/render/query usability with large realistic data.

---

# 11. Light/Dark visual QA

## Light

Check:

- neutral surfaces;
- text contrast;
- semantic accent footprint;
- borders not too faint/strong;
- no over-warm Claude-like brand palette.

## Dark

Check:

- one graphite family;
- no brown-sidebar/blue-content split;
- no neon;
- semantic color footprint no larger than Light;
- selected row vs hover clear;
- Inspector boundary clear without card/shadow;
- muted text still readable.

---

# 12. Color audit

For every completed screen ask:

1. Does this color encode a real semantic state?
2. If removed, is hierarchy still understandable?
3. Is the same semantic color used elsewhere for a different meaning?
4. Are too many colored labels visible at once?
5. Could this be neutral text + a small dot instead?

If ordinary data is colorful, reduce it.

---

# 13. Grayscale test

Temporarily inspect the screen without semantic color.

The page should still make sense.

If not, improve:

- typography;
- spacing;
- grouping;
- selection;
- labels.

Do not solve hierarchy with more color.

## 13.1 Final contextual surface audit

Confirm that only the six approved aliases are used for contextual surface
hierarchy:

```text
--pl-inspector                 Inspector surface
--pl-row-selected              selected History row
--pl-sidebar-hover             sidebar navigation hover
--pl-sidebar-selected          sidebar navigation active
--pl-control-selected          Time Range / Route / Locale active segment
--pl-control-selected-border  active segment boundary
```

The generic overlay family remains independent: DatePicker and listbox menus
use the shared raised surface, neutral border, 6px radius, restrained shadow
and focus ring. RouteBadge, EvidenceChip and NetworkToken share 18px / 7px /
2px / 10px compact geometry while retaining separate semantic treatments.
The Overview ranking grid uses spacing, not a replacement divider, between
sections.

---

# 14. Typography audit

Check:

- Sans remains readable and calm;
- Mono is used for evidence, not decoration;
- technical columns align;
- uppercased tracked labels are not overused;
- titles are not oversized;
- long rules/hosts do not destroy layout.

## 14.1 Deterministic font delivery

- Bundle only the canonical normal WOFF2 weights required by the UI: 400 and
  500;
- use the script-aware Narrative composition (`--pl-font-narrative-latin` for
  Latin glyphs and `--pl-font-narrative-cjk` for Han/CJK glyphs); formal
  production uses Manrope for Latin, Sarasa Gothic UI SC for Han/CJK, and
  JetBrains Mono for technical/evidence values;
- keep system fonts as last-resort fallbacks; do not require a system-wide
  install or a runtime font CDN;
- map Sarasa's native SemiBold source to CSS weight 500 because the family has
  no native Medium face; enable `font-synthesis: none`;
- verify actual rendered platform fonts after `document.fonts.ready` using
  DevTools/CDP rendered-font inspection, not CSS declarations alone;
- use the same leading in both locales: body 1.45, heading 1.35, caption 1.45,
  helper 1.52 and mono 1.40;
- keep title/section heading leading near 1.3–1.4 and preserve the existing
  control, navigation, history-row, inspector, sidebar and toolbar geometry;
- do not add global positive letter-spacing for Chinese; use normal spacing for
  body, controls, tables, technical values and buttons;
- keep title-to-subtitle grouping at the existing compact rhythm: 6px in both
  locales, without expanding global section spacing;
- do not add locale-specific font, size, weight, leading, letter-spacing,
  padding or spacing overrides; translated copy may still wrap naturally;
- use the Narrative family for locale preferences such as the sidebar locale
  switch; reserve Mono for technical/evidence content.

The formal font source versions, SHA-256 values, output sizes and conversion
command are recorded in `ui/src/assets/fonts/LICENSES.md`. The previous
candidate lab is removed from the production and development surface; the
temporary visual Surface Lab is separate and only loads from the DEV
`?surfacelab=1` query.

## 14.2 Inline alignment QA

Verify the alignment model by category:

- fixed-height controls and compact primitives: visible text, dot and swatch
  are optically centered;
- Causal Path, key/value lists and Accounting Events: key/timestamp and primary
  content share the first baseline;
- causal/timeline markers are anchored to the primary first line: Causal Path
  renders `marker | key | primary value` with secondary path/IP content below
  the value column, and Accounting Events renders `marker | timestamp | primary
  event content` with later evidence details in a separate secondary row. The
  key/timestamp never defines marker position; hollow markers mask the
  connector and the connector never renders over the dot;
- English and Simplified Chinese use the same leading and geometry.
- Verify this from screenshots/rendered glyph pixels or equivalent rendered text
  quads, not only from outer DOM bounding boxes: marker center must track the
  primary first-line content while secondary and wrapped content remain below.

Do not accept a fix that relies on a locale-, font-, string- or page-specific
top padding, pixel offset or transform. Marker placement must come from the
explicit primary-first-line layout; there is no shared optical-shift token.
`text-box: trim-both text` may be tested as progressive enhancement on
single-line primary wrappers only, but the fallback must remain correct when
unsupported.

---

# 15. Interaction audit

Verify:

- hover < selected;
- focus-visible is distinct;
- selected row clearly drives Inspector;
- closing Inspector does not lose History context;
- filters are obvious;
- Refresh behavior advances the frozen History snapshot;
- pagination does not imply nonexistent totals;
- opening a Select keeps one listbox tab stop, Arrow/Home/End update the active option, Enter/Space commits, and Tab closes it;
- opening a DatePicker keeps one day gridcell tab stop, Arrow keys move by day/week (including across months), Home/End move within the row, and Tab closes it;
- switching EN / 中文 updates visible UI copy immediately and survives reload;
- date/time display follows the active locale while raw technical values remain unchanged.
- the formal production build renders Manrope for Latin, Sarasa Gothic UI SC
  for Han/CJK and JetBrains Mono for evidence; the obsolete candidate-lab
  entry, manifest and ignored asset directory are absent;
- the DEV-only Surface Lab is available only with `?surfacelab=1`, starts
  collapsed, provides grouped low-chroma neutral Inspector candidates during
  visual Freeze, and is absent from the production bundle;
- the 1280×800 and 1600×1000 matrix is checked for Overview, History +
  Inspector and Coverage in EN/ZH and Light/Dark.

---

# 16. Product-semantics audit

Reject any UI that invents:

- security scoring;
- threat/malware claims;
- cryptographic verification;
- generic Trust Score;
- unsupported total counts;
- unsupported filters/sorts;
- writable network controls.

The UI may be visually polished and still fail if product semantics are false.

---

# 17. Build/runtime validation

Run the repository-defined:

- frontend tests;
- type checks;
- build;
- locale dictionary parity and static translation-key audit;
- Tauri dev/runtime smoke as appropriate.

The actual rendered Tauri UI is required.

A build passing without visually inspecting the application is not enough for this work package.

---

# 18. Documentation synchronization

At the end of the implementation:

- record any intentional deviations from Design System Draft;
- update the Design System when the implementation proved a better rule;
- do not leave “doc says one thing, code permanently does another.”

The eventual frozen v1 should mirror the approved shipped UI.
