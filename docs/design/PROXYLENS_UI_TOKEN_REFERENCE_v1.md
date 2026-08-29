# ProxyLens — UI Token Reference v1

**Status:** Draft implementation seed  
**Important:** Token names/roles are more authoritative than first-pass hex values.

Use semantic variables in components.

Do not hard-code visual hex values throughout page CSS.

---

# 1. Token layers

Recommended mental model:

```text
Primitive color values
        ↓
Semantic theme tokens
        ↓
Component states
        ↓
Product patterns
```

Most application code should consume semantic tokens.

---

# 2. Semantic surface tokens

```css
--pl-canvas;
--pl-sidebar;
--pl-workspace;

--pl-surface-subtle;
--pl-surface-raised;
--pl-surface-inset;
--pl-surface-selected;
--pl-surface-hover;

--pl-border;
--pl-border-muted;
--pl-border-strong;

--pl-text-primary;
--pl-text-secondary;
--pl-text-muted;
--pl-text-disabled;

--pl-overlay-shadow;
```

`--pl-overlay-shadow` is reserved for temporary floating UI such as menus,
popovers and calendars. Core workspace surfaces continue to use tonal contrast
and borders rather than elevation.

---

# 3. Interaction tokens

```css
--pl-accent;
--pl-accent-hover;
--pl-accent-soft;
--pl-focus-ring;
```

Selection is not a route/status color.

---

# 4. Semantic route/evidence tokens

```css
--pl-route-proxy;
--pl-route-proxy-soft;

--pl-route-direct;
--pl-route-direct-soft;

--pl-route-reject;
--pl-route-reject-soft;

--pl-status-fresh;
--pl-status-fresh-soft;

--pl-status-stale;
--pl-status-stale-soft;

--pl-status-gap;
--pl-status-gap-soft;

--pl-status-offline;
--pl-status-offline-soft;

--pl-evidence-estimated;
--pl-evidence-estimated-soft;

--pl-evidence-ambiguous;
--pl-evidence-ambiguous-soft;

--pl-evidence-missing;
--pl-evidence-missing-soft;
```

Exact/default evidence may often remain neutral rather than requiring a bright “success” color.

---

# 5. Light theme seed

Initial working values:

```css
:root {
  color-scheme: light;

  --pl-canvas: #f7f7f5;
  --pl-sidebar: #f2f2f0;
  --pl-workspace: #fafaf8;

  --pl-surface-subtle: #f4f4f1;
  --pl-surface-raised: #ffffff;
  --pl-surface-inset: #eeeeeb;
  --pl-surface-selected: #ecefec;
  --pl-surface-hover: #f0f1ee;

  --pl-border: #dedfdb;
  --pl-border-muted: #e7e8e4;
  --pl-border-strong: #cfd1cc;

  --pl-text-primary: #1d201e;
  --pl-text-secondary: #555a56;
  --pl-text-muted: #7e847f;
  --pl-text-disabled: #a8ada8;

  --pl-accent: #607b82;
  --pl-accent-hover: #536d74;
  --pl-accent-soft: #e5ecec;
  --pl-focus-ring: rgba(96, 123, 130, 0.32);

  --pl-route-proxy: #637d8c;
  --pl-route-proxy-soft: #e8eef1;

  --pl-route-direct: #667d69;
  --pl-route-direct-soft: #e8eee8;

  --pl-route-reject: #965f57;
  --pl-route-reject-soft: #f3e9e6;

  --pl-status-fresh: #5e765f;
  --pl-status-fresh-soft: #e8eee8;

  --pl-status-stale: #8d733f;
  --pl-status-stale-soft: #f2eee2;

  --pl-status-gap: #946744;
  --pl-status-gap-soft: #f3e9df;

  --pl-status-offline: #9a5b55;
  --pl-status-offline-soft: #f3e6e4;

  --pl-evidence-estimated: #88724a;
  --pl-evidence-estimated-soft: #f0ece2;

  --pl-evidence-ambiguous: #6d7482;
  --pl-evidence-ambiguous-soft: #e9eaee;

  --pl-evidence-missing: #7b7470;
  --pl-evidence-missing-soft: #ece9e7;
}
```

This palette is intentionally less warm/brand-specific than the former Stitch/Claude-like direction.

It should read as neutral professional software rather than as an Anthropic-derived palette.

---

# 6. Dark theme seed

Use one coherent neutral graphite family.

