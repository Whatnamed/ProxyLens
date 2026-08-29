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

---

# 14. Typography audit

Check:

- Sans remains readable and calm;
- Mono is used for evidence, not decoration;
- technical columns align;
- uppercased tracked labels are not overused;
- titles are not oversized;
- long rules/hosts do not destroy layout.

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
