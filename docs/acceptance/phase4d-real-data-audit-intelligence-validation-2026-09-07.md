# Phase 4D — Real-Data Audit Intelligence Validation & Product Calibration

Date: 2026-09-07
Baseline: a15a8c5542586cf2106da67e050e320dfe7d3c32
Branch: validation/phase4d-real-data-audit-intelligence
Mode: isolated E: copy, read-only Query API, query-only Tauri spot checks

## Scope and safety

Phase 4D was validation and calibration only. No accounting writer, detector
implementation, threshold, catalog, index, migration design, UI contract, or
runtime code was changed.

Before copying, the canonical source was quiescent:

- no ProxyLens Supervisor, Runtime, Collector, Query API, or desktop writer
  process was present;
- the exact production Task Scheduler owner was absent;
- the source WAL was zero bytes;
- source bytes and UTC mtime were unchanged across the quiescence interval;
- E: free space exceeded the required copy budget.

The source was copied through the filesystem only. The immutable schema-008
source-copy hash matched the original copy manifest, and the source remained
unchanged after copying. The original schema-008 work-copy was preserved as a
before/reference copy.

Phase 4D0 then created a third disposable analysis copy from the immutable
source-copy. The official current-main migration runner applied only migration
009 to that disposable copy. Migration before/after checks showed:

- schema version changed from 8 to 9;
- only the official single-live-generation index and migration record were
  added;
- all recorded business invariants were equivalent at the aggregate level:
  journal count/maximum sequence, active generation boundary/totals, v2
  connection state count, v2 accounted traffic counts and sums, relay status
  counts, hourly dimension counts/sums, and dimension connection count;
- PRAGMA quick_check returned ok.

The current-main Query API then opened only the schema-009 analysis copy using
mode=ro and query_only=ON. Health, metadata, coverage, summary, findings,
temporal findings, and connections requests completed without changing the
analysis-copy hash, mtime, or WAL.

No real Controller, FLClash, Mihomo, production writer, Task Scheduler
mutation, WinCred mutation, TUN, system proxy, DNS, route, node, or rule
operation was performed.

## Real-history scale and authority

- source database size: 5,410,275,328 bytes;
- event journal rows: approximately 1.60M (1,602,541);
- active accounting authority: incremental Accounting v2;
- active published boundary: equal to the journal maximum in the analysis copy;
- Query API metadata: READY, schema 9, accounting freshness fresh.

## Honest windows

The historical source contains substantial monitoring gaps. The longest
continuous gap-free interval was only approximately 20.95 minutes, so no valid
one-hour or 24-hour temporal comparison pair existed. Readiness was not
weakened to obtain a result.

| Window | Range | Coverage | Use |
| --- | --- | ---: | --- |
| W0 | 2026-09-05 11:56:33Z → 12:17:30Z | 1.0000 | longest complete static window |
| W2 | 24-hour known-history window | 0.1260 | scale/performance only; not an absence claim |
| W-known | approximately 41.5-hour known-history span | 0.0729 | static population/sample only; not an absence claim |

The W2 and W-known windows each contained 27 merged monitoring gaps. Two
temporal candidates whose boundaries were inside known history returned
baseline_has_monitoring_gaps with empty findings. Candidates beginning outside
known history correctly returned baseline_outside_known_scope. There was
therefore no ready T1/T2 temporal pair.

## Query performance

Each endpoint was requested once cold and twice warm. No request timed out.

| Endpoint | Range | First ms | Warm median ms | Result count |
| --- | --- | ---: | ---: | ---: |
| /intelligence/findings | W0 | 376.9 | 400.7 | 1 |
| /intelligence/findings | W2 | 378.1 | 370.8 | 1 |
| /intelligence/findings | W-known | 383.6 | 364.6 | 1 |
| /intelligence/temporal-findings | in-scope 1-hour candidate | 14.4 | 14.6 | 0, unavailable |
| /intelligence/temporal-findings | in-scope 3-hour candidate | 14.8 | 14.8 | 0, unavailable |
| /analytics/summary | W2 | 202.3 | 192.5 | 200 response |
| /connections?route=PROXY&limit=50 | W2 | 9.9 | 8.4 | 50 |

The common Review and History paths were interactive on this 5+ GB analysis
copy. No performance blocker or evidence-supported optimization task was
found. No index or migration was added for measurement.

## Detector correctness audit

