# ProxyLens UI Design System

**Status:** Draft v1 — implementation-facing, not yet frozen  
**Current lifecycle:** initial C implementation + engineering/semantic Closure complete; real multi-state visual acceptance still pending.  
**Canonical visual reference:** the rules in this directory, not external screenshots.  
**Product semantics:** current repository code and frozen UI/API/semantic documents remain authoritative.

---

## Purpose

This directory defines how the official ProxyLens UI should look, feel and behave.

It is intentionally more detailed than a mood board but lighter than a fully mature standalone component package.

The system should evolve with the shipped app:

```text
Design System Draft
→ Real Tauri implementation
→ Visual QA
→ Token / Pattern corrections
→ Shipped UI
→ Design System synchronized back to reality
```

The initial real implementation now exists. The remaining purpose of this Draft is to guide visual acceptance, maintenance and future UI work until the validated shipped UI is stable enough to Freeze v1.

Do not preserve a written rule when the real product proves it is visually or ergonomically wrong; update the rule and implementation together.

---

## Read order

For UI implementation or maintenance:

1. current `AGENTS.md`
2. `docs/STATUS.md`
3. frozen UI/API/semantic contracts
4. `docs/PROXYLENS_UI_PRODUCT_INTERACTION_FRAMEWORK_v1.md`
5. `docs/design/PROXYLENS_UI_DESIGN_SYSTEM_v1.md`
6. `docs/design/PROXYLENS_UI_TOKEN_REFERENCE_v1.md`
7. `docs/design/PROXYLENS_UI_COMPONENTS_AND_PATTERNS_v1.md`
8. `docs/design/PROXYLENS_UI_IMPLEMENTATION_AND_QA_v1.md`
9. `docs/design/AGENT-BRIEF.md`
10. current implementation under `ui/src/`

The former C-group/Qwen implementation prompt was a one-time experiment input and is **not** a repository source of truth. Do not try to recover or follow an old harness-specific prompt when continuing development.

---

## Source-of-truth hierarchy

When sources conflict:

```text
current verified code / runtime facts
+
frozen API / IA / semantic contracts
>
product interaction framework
>
Draft Design System
>
generic UI conventions / external references
```

If a verified implementation correction changes a Draft Design System rule, synchronize the document and code together rather than leaving permanent drift.

---

## Reference hierarchy

### Linear

Reference only for:

- calm information hierarchy;
- high density without noise;
- content taking precedence over navigation/chrome;
- compact controls;
- restrained separators;
- clear selection;
- integrated list/detail workspaces.

Do not copy Linear's exact palette, layout, icons or component geometry.

### Supabase

Reference for:

- semantic token architecture;
- themeable Light/Dark surfaces;
- data/table discipline;
- reusable UI patterns;
- accessible component composition;
- separating sidebar tokens from application tokens without making the sidebar a competing color slab.

Do not make ProxyLens look like Supabase Studio.

### Claude Console

Secondary mood reference only:

- low saturation;
- soft contrast;
- calm tool-like character.

Do not use Anthropic's recognizable cream/coral/olive brand palette.

### Stitch

The former “Analytical Ledger” mockups were direction-finding artifacts only.

They are **not** implementation sources of truth and should not be copied into the repo.

Useful qualities have already been extracted into these documents.

---

## Design-system maturity target

Current target remains intentionally moderate:

```text
Foundations
+ semantic tokens
+ core components
+ ProxyLens-specific patterns
+ interaction/state rules
+ implementation/QA guidance
```

Not yet required:

- one prompt file per component;
- standalone component package;
- source map for every component;
- exhaustive prop documentation;
- dozens of mature component variants.

Add those only after the real UI stabilizes and repeated reuse justifies them.

---

## Current acceptance boundary

Before freezing Design System v1, complete real Tauri visual review across:

- healthy / gaps / stale / empty / scaled fixtures;
- 1280×800 and 1600×1000 minimum review sizes;
- Light and Dark themes;
- History + Inspector, Overview and Coverage;
- typography/fallback behavior and semantic color footprint.

Use `docs/STATUS.md` for the current validation checklist and `docs/DEVLOG.md` for completed milestone history.
