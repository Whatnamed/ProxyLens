import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { buildSegments, windowAroundGap } from './coverage';
import {
  isEstimatedPrecision,
  describePrecision,
  deriveCollectorHealth,
  qualityIssues,
  evidenceFlags,
  finalEgress,
  topPolicyGroup,
  intermediateHops
} from './semantics';
import { toProductError } from './apiError';
import { ApiClientError } from '../api/client';
import { CoverageSummary, ConnectionRecord } from '../api/types';

/* ==========================================================================
 * Coverage timeline reconstruction
 * ========================================================================== */

function coverage(partial: Partial<CoverageSummary>): CoverageSummary {
  return {
    coveredDurationMs: 0,
    uncoveredDurationMs: 0,
    outsideKnownScopeMs: 0,
    futureDurationMs: 0,
    controllerGapDurationMs: 0,
    collectorOfflineDurationMs: 0,
    mergedGaps: [],
    ...partial
  };
}

describe('buildSegments: outside monitored history', () => {
  it('marks the whole window as outside when no collector session has ever existed', () => {
    const c = coverage({
      requestedStart: '2026-01-01T00:00:00Z',
      requestedEnd: '2026-01-02T00:00:00Z',
      knownScopeStart: undefined
    });
    const segs = buildSegments(c);
    assert.equal(segs.length, 1);
    // Must be "outside", never a coverage gap: no claim can be made about it.
    assert.equal(segs[0].type, 'outside');
    assert.equal(segs[0].ms, 86_400_000);
  });

  it('separates the pre-history prefix from the monitored remainder', () => {
    const c = coverage({
      requestedStart: '2026-01-01T00:00:00Z',
      requestedEnd: '2026-01-03T00:00:00Z',
      knownScopeStart: '2026-01-02T00:00:00Z',
      outsideKnownScopeMs: 86_400_000
    });
    const segs = buildSegments(c);
    assert.deepEqual(
      segs.map((s) => s.type),
      ['outside', 'covered']
    );
    assert.equal(segs[0].ms, 86_400_000);
    assert.equal(segs[1].ms, 86_400_000);
  });
});

describe('buildSegments: gaps', () => {
  it('renders covered runs on both sides of a controller gap', () => {
    const c = coverage({
      requestedStart: '2026-01-01T00:00:00Z',
      requestedEnd: '2026-01-01T03:00:00Z',
      knownScopeStart: '2026-01-01T00:00:00Z',
      coveredDurationMs: 7_200_000,
      uncoveredDurationMs: 3_600_000,
      mergedGaps: [
        {
          source: 'controller_stream',
          startedAt: '2026-01-01T01:00:00Z',
          endedAt: '2026-01-01T02:00:00Z',
          durationMs: 3_600_000,
          reason: 'reconnect'
        }
      ]
    });
    const segs = buildSegments(c);
    assert.deepEqual(
      segs.map((s) => s.type),
      ['covered', 'controller_stream', 'covered']
    );
    assert.equal(segs[1].ms, 3_600_000);
    assert.equal(segs[1].gapIndex, 0);
  });

  it('distinguishes collector offline from controller gap', () => {
    const c = coverage({
      requestedStart: '2026-01-01T00:00:00Z',
      requestedEnd: '2026-01-01T02:00:00Z',
      knownScopeStart: '2026-01-01T00:00:00Z',
      mergedGaps: [
        {
          source: 'collector_session_boundary',
          startedAt: '2026-01-01T00:30:00Z',
          endedAt: '2026-01-01T01:00:00Z',
          durationMs: 1_800_000,
          reason: 'offline'
        }
      ]
    });
    const segs = buildSegments(c);
    assert.ok(segs.some((s) => s.type === 'collector_session_boundary'));
  });

  it('appends a future segment when the window extends past now', () => {
    const c = coverage({
      requestedStart: '2026-01-01T00:00:00Z',
      requestedEnd: '2026-01-01T04:00:00Z',
      knownScopeStart: '2026-01-01T00:00:00Z',
      futureDurationMs: 3_600_000
    });
    const segs = buildSegments(c);
    assert.equal(segs[segs.length - 1].type, 'future');
  });

  it('returns no segments when coverage is unavailable', () => {
    assert.deepEqual(buildSegments(undefined), []);
  });
});

