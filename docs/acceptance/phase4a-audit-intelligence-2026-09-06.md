# Phase 4A Audit Intelligence Foundation Acceptance

- **Date**: 2026-09-06
- **Branch**: `feat/phase4a-audit-intelligence-foundation`
- **Scope**: deterministic Review foundation only
- **Status**: PASS for Phase 4A foundation plus final semantic closure; Phase 4B/4C and real-environment validation remain Deferred

## Acceptance matrix

| Area | Evidence | Result |
| --- | --- | --- |
| Reconciled authority | Service resolves active v2 generation, otherwise latest completed legacy run | PASS |
| MATCH detector | PROXY + normalized `MATCH` + positive accounted bytes; process/target aggregation | PASS |
| Broad UDP detector | PROXY + `network=udp` + normalized `NETWORK,udp`; direct/specific UDP excluded | PASS |
| IP-only detector | PROXY + destination IP with empty host/sniff host; zero-accounted rows excluded | PASS |
| Large connection detector | Physical `(session_id,epoch_id,connection_id)` aggregation; strict `>100 MiB` threshold returned | PASS |
| Time/evidence | Exact `[from,to)` membership and existing interval allocator; exact/estimated split tested | PASS |
| Identity | Stable IDs use detector-specific canonical identity and do not include mutable display metadata or live `to` | PASS |
| Query API | Required range, bounded `limitPerKind` 1–50, GET-only, Bearer/CORS, read-only errors | PASS |
| History investigation | Detector-specific exact filters only: MATCH rule, broad UDP rule/network, IP-only process/IP, large process/target | PASS |
| UI explanation | Rule/Network are shown as detector reasons only where the detector uses them; IP-only/large omit incidental metadata | PASS |
| UI boundary | React uses Query API only; no filesystem, SQLite, Controller, or lifecycle authority | PASS |
| Safety | Query-only visual runs use copied temporary fixture DB; `owner=0 runtime=0 controller=0` | PASS |

## Final semantic closure

- Detector comparison uses normalized Rule copies while `AuditFindingEvidence.Rule`
  and `RulePayload` preserve raw occurrence-time evidence, including exact
  `DomainSuffix` casing.
- History drill filters are detector-specific and do not use incidental
  Rule/Network metadata to narrow IP-only or large physical-connection review.
- Finding IDs use canonical detector identity. A large physical connection keeps
  the same ID when later metadata enriches an IP-only display subject into a
  host display subject and its bytes grow.

## Focused validation

- `go test ./pkg/storage -run '^TestAuditIntelligence' -count=1 -v`: PASS.
- `go test ./pkg/api -run 'Test(IntelligenceFindingsAPIContractAndRuleFilter|APIServerAuthCORSAndEndpoints)$' -count=1 -v`: PASS.
- `go test ./pkg/storage ./pkg/api`: PASS.
- `go vet ./pkg/storage ./pkg/api`: PASS.
- `npm.cmd test`: PASS, including locale parity and Review semantics.
- `npm.cmd run build`: PASS.
- `node --test tools/ui-acceptance/test/*.test.mjs`: PASS.
- `git diff --check`: PASS.

## Scale timing evidence

The timing fixture uses a temporary migrated SQLite database with a precomputed
completed legacy accounting run. It measures the single read/aggregate path;
it does not change or benchmark production accounting implementation.

| Rows | Fixture insertion | Findings read/aggregate | Total |
| ---: | ---: | ---: | ---: |
| 10,000 | ~302 ms | ~50 ms | ~351 ms |
| 100,000 | ~3.09 s | ~507 ms | ~3.60 s |

The 1.5M E-drive scale acceptance remains an independent Phase 3S artifact.

## Query-only Tauri Review matrix

Every passing run used `profile=review`, `view=review`, an isolated copied DB,
and the same recorded fixture anchor. CDP observed the actual CSS viewport and
the runner checked locale, theme, and page state after the preference reload.

| Evidence directory | Requested | Actual | Locale | Theme | Safety |
| --- | ---: | ---: | --- | --- | --- |
| `tmp/phase3-final-acceptance/phase4a-review-1280-en-light-v2-AvUWVu` | 1280×800 | 1280×800 | EN | Light | source/copy unchanged; owner/runtime/controller 0 |
| `tmp/phase3-final-acceptance/phase4a-review-1280-en-dark-zPlsWz` | 1280×800 | 1280×800 | EN | Dark | source/copy unchanged; owner/runtime/controller 0 |
| `tmp/phase3-final-acceptance/phase4a-review-1280-zh-light-cb4vrX` | 1280×800 | 1280×800 | 中文 | Light | source/copy unchanged; owner/runtime/controller 0 |
| `tmp/phase3-final-acceptance/phase4a-review-1280-zh-dark-v2-Zpon8N` | 1280×800 | 1280×800 | 中文 | Dark | source/copy unchanged; owner/runtime/controller 0 |
| `tmp/phase3-final-acceptance/phase4a-review-1600-en-light-v2-ShAW8b` | 1600×1000 | 1600×1000 | EN | Light | source/copy unchanged; owner/runtime/controller 0 |
| `tmp/phase3-final-acceptance/phase4a-review-1600-en-dark-v2-TkUVAp` | 1600×1000 | 1600×1000 | EN | Dark | source/copy unchanged; owner/runtime/controller 0 |
| `tmp/phase3-final-acceptance/phase4a-review-1600-zh-light-v2-sFBavu` | 1600×1000 | 1600×1000 | 中文 | Light | source/copy unchanged; owner/runtime/controller 0 |
| `tmp/phase3-final-acceptance/phase4a-review-1600-zh-dark-v2-GCLacP` | 1600×1000 | 1600×1000 | 中文 | Dark | source/copy unchanged; owner/runtime/controller 0 |

The runner also checked the Review page state after reload, not merely the
requested profile. No test used real `127.0.0.1:9090`/`7988`, FLClash,
Mihomo, production DB, TUN, system proxy, DNS, routes, nodes, or rules.

## Deferred

- Phase 4B/4C historical comparisons, background-service candidates, rule
  suggestions, scoring, and automatic actions;
- richer Rule Payload / Final Proxy / Port search dimensions;
- real FLClash/Mihomo and real-data acceptance.
