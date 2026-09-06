# Phase 4C1 Background/Security Process Intelligence Acceptance

- **Branch:** `feat/phase4c1-background-process-intelligence`
- **Baseline:** `main` / `origin/main` at `00a6ffb7d8f1ffa042652b33c5b2907a617aa481`
- **Scope:** embedded provenance-backed background/security process catalog and current-window Review findings
- **Status:** PASS; Phase 4B2B and Phase 4C2 remain Deferred

## Catalog and provenance

Catalog version: `background-processes-v1`, reviewed `2026-09-06`.

The exact three production entries are:

| Entry | Process | Path contract |
| --- | --- | --- |
| `microsoft-defender-antivirus-service` | `MsMpEng.exe` | under `C:\ProgramData\Microsoft\Windows Defender\Platform` |
| `microsoft-defender-core-service` | `MpDefenderCoreService.exe` | under the Defender Platform directory, or exact `C:\Program Files\Windows Defender\MpDefenderCoreService.exe` |
| `microsoft-defender-network-inspection-service` | `NisSrv.exe` | under the Defender Platform directory, or exact `C:\Program Files\Windows Defender\NisSrv.exe` |

Provenance uses only these official Microsoft Learn sources:

- `microsoft-defender-processes`: [Microsoft Defender Antivirus in Windows overview](https://learn.microsoft.com/en-us/defender-endpoint/microsoft-defender-antivirus-windows)
- `microsoft-defender-connectivity-binaries`: [Microsoft Defender for Endpoint standard connectivity URLs - commercial](https://learn.microsoft.com/en-us/defender-endpoint/standard-device-connectivity-urls-commercial)

URLs are embedded metadata only; the binary does not fetch them. Matching is
case-insensitive normalized process name → O(1) catalog candidate → exact or
under-directory Windows path rule. Normalization trims whitespace/one quote pair,
converts `/` to `\\`, collapses repeated separators, preserves drive roots,
removes non-root trailing separators, and lowercases. No filesystem, symlink,
service manager, Authenticode, or signature inspection is used.

## Detector and API

`cataloged_background_process_proxy` is an additive, read-only detector for
positive accounted `PROXY` bytes in `[from,to)`. It aggregates by catalog entry,
normalized process name, and target precedence `host → sniff_host →
destination_ip → missing`. `subject.processPath` is display evidence and the
latest matching path may replace an earlier path without changing the stable ID.
The ID excludes path, bytes, counts, catalog provenance/review metadata, catalog
version, and live query end. The response adds `knowledgeCatalogVersion` and a
provenance `knowledge` object with `matchBasis=process_name_and_path`.

The existing four Phase 4A detector families and their IDs remain unchanged.
No endpoint, writer, migration, index, or temporal authority resolver was
added. Legacy and active-v2 equivalent fixtures produced equivalent catalog
findings.

## Negative and deterministic fixture evidence

`review-background-services` contains exactly these positives:

- `MsMpEng.exe` → `defender-antivirus.synthetic.example`;
- `MpDefenderCoreService.exe` → `defender-core.synthetic.example`;
- `NisSrv.exe` → `defender-network.synthetic.example`.

It also contains explicit negatives that were absent from catalog findings:

- `MsMpEng.exe` with `C:\Users\Synthetic\Downloads\MsMpEng.exe`;
- `MsMpEng.exe` with an empty `process_path`;
- a correct `NisSrv.exe` identity/path routed `DIRECT`.

The `NisSrv.exe` positive uses interval-derived evidence and renders as
estimated. The Go fixture regression generated the profile at anchors
`2026-09-06T13:05:00Z` and `2026-09-06T13:55:00Z`; both produced the same exact
ordered three-process/target set. Storage tests also cover exact and
under-directory matching, case/path normalization, wrong-path/pathless
exclusion, duplicate catalog validation, provenance validation, path-version
stable identity, direct exclusion, and legacy/v2 equivalence.

## Static timing

`TestAuditIntelligenceScaledTimings` used temporary migrated databases and the
same catalog-enabled `ListFindings` scan. The scale rows use `scale.exe`, so
catalog candidate rows/matches were deterministically `0/0`; this measures the
additional lookup path without fabricating catalog positives.

| Rows | Fixture insertion | Findings scan | Total |
| ---: | ---: | ---: | ---: |
| 10,000 | 375.863 ms | 68.048 ms | 443.911 ms |
| 100,000 | 3.666 s | 596.959 ms | 4.263 s |

The 1.5M E-drive proof remains an independent Phase 3S artifact.

## Query-only Tauri matrix

All runs used a copied ignored fixture DB, `PROXYLENS_E2E_MODE=1`,
`PROXYLENS_VISUAL_QA_QUERY_ONLY=1`, and no Controller/Secret authority env.
Every run recorded exact actual CSS viewport, `catalogRows=3`, all three
expected process/target pairs, all negatives absent, provenance boundary text,
estimated evidence, Phase 4A section presence, no horizontal overflow,
`owner=0 runtime=0 controller=0`, and unchanged source/copy SHA.

| Size | EN Light | EN Dark | 中文 Light | 中文 Dark |
| --- | --- | --- | --- | --- |
| 1280×800 | `phase4c1-1280-en-light` PASS | `phase4c1-1280-en-dark` PASS | `phase4c1-1280-zh-light` PASS | `phase4c1-1280-zh-dark` PASS |
| 1600×1000 | `phase4c1-1600-en-light` PASS | `phase4c1-1600-en-dark` PASS | `phase4c1-1600-zh-light` PASS | `phase4c1-1600-zh-dark` PASS |

The 1280×800 EN Light run also activated the `MsMpEng.exe` finding. History
showed `route=PROXY`, process `MsMpEng.exe`, the target context filter
`defender-antivirus.synthetic.example`, page 1, a fresh snapshot, and no
unrelated filters. No process-path History filter was added.

## Validation and safety

- `go test ./pkg/storage -run 'Test(BackgroundCatalog|AuditIntelligence)' -count=1 -v`: PASS;
- `go test ./pkg/api -run 'Test(IntelligenceFindingsAPIContractAndRuleFilter|APIServerAuthCORSAndEndpoints)$' -count=1 -v`: PASS;
- `go test ./cmd/proxylens-ui-fixture -count=1`: PASS;
- `go test ./pkg/storage ./pkg/api`: PASS (final focused package validation);
- `go vet ./pkg/storage ./pkg/api ./cmd/proxylens-ui-fixture`: PASS;
- `npm.cmd test`: PASS, 103 tests; `npm.cmd run build`: PASS;
- `node --test tools/ui-acceptance/test/*.test.mjs`: PASS, 6 tests; modified Node scripts passed `node --check`;
- `npm.cmd run sidecar:build`: PASS; `git diff --check`: PASS.

All runtime/Tauri checks were query-only synthetic acceptance. No real
`127.0.0.1:9090`/`7988`, FLClash/Mihomo, production DB, Windows process/service
enumeration, TUN, system proxy, DNS, route, node, rule, Task Scheduler, WinCred,
or production collection lifecycle was touched. Phase 4B2B canonical target
storage and Phase 4C2 copyable rule suggestions remain Deferred.