describe('windowAroundGap', () => {
  it('pads the gap and clamps padding for very long gaps', () => {
    const short = windowAroundGap({
      source: 'controller_stream',
      startedAt: '2026-01-01T01:00:00Z',
      endedAt: '2026-01-01T01:05:00Z', // 5 min
      durationMs: 300_000,
      reason: 'r'
    });
    // Minimum padding is 15 minutes.
    assert.equal(new Date(short.from).toISOString(), '2026-01-01T00:45:00.000Z');
    assert.equal(new Date(short.to).toISOString(), '2026-01-01T01:20:00.000Z');

    const long = windowAroundGap({
      source: 'collector_session_boundary',
      startedAt: '2026-01-01T00:00:00Z',
      endedAt: '2026-01-02T00:00:00Z', // 24 h
      durationMs: 86_400_000,
      reason: 'r'
    });
    // Maximum padding is 2 hours, so the window is 28 h, not 72 h.
    assert.equal(new Date(long.from).toISOString(), '2025-12-31T22:00:00.000Z');
    assert.equal(new Date(long.to).toISOString(), '2026-01-02T02:00:00.000Z');
  });
});

/* ==========================================================================
 * Precision
 * ========================================================================== */

describe('precision semantics', () => {
  it('treats interval_derived and estimated as non-exact', () => {
    assert.equal(isEstimatedPrecision('interval_derived'), true);
    assert.equal(isEstimatedPrecision('estimated'), true);
    assert.equal(isEstimatedPrecision('exact'), false);
    assert.equal(isEstimatedPrecision('exact_snapshot'), false);
  });

  it('never labels an unknown precision as exact', () => {
    const p = describePrecision('something_new');
    assert.match(p.label, /Unrecognised/);
    assert.equal(p.tone, 'neutral');
  });
});

/* ==========================================================================
 * Collector health
 * ========================================================================== */

describe('deriveCollectorHealth', () => {
  const now = Date.now();

  it('reports unknown when there is no session', () => {
    assert.equal(deriveCollectorHealth(undefined, now).level, 'unknown');
  });

  it('treats interrupted and cleanly closed sessions as offline', () => {
    assert.equal(deriveCollectorHealth({ status: 'interrupted' }, now).level, 'offline');
    assert.equal(deriveCollectorHealth({ status: 'closed_clean' }, now).level, 'offline');
  });

  it('is healthy when the heartbeat is within its own interval', () => {
    const h = deriveCollectorHealth(
      {
        status: 'running',
        heartbeatIntervalMs: 5_000,
        lastHeartbeatAt: new Date(now - 6_000).toISOString()
      },
      now
    );
    assert.equal(h.level, 'healthy');
  });

  it('becomes stale, then offline, as the heartbeat ages', () => {
    const base = { status: 'running' as const, heartbeatIntervalMs: 5_000 };
    // 5 minutes silent: past the 3x grace floor but inside the stale window.
    assert.equal(
      deriveCollectorHealth({ ...base, lastHeartbeatAt: new Date(now - 300_000).toISOString() }, now)
        .level,
      'stale'
    );
    // 2 hours silent: offline.
    assert.equal(
      deriveCollectorHealth(
        { ...base, lastHeartbeatAt: new Date(now - 7_200_000).toISOString() },
        now
      ).level,
      'offline'
    );
  });

  it('does not mistake a slow heartbeat cadence for failure', () => {
    const h = deriveCollectorHealth(
      {
        status: 'running',
        heartbeatIntervalMs: 120_000, // 2-minute cadence
        lastHeartbeatAt: new Date(now - 150_000).toISOString()
      },
      now
    );
    assert.equal(h.level, 'healthy');
  });
});

