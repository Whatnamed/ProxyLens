import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { buildSegments } from './CoveragePage.js';
import { CoverageSummary } from '../../api/types.js';
import { translate } from '../../i18n.js';

const t = (key: string, vars?: Record<string, string | number>) => translate('en', key, vars);

describe('Coverage timeline segment derivation', () => {
  const baseCoverage: CoverageSummary = {
    coveredDurationMs: 3600000,
    uncoveredDurationMs: 0,
    outsideKnownScopeMs: 0,
    futureDurationMs: 0,
    coverageRatio: 1.0,
    controllerGapDurationMs: 0,
    collectorOfflineDurationMs: 0,
    mergedGaps: [],
    effectiveScopeStart: '2026-08-28T10:00:00.000Z',
    effectiveScopeEnd: '2026-08-28T12:00:00.000Z',
    knownScopeStart: '2026-08-28T10:00:00.000Z',
  };

  it('1. Window completely in the future produces a single future segment', () => {
    const windowStart = new Date('2026-08-28T14:00:00.000Z').getTime();
    const windowEnd = new Date('2026-08-28T16:00:00.000Z').getTime();
    const duration = windowEnd - windowStart;

    const coverage: CoverageSummary = {
      ...baseCoverage,
      coveredDurationMs: 0,
      coverageRatio: undefined,
      futureDurationMs: duration,
      effectiveScopeStart: undefined,
      effectiveScopeEnd: undefined,
    };

    const segments = buildSegments(coverage, windowStart, windowEnd, 'en', t);
    assert.equal(segments.length, 1);
    assert.equal(segments[0].kind, 'future');
    assert.equal(segments[0].from, windowStart);
    assert.equal(segments[0].to, windowEnd);
  });

  it('2. Half past and half future produces covered past and future segment', () => {
    const windowStart = new Date('2026-08-28T10:00:00.000Z').getTime();
    const windowEnd = new Date('2026-08-28T12:00:00.000Z').getTime();
    const futureMs = 3600000;

    const coverage: CoverageSummary = {
      ...baseCoverage,
      effectiveScopeStart: '2026-08-28T10:00:00.000Z',
      effectiveScopeEnd: '2026-08-28T11:00:00.000Z',
      coveredDurationMs: 3600000,
      futureDurationMs: futureMs,
    };

    const segments = buildSegments(coverage, windowStart, windowEnd, 'en', t);
    assert.equal(segments.length, 2);
    assert.equal(segments[0].kind, 'covered');
    assert.equal(segments[0].from, windowStart);
    assert.equal(segments[0].to, windowStart + 3600000);

    assert.equal(segments[1].kind, 'future');
    assert.equal(segments[1].from, windowStart + 3600000);
    assert.equal(segments[1].to, windowEnd);
  });

  it('3. Outside history + covered + future', () => {
    const windowStart = new Date('2026-08-28T08:00:00.000Z').getTime();
    const windowEnd = new Date('2026-08-28T12:00:00.000Z').getTime();
    const scopeStart = new Date('2026-08-28T09:00:00.000Z').getTime();
    const futureStart = new Date('2026-08-28T11:00:00.000Z').getTime();

    const coverage: CoverageSummary = {
      ...baseCoverage,
      effectiveScopeStart: new Date(scopeStart).toISOString(),
      effectiveScopeEnd: new Date(futureStart).toISOString(),
      outsideKnownScopeMs: scopeStart - windowStart,
      coveredDurationMs: futureStart - scopeStart,
      futureDurationMs: windowEnd - futureStart,
    };

    const segments = buildSegments(coverage, windowStart, windowEnd, 'en', t);
    assert.equal(segments.length, 3);
    assert.equal(segments[0].kind, 'outside');
    assert.equal(segments[0].from, windowStart);
    assert.equal(segments[0].to, scopeStart);

    assert.equal(segments[1].kind, 'covered');
    assert.equal(segments[1].from, scopeStart);
    assert.equal(segments[1].to, futureStart);

    assert.equal(segments[2].kind, 'future');
    assert.equal(segments[2].from, futureStart);
    assert.equal(segments[2].to, windowEnd);
  });

  it('4. Gaps in past with future segment at the end', () => {
    const windowStart = new Date('2026-08-28T10:00:00.000Z').getTime();
    const windowEnd = new Date('2026-08-28T12:00:00.000Z').getTime();
    const gapStart = '2026-08-28T10:30:00.000Z';
    const gapEnd = '2026-08-28T10:45:00.000Z';
    const futureMs = 1800000;

    const coverage: CoverageSummary = {
      ...baseCoverage,
      effectiveScopeStart: '2026-08-28T10:00:00.000Z',
      effectiveScopeEnd: '2026-08-28T11:30:00.000Z',
      futureDurationMs: futureMs,
      mergedGaps: [
        {
          startedAt: gapStart,
          endedAt: gapEnd,
          durationMs: 900000,
          source: 'controller_stream',
          reason: 'controller reconnect',
        },
      ],
    };

    const segments = buildSegments(coverage, windowStart, windowEnd, 'en', t);
    assert.equal(segments.length, 4);
    assert.equal(segments[0].kind, 'covered');
    assert.equal(segments[1].kind, 'controller');
    assert.equal(segments[2].kind, 'covered');
    assert.equal(segments[3].kind, 'future');
    assert.equal(segments[3].to, windowEnd);
  });

  it('5. No known scope + future segment', () => {
    const windowStart = new Date('2026-08-28T10:00:00.000Z').getTime();
    const windowEnd = new Date('2026-08-28T12:00:00.000Z').getTime();
    const futureMs = 3600000;

    const coverage: CoverageSummary = {
      ...baseCoverage,
      effectiveScopeStart: undefined,
      effectiveScopeEnd: undefined,
      knownScopeStart: undefined,
      coveredDurationMs: 0,
      outsideKnownScopeMs: 3600000,
      futureDurationMs: futureMs,
    };

    const segments = buildSegments(coverage, windowStart, windowEnd, 'en', t);
    assert.equal(segments.length, 2);
    assert.equal(segments[0].kind, 'outside');
    assert.equal(segments[0].from, windowStart);
    assert.equal(segments[0].to, windowStart + 3600000);

    assert.equal(segments[1].kind, 'future');
    assert.equal(segments[1].from, windowStart + 3600000);
    assert.equal(segments[1].to, windowEnd);
  });

  it('6. Custom range ending after now triggers future segment', () => {
    const windowStart = new Date('2026-08-28T10:00:00.000Z').getTime();
    const windowEnd = new Date('2026-08-28T14:00:00.000Z').getTime();
    const futureMs = 7200000;

    const coverage: CoverageSummary = {
      ...baseCoverage,
      effectiveScopeStart: '2026-08-28T10:00:00.000Z',
      effectiveScopeEnd: '2026-08-28T12:00:00.000Z',
      futureDurationMs: futureMs,
    };

    const segments = buildSegments(coverage, windowStart, windowEnd, 'en', t);
    const lastSeg = segments[segments.length - 1];
    assert.equal(lastSeg.kind, 'future');
    assert.equal(lastSeg.to, windowEnd);
  });

  it('7. Normal Today (futureDurationMs = 0) has NO future segment', () => {
    const windowStart = new Date('2026-08-28T00:00:00.000Z').getTime();
    const windowEnd = new Date('2026-08-28T12:00:00.000Z').getTime();

    const coverage: CoverageSummary = {
      ...baseCoverage,
      effectiveScopeStart: '2026-08-28T00:00:00.000Z',
      effectiveScopeEnd: '2026-08-28T12:00:00.000Z',
      futureDurationMs: 0,
    };

    const segments = buildSegments(coverage, windowStart, windowEnd, 'en', t);
    assert.ok(segments.every((s) => s.kind !== 'future'));
  });
});