The static endpoint returned one non-empty detector family in all three static
windows. There were no sampled instances for the other four static families.

| Detector | Sampled | Semantic mismatch | Evidence result |
| --- | ---: | ---: | --- |
| match_fallback_proxy | 0 | 0 | no real sample |
| broad_udp_proxy | 0 | 0 | no real sample |
| ip_only_proxy_target | 1 | 0 | verified |
| large_proxy_connection | 0 | 0 | no real sample |
| cataloged_background_process_proxy | 0 | 0 | no real sample |
| process_newly_observed_on_proxy | 0 | 0 | no ready temporal pair |
| process_proxy_growth | 0 | 0 | no ready temporal pair |
| host_gained_proxy_after_direct_baseline | 0 | 0 | no ready temporal pair |

The IP-only finding was independently checked against the active v2 rows in
the analysis copy. Its API evidence was PROXY/TCP, approximately 23.6 MB, and
812 physical connection identities, all exact. The lower-level cross-check
found 7,006 matching accounted evidence rows representing the same 812
physical identities; every matching row was PROXY, had no host or sniff-host
evidence, and had a destination IP. The API connection count matched the
distinct identity count.

No static finding claimed evidence outside its detector condition. Temporal
semantics were not inferred from incomplete windows; the API correctly
returned an unavailable status instead of zero-change findings.

## Product usefulness and calibration

### MATCH, broad UDP, large connection, and catalog

No real findings were returned for these families in the available history
windows. This is insufficient evidence to call them noisy or useful, and does
not justify threshold, ranking, or catalog changes. Their synthetic contracts
remain the current evidence for those families.

### IP-only

The single observed IP-only candidate was not a tiny routine result:

- 23,593,959 accounted bytes;
- 812 physical connection identities;
- 0 findings below 1 MiB or 10 MiB;
- 100% of the sampled family bytes were represented by the top finding.

This sample supports keeping the detector thresholdless for now. It does not
support adding a named byte floor.

### Process growth and prior-period new process

No ready comparison pair existed. Tiny-baseline amplification, lifetime
first-ever usefulness, and growth-ratio noise therefore remain uncalibrated
against real data. No detector behavior was changed.

### Host transition and catalog breadth

No ready temporal pair existed, so this history cannot establish whether
host-only route transitions miss material sniff-host or destination-IP
transitions. Phase 4B2B remains Deferred.

No catalog match appeared in the real static window. This does not establish
that the catalog is too narrow; no catalog expansion was made.

## Real-data Tauri spot acceptance

Both required runs used the schema-009 analysis copy, Visual QA query-only
gates, and exact owner/runtime/controller skip markers.

| Spot | Actual viewport | Review | History drill | Overflow | DB unchanged |
| --- | --- | --- | --- | --- | --- |
| EN Light | 1600×1000 | PASS | PASS, 50 rows | none | PASS |
| 中文 Dark | 1280×800 | PASS | PASS, 50 rows | none | PASS |

Review showed the real IP-only section and all Phase 4A sections. The
finding-to-History drill preserved a non-empty filter and PROXY context. The
sampled real History page included process/path/host fields up to approximately
18/69/35 characters respectively, with no horizontal overflow. The temporal
section remained locally unavailable because the selected complete window did
not contain a valid full-hour comparison pair; Phase 4A sections remained
available.

## Findings and decision

### P0/P1 findings

None. No data corruption, false detector claim, wrong authority, unsafe
side-effect, common-path timeout, or nondeterministic acceptance was observed.

### Non-blocking observations

- The source history has only approximately 7.29% monitoring coverage across
  the known span and no complete one-hour comparison pair. This is a real-data
  availability limitation, not a readiness-contract defect.
- Five static detector families had no real sample, so their usefulness is
  not calibrated by this dataset.
- No threshold or catalog change is justified by the available evidence.

### Explicit next task

Keep Phase 4B2B and Phase 4C2 Deferred. The next work direction is the
separately deferred real installed FLClash/Mihomo lifecycle and real-environment
acceptance, subject to its own safety gate. No Phase 4B2B/4C2 implementation,
threshold calibration, index, migration, or accounting change is warranted by
this Phase 4D dataset.

## Privacy and final state

Raw API responses, real domains, IPs, process names/paths, rule payloads,
node names, and screenshots remain only under the private E: validation
directory. None are committed here.

Production collection remained stopped and production autostart/task state was
not modified.