/* ==========================================================================
 * Explainable unknowns
 * ========================================================================== */

describe('quality issues stay separated', () => {
  it('emits one named issue per flag rather than a single unknown bucket', () => {
    const issues = qualityIssues({
      missingProcess: false,
      missingProcessPath: true,
      missingHost: false,
      ipOnly: true,
      missingRule: true,
      missingChain: true
    });
    assert.equal(issues.length, 4);
    assert.deepEqual(
      issues.map((i) => i.key),
      ['missingProcessPath', 'ipOnly', 'missingRule', 'missingChain']
    );
  });

  it('does not report a missing path when the process itself is missing', () => {
    const issues = qualityIssues({
      missingProcess: true,
      missingProcessPath: true,
      missingHost: false,
      ipOnly: false,
      missingRule: false,
      missingChain: false
    });
    assert.ok(!issues.some((i) => i.key === 'missingProcessPath'));
  });
});

describe('evidenceFlags', () => {
  const base: ConnectionRecord = {
    sessionId: 's',
    epochId: 1,
    connectionId: 'c',
    firstObservedAt: '2026-01-01T00:00:00Z',
    lastObservedAt: '2026-01-01T00:01:00Z',
    observationActive: false,
    state: 'disappeared_from_snapshot',
    preexistingAtStart: false,
    possibleUnobservedTail: false,
    metadata: {},
    route: 'PROXY',
    baselineUploadCounter: 0,
    baselineDownloadCounter: 0,
    lastObservedUploadCounter: 0,
    lastObservedDownloadCounter: 0,
    monitoredUploadTotal: 0,
    monitoredDownloadTotal: 0
  };

  it('reports nothing for a fully attributed connection', () => {
    assert.equal(evidenceFlags(base).length, 0);
  });

  it('surfaces missing process and lifecycle conditions separately', () => {
    const conn: ConnectionRecord = {
      ...base,
      preexistingAtStart: true,
      possibleUnobservedTail: true,
      qualityFlags: {
        missingProcess: true,
        missingProcessPath: true,
        missingHost: false,
        ipOnly: false,
        missingRule: false,
        missingChain: false
      }
    };
    const keys = evidenceFlags(conn).map((f) => f.key);
    assert.ok(keys.includes('missingProcess'));
    assert.ok(keys.includes('tail'));
    assert.ok(keys.includes('preexisting'));
  });
});

describe('proxy chain hop order', () => {
  const conn = { chains: ['Node-HK-01', 'Relay-A', 'ProxyGroup'], route: 'PROXY' };
  it('reads chains[0] as the final physical egress', () => {
    assert.equal(finalEgress(conn), 'Node-HK-01');
  });
  it('reads chains[last] as the top policy group', () => {
    assert.equal(topPolicyGroup(conn), 'ProxyGroup');
  });
  it('treats entries between them as intermediate hops', () => {
    assert.deepEqual(intermediateHops(conn), ['Relay-A']);
  });
});

/* ==========================================================================
 * Product error mapping
 * ========================================================================== */

describe('API errors map to distinct product states', () => {
  it('does not present a missing accounting run as a system failure', () => {
    const e = toProductError(new ApiClientError(404, 'NO_COMPLETED_ACCOUNTING_RUN', 'none'));
    assert.equal(e.kind, 'no_accounting_run');
    assert.equal(e.retryable, true);
  });

  it('separates database unavailability from schema incompatibility', () => {
    assert.equal(toProductError(new ApiClientError(503, 'DB_UNAVAILABLE', 'x')).kind, 'db_unavailable');
    assert.equal(
      toProductError(new ApiClientError(500, 'DB_INCOMPATIBLE', 'x')).kind,
      'db_incompatible'
    );
  });

  it('marks an unreachable API as retryable and an auth failure as not', () => {
    assert.equal(toProductError(new ApiClientError(0, 'API_UNAVAILABLE', 'x')).retryable, true);
    assert.equal(toProductError(new ApiClientError(401, 'UNAUTHORIZED', 'x')).retryable, false);
  });
});