```css
[data-theme="dark"],
.dark {
  color-scheme: dark;

  --pl-canvas: #0f1110;
  --pl-sidebar: #111312;
  --pl-workspace: #151715;

  --pl-surface-subtle: #191b19;
  --pl-surface-raised: #1e201e;
  --pl-surface-inset: #121412;
  --pl-surface-selected: #222622;
  --pl-surface-hover: #1c201d;

  --pl-border: #2a2e2a;
  --pl-border-muted: #222622;
  --pl-border-strong: #363b36;

  --pl-text-primary: #e9ece8;
  --pl-text-secondary: #b4b9b4;
  --pl-text-muted: #858b85;
  --pl-text-disabled: #5f655f;

  --pl-accent: #86a0a6;
  --pl-accent-hover: #99b1b6;
  --pl-accent-soft: #202b2d;
  --pl-focus-ring: rgba(134, 160, 166, 0.38);

  --pl-route-proxy: #8fa8b5;
  --pl-route-proxy-soft: #1d282d;

  --pl-route-direct: #91a993;
  --pl-route-direct-soft: #202a21;

  --pl-route-reject: #c0877e;
  --pl-route-reject-soft: #302321;

  --pl-status-fresh: #8ea790;
  --pl-status-fresh-soft: #202a21;

  --pl-status-stale: #baa06b;
  --pl-status-stale-soft: #2b281e;

  --pl-status-gap: #c0916a;
  --pl-status-gap-soft: #2f251f;

  --pl-status-offline: #c5827b;
  --pl-status-offline-soft: #302220;

  --pl-evidence-estimated: #ae986d;
  --pl-evidence-estimated-soft: #29261e;

  --pl-evidence-ambiguous: #969dac;
  --pl-evidence-ambiguous-soft: #23262d;

  --pl-evidence-missing: #9b9691;
  --pl-evidence-missing-soft: #272523;
}
```

Critical rule:

```text
sidebar + workspace + inspector
must read as one family,
not separate brown/blue/gray blocks.
```

---

# 7. Typography tokens

```css
/* Bundled canonical families; system fonts are last-resort fallbacks. */
--pl-font-narrative:
  "Public Sans",
  "IBM Plex Sans SC",
  Inter,
  "Segoe UI",
  "Microsoft YaHei",
  system-ui,
  sans-serif;

--pl-font-sans: var(--pl-font-narrative);

--pl-font-mono:
  "JetBrains Mono",
  "Cascadia Mono",
  Consolas,
  ui-monospace,
  monospace;
```

Locale-specific typography roles are concentrated on the root element:

```css
html[lang="zh-CN"] {
  --pl-font-narrative: "IBM Plex Sans SC", "Public Sans", ...;
  --pl-leading-body: 1.52;
  --pl-leading-heading: 1.36;
  --pl-leading-caption: 1.5;
  --pl-leading-helper: 1.54;
  --pl-letter-spacing-heading: normal;
  --pl-letter-spacing-eyebrow: normal;
  --pl-title-subtitle-gap: 8px;
}
```

Typography roles:

```text
--pl-font-weight-regular   400
--pl-font-weight-control   500
--pl-font-weight-heading   500
--pl-font-weight-emphasis  500
--pl-leading-body          1.45 (EN) / 1.52 (ZH)
--pl-leading-heading       1.35 (EN) / 1.36 (ZH)
--pl-leading-caption       1.45 (EN) / 1.50 (ZH)
--pl-leading-helper        1.52 (EN) / 1.54 (ZH)
--pl-leading-mono          1.40
```

Do not apply positive letter-spacing globally to Chinese. Body, controls,
tables, technical values, IP/domain/protocol tokens and buttons keep normal
letter-spacing. The existing English eyebrow treatment is removed in the
Chinese locale through the root role token.

Suggested size seed:

```css
--pl-text-10: 10px;
--pl-text-11: 11px;
--pl-text-12: 12px;
--pl-text-13: 13px;
--pl-text-14: 14px;
--pl-text-16: 16px;
--pl-text-20: 20px;
--pl-text-24: 24px;
```

Suggested roles:

```text
micro / eyebrow          10–11px
table header             11px
dense row                12–13px
body / controls          13px
section title            14px
page title               16–20px
major analytical value   20–24px
```

Avoid huge 32–48px dashboard numbers unless a specific composition proves it useful.

---

# 8. Spacing tokens

Base unit 4px.

Recommended:

```css
--pl-space-1: 4px;
--pl-space-2: 8px;
--pl-space-3: 12px;
--pl-space-4: 16px;
--pl-space-5: 20px;
--pl-space-6: 24px;
--pl-space-8: 32px;
--pl-space-10: 40px;
```

Use intermediate 6/10/14 values only where compact controls genuinely need them.

---

# 9. Radius tokens

```css
--pl-radius-xs: 2px;
--pl-radius-sm: 4px;
--pl-radius-md: 6px;
--pl-radius-lg: 8px;
```

Pills are allowed for truly compact categorical/status chips, not as the default shape for every control.

---

# 10. Motion tokens

```css
--pl-duration-fast: 120ms;
--pl-duration-ui: 180ms;
--pl-duration-panel: 210ms;
--pl-ease-out: cubic-bezier(0.2, 0.7, 0.2, 1);
```

No decorative looping motion.

---

# 11. Density tokens

Working implementation constants:

```css
--pl-control-h-compact: 28px;
--pl-control-h: 32px;
--pl-history-row-h: 42px;

--pl-sidebar-w: 208px;
--pl-inspector-w: 410px;
```

These geometry values are intentionally **tunable**.

Do not freeze them until real Tauri visual QA.

---

# 12. Token usage rules

Correct:

```css
color: var(--pl-text-secondary);
background: var(--pl-surface-selected);
border-color: var(--pl-border);
```

Avoid:

```css
color: #7e847f;
background: #ecefec;
```

inside page/component rules.

A component may use a semantic route/status token only when it actually expresses that semantic role.
