---
name: proxylens-ui-design
description: Implementation brief for the ProxyLens visual system and audit UI.
status: draft-v1
---

# Agent Brief

ProxyLens is a Windows-local, read-only Mihomo traffic audit and explainability application.

The UI is not a generic admin dashboard.

Its core job is to preserve and explain:

```text
Process
→ Destination
→ Rule
→ Policy / Proxy Chain
→ Final Physical Egress
→ Traffic
```

and to communicate whether the evidence is complete, fresh, exact, estimated or missing.

## Before writing UI

Read the current repository truth first.

Do not infer product behavior from screenshots, old diagnostics UI, or generic dashboard conventions.

Then read this design-system directory.

## Visual character

Build a:

```text
quiet
precise
high-density
professional
technical workspace
```

Use:

- neutral surfaces;
- compact geometry;
- restrained semantic color;
- strong typography;
- selective monospace;
- subtle separators;
- clear selection;
- integrated list + Inspector relationships.

Avoid:

- cyberpunk;
- consumer VPN styling;
- giant KPI cards;
- card grids;
- neon;
- gradients/glow;
- world maps;
- excessive colored text/badges;
- glassmorphism in the core workspace;
- invented security semantics.

## Color rule

Most of the interface is neutral.

Semantic color communicates state.

Do not decorate ordinary data with semantic colors.

A normal History row should usually have no more than one dominant semantic accent, typically Route.

## Typography rule

Use:

```text
Public Sans
→ product/navigation/explanation layer

JetBrains Mono
→ machine/evidence layer
```

Mono is semantic, not decorative.

## Theme rule

Light is the current canonical visual baseline.

Dark is a direct adaptation of the same system.

Dark mode must not become more colorful, more “cyber,” or structurally different.

## Product truth

Do not introduce concepts such as:

- Trust Score;
- malware scoring;
- cryptographic executable signatures;
- threat detection;
- New Investigation;
- arbitrary Settings;
- fabricated totals/pages.

unless explicitly supported by current repository product/API contracts.

## Implementation philosophy

Prefer a small number of reusable primitives and product patterns.

Do not build an abstract enterprise component framework before the pages prove what is needed.

Implement real UI against the real Query API / synthetic fixtures.

Use the running Tauri window as the main visual review surface.
