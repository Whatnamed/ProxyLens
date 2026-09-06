import assert from 'node:assert/strict';
import test from 'node:test';
import {
  ceilToUtcHour,
  deriveTemporalComparisonRange,
  floorToUtcHour,
} from './time.js';

test('temporal comparison shifts quick ranges by local calendar days', () => {
  const recent = {
    from: new Date(2026, 8, 6, 0, 0, 0, 0).toISOString(),
    to: new Date(2026, 8, 6, 13, 37, 0, 0).toISOString(),
  };
  const comparison = deriveTemporalComparisonRange('today', recent);
  const baselineFrom = new Date(comparison.baselineRequested.from);
  const baselineTo = new Date(comparison.baselineRequested.to);
  const recentFrom = new Date(comparison.recentRequested.from);
  const recentTo = new Date(comparison.recentRequested.to);

  assert.equal(baselineFrom.getHours(), recentFrom.getHours());
  assert.equal(baselineFrom.getDate(), recentFrom.getDate() - 1);
  assert.equal(baselineTo.getHours(), recentTo.getHours());
  assert.equal(baselineTo.getDate(), recentTo.getDate() - 1);
});

test('custom temporal comparison uses an immediately preceding equal interval', () => {
  const recent = {
    from: '2026-09-06T10:15:00.000Z',
    to: '2026-09-06T14:45:00.000Z',
  };
  const comparison = deriveTemporalComparisonRange('custom', recent);
  assert.deepEqual(comparison.baselineRequested, {
    from: '2026-09-06T05:45:00.000Z',
    to: '2026-09-06T10:15:00.000Z',
  });
});

test('effective temporal ranges clip inward to complete UTC hours', () => {
  assert.equal(ceilToUtcHour('2026-09-06T10:17:00.000Z').toISOString(), '2026-09-06T11:00:00.000Z');
  assert.equal(floorToUtcHour('2026-09-06T13:59:59.999Z').toISOString(), '2026-09-06T13:00:00.000Z');
  const comparison = deriveTemporalComparisonRange('custom', {
    from: '2026-09-06T10:17:00.000Z',
    to: '2026-09-06T13:59:59.999Z',
  });
  assert.deepEqual(comparison.recentEffective, {
    from: '2026-09-06T11:00:00.000Z',
    to: '2026-09-06T13:00:00.000Z',
  });
});

test('less than one complete hour makes comparison unavailable', () => {
  const comparison = deriveTemporalComparisonRange('custom', {
    from: '2026-09-06T10:15:00.000Z',
    to: '2026-09-06T10:59:59.000Z',
  });
  assert.equal(comparison.baselineEffective, null);
  assert.equal(comparison.recentEffective, null);
  assert.equal(comparison.unavailableReason, 'insufficient_full_hours');
});

test('live ticks within an hour keep the effective recent key stable', () => {
  const first = deriveTemporalComparisonRange('today', {
    from: '2026-09-06T00:00:00.000Z',
    to: '2026-09-06T13:30:00.000Z',
  });
  const second = deriveTemporalComparisonRange('today', {
    from: '2026-09-06T00:00:00.000Z',
    to: '2026-09-06T13:59:59.000Z',
  });
  const crossed = deriveTemporalComparisonRange('today', {
    from: '2026-09-06T00:00:00.000Z',
    to: '2026-09-06T14:00:01.000Z',
  });
  assert.deepEqual(second.recentEffective, first.recentEffective);
  assert.notDeepEqual(crossed.recentEffective, first.recentEffective);
});
