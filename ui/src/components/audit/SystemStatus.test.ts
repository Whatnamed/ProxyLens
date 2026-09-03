import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { heartbeatStaleThresholdMs, deriveSystemStatus } from './SystemStatus.js';
import { CollectorSessionRecord, MetaResponse } from '../../api/types';

function makeSession(overrides: Partial<CollectorSessionRecord> = {}): CollectorSessionRecord {
  return {
    sessionId: 'sess-1',
    startedAt: '2026-08-29T00:00:00Z',
    lastFrameSequence: 10,
    status: 'running',
    createdAt: '2026-08-29T00:00:00Z',
    updatedAt: '2026-08-29T00:00:00Z',
    ...overrides,
  };
}

function makeMeta(session?: CollectorSessionRecord): MetaResponse {
  return {
    apiVersion: 'v1',
    appVersion: '0.7.0',
    dbState: 'READY',
    schemaVersion: 1,
    maxBinarySchemaVersion: 1,
    latestCollectorSession: session,
  };
}

const NOW = new Date('2026-08-29T12:00:00Z').getTime();
const iso = (msBeforeNow: number) => new Date(NOW - msBeforeNow).toISOString();

describe('heartbeatStaleThresholdMs matches the backend Coverage grace rule', () => {
  it('uses 3x heartbeat interval', () => {
    assert.equal(heartbeatStaleThresholdMs(10_000), 30_000);
    assert.equal(heartbeatStaleThresholdMs(60_000), 180_000);
  });

  it('enforces the 15s minimum grace', () => {
    assert.equal(heartbeatStaleThresholdMs(5_000), 15_000);
    assert.equal(heartbeatStaleThresholdMs(4_000), 15_000);
    assert.equal(heartbeatStaleThresholdMs(1_000), 15_000);
  });

  it('falls back to the default 5s interval when missing or invalid', () => {
    assert.equal(heartbeatStaleThresholdMs(undefined), 15_000);
    assert.equal(heartbeatStaleThresholdMs(0), 15_000);
    assert.equal(heartbeatStaleThresholdMs(-1), 15_000);
  });
});

describe('deriveSystemStatus collector staleness', () => {
  it('reports healthy when the heartbeat is inside the grace window', () => {
    const meta = makeMeta(makeSession({ lastHeartbeatAt: iso(10_000), heartbeatIntervalMs: 5_000 }));
    assert.equal(deriveSystemStatus(meta, NOW).collector.kind, 'fresh');
  });

  // Regression: the sidebar used a hardcoded 90s threshold while backend
  // Coverage goes stale after max(3x interval, 15s). A 20s-old heartbeat with
  // a 5s interval must already be stale, matching Coverage semantics.
  it('reports stale once the heartbeat exceeds max(3x interval, 15s)', () => {
    const meta = makeMeta(makeSession({ lastHeartbeatAt: iso(20_000), heartbeatIntervalMs: 5_000 }));
    assert.equal(deriveSystemStatus(meta, NOW).collector.kind, 'stale');
  });

  it('honors a larger heartbeat interval from meta', () => {
    const fresh = makeMeta(makeSession({ lastHeartbeatAt: iso(20_000), heartbeatIntervalMs: 10_000 }));
    assert.equal(deriveSystemStatus(fresh, NOW).collector.kind, 'fresh');
    const stale = makeMeta(makeSession({ lastHeartbeatAt: iso(35_000), heartbeatIntervalMs: 10_000 }));
    assert.equal(deriveSystemStatus(stale, NOW).collector.kind, 'stale');
  });

  it('treats a running session without heartbeat as stale', () => {
    const meta = makeMeta(makeSession({ lastHeartbeatAt: undefined }));
    assert.equal(deriveSystemStatus(meta, NOW).collector.kind, 'stale');
  });

  it('reports offline for ended sessions and missing sessions', () => {
    const ended = makeMeta(makeSession({ status: 'interrupted', endedAt: iso(60_000) }));
    assert.equal(deriveSystemStatus(ended, NOW).collector.kind, 'offline');
    assert.equal(deriveSystemStatus(makeMeta(undefined), NOW).collector.kind, 'offline');
  });

  it('reports unknown statuses when meta is unavailable', () => {
    const status = deriveSystemStatus(undefined, NOW);
    assert.equal(status.collector.kind, 'offline');
    assert.equal(status.accounting.kind, 'offline');
    assert.equal(status.db.kind, 'offline');
  });
});

describe('deriveSystemStatus accounting & database state', () => {
  it('reports fresh accounting when freshness.isFresh is true', () => {
    const meta: MetaResponse = {
      ...makeMeta(),
      latestAccountingRun: {
        runId: 'run-1',
        algorithmVersion: 'v1',
        startedAt: '2026-08-29T00:00:00Z',
        status: 'completed',
        sourceJournalEventCount: 100,
        sourceBoundaryJson: '{}',
      },
      freshness: {
        runId: 'run-1',
        sourceJournalSequenceMax: 100,
        currentJournalSequenceMax: 100,
        lagEvents: 0,
        isFresh: true,
      },
    };
    const status = deriveSystemStatus(meta, NOW);
    assert.equal(status.accounting.kind, 'fresh');
  });

  it('reports stale accounting when lagEvents > 0, without marking it offline/failed', () => {
    const meta: MetaResponse = {
      ...makeMeta(),
      latestAccountingRun: {
        runId: 'run-1',
        algorithmVersion: 'v1',
        startedAt: '2026-08-29T00:00:00Z',
        status: 'completed',
        sourceJournalEventCount: 100,
        sourceBoundaryJson: '{}',
      },
      freshness: {
        runId: 'run-1',
        sourceJournalSequenceMax: 100,
        currentJournalSequenceMax: 105,
        lagEvents: 5,
        isFresh: false,
      },
    };
    const status = deriveSystemStatus(meta, NOW);
    assert.equal(status.accounting.kind, 'stale');
  });

  it('reports database incompatible when schema exceeds binary max', () => {
    const meta: MetaResponse = {
      ...makeMeta(),
      dbState: 'INCOMPATIBLE',
      schemaVersion: 5,
      maxBinarySchemaVersion: 3,
    };
    const status = deriveSystemStatus(meta, NOW);
    assert.equal(status.db.kind, 'offline');
    assert.match(status.db.title, /Schema v5/);
  });
});
