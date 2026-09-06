import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { keepLiveTickOnly, isLiveRangeKey, processChangesQueryKey, temporalFindingsQueryKey } from './queries.js';
import { deriveTemporalComparisonRange } from '../utils/time.js';

describe('Query caching: keepLiveTickOnly semantic scope guard', () => {
  const previousData = { proxyBytes: 1024, directBytes: 2048 };

  it('identifies live rolling range kinds accurately', () => {
    assert.equal(isLiveRangeKey('today'), true);
    assert.equal(isLiveRangeKey('7d'), true);
    assert.equal(isLiveRangeKey('30d'), true);
    assert.equal(isLiveRangeKey('yesterday'), false);
    assert.equal(isLiveRangeKey('custom:2026-08-28T10:00..2026-08-28T12:00'), false);
    assert.equal(isLiveRangeKey(undefined), false);
  });

  it('keeps previous data during live tick on live rolling ranges (Today, 7d, 30d)', () => {
    const from = '2026-08-28T00:00:00.000Z';
    const prevTo = '2026-08-28T10:00:00.000Z';
    const nextTo = '2026-08-28T10:00:30.000Z'; // 30s tick
    const route = 'PROXY';

    const keeperToday = keepLiveTickOnly(from, nextTo, route, 'today');
    const resultToday = keeperToday(previousData, {
      queryKey: ['summary', from, prevTo, route, 'today'],
    });
    assert.equal(resultToday, previousData);

    const keeper7d = keepLiveTickOnly(from, nextTo, route, '7d');
    const result7d = keeper7d(previousData, {
      queryKey: ['summary', from, prevTo, route, '7d'],
    });
    assert.equal(result7d, previousData);
  });

  it('blocks keeping previous data when user manually extends Custom end time (even with same from and larger to)', () => {
    const from = '2026-08-28T10:00:00.000Z';
    const prevTo = '2026-08-28T12:00:00.000Z';
    const extendedTo = '2026-08-28T14:00:00.000Z';
    const route = 'PROXY';

    const prevRangeKey = 'custom:2026-08-28T10:00..2026-08-28T12:00';
    const nextRangeKey = 'custom:2026-08-28T10:00..2026-08-28T14:00';

    const keeper = keepLiveTickOnly(from, extendedTo, route, nextRangeKey);
    const result = keeper(previousData, {
      queryKey: ['summary', from, prevTo, route, prevRangeKey],
    });
    assert.equal(result, undefined, 'Custom range manual change must enter explicit loading state');
  });

  it('blocks keeping previous data when switching from Today to Custom', () => {
    const from = '2026-08-28T00:00:00.000Z';
    const prevTo = '2026-08-28T10:00:00.000Z';
    const nextTo = '2026-08-28T12:00:00.000Z';
    const route = 'PROXY';

    const keeper = keepLiveTickOnly(from, nextTo, route, 'custom:2026-08-28T00:00..2026-08-28T12:00');
    const result = keeper(previousData, {
      queryKey: ['summary', from, prevTo, route, 'today'],
    });
    assert.equal(result, undefined);
  });

  it('clears previous data when Route Focus changes (e.g. PROXY -> DIRECT) to avoid stale evidence', () => {
    const from = '2026-08-28T00:00:00.000Z';
    const to = '2026-08-28T10:00:00.000Z';

    const keeper = keepLiveTickOnly(from, to, 'DIRECT', 'today');
    const result = keeper(previousData, {
      queryKey: ['summary', from, to, 'PROXY', 'today'],
    });
    assert.equal(result, undefined);
  });

  it('clears previous data when Route Focus changes from specific route to ALL', () => {
    const from = '2026-08-28T00:00:00.000Z';
    const to = '2026-08-28T10:00:00.000Z';

    const keeper = keepLiveTickOnly(from, to, 'ALL', 'today');
    const result = keeper(previousData, {
      queryKey: ['summary', from, to, 'PROXY', 'today'],
    });
    assert.equal(result, undefined);
  });

  it('clears previous data when Time Range changes (e.g. Today -> 7d)', () => {
    const prevFrom = '2026-08-28T00:00:00.000Z';
    const newFrom = '2026-08-21T00:00:00.000Z';
    const to = '2026-08-28T10:00:00.000Z';
    const route = 'PROXY';

    const keeper = keepLiveTickOnly(newFrom, to, route, '7d');
    const result = keeper(previousData, {
      queryKey: ['summary', prevFrom, to, route, 'today'],
    });
    assert.equal(result, undefined);
  });

  it('clears previous data on closed ranges like Yesterday', () => {
    const from = '2026-08-27T00:00:00.000Z';
    const to = '2026-08-27T23:59:59.000Z';
    const route = 'PROXY';

    const keeper = keepLiveTickOnly(from, to, route, 'yesterday');
    const result = keeper(previousData, {
      queryKey: ['summary', from, to, route, 'yesterday'],
    });
    assert.equal(result, undefined, 'Closed range must never keep previous data');
  });

  it('clears previous data when Time Range moves backwards', () => {
    const from = '2026-08-28T00:00:00.000Z';
    const prevTo = '2026-08-28T10:00:30.000Z';
    const backwardTo = '2026-08-28T09:00:00.000Z';
    const route = 'PROXY';

    const keeper = keepLiveTickOnly(from, backwardTo, route, 'today');
    const result = keeper(previousData, {
      queryKey: ['summary', from, prevTo, route, 'today'],
    });
    assert.equal(result, undefined);
  });

  it('works for unscoped queries such as Coverage (no route scope)', () => {
    const from = '2026-08-28T00:00:00.000Z';
    const prevTo = '2026-08-28T10:00:00.000Z';
    const nextTo = '2026-08-28T10:00:30.000Z';

    const keeper = keepLiveTickOnly(from, nextTo, undefined, 'today');
    const result = keeper(previousData, {
      queryKey: ['coverage', from, prevTo, 'today'],
    });
    assert.equal(result, previousData);

    // If window start changes (e.g. Yesterday)
    const yesterdayFrom = '2026-08-27T00:00:00.000Z';
    const keeperYesterday = keepLiveTickOnly(yesterdayFrom, nextTo, undefined, 'yesterday');
    const yesterdayResult = keeperYesterday(previousData, {
      queryKey: ['coverage', from, prevTo, 'today'],
    });
    assert.equal(yesterdayResult, undefined);
  });
});

describe('History query policy: useConnectionsQuery scope isolation', () => {
  it('generates distinct query keys for different routes, filters, and pages', () => {
    const baseParams = {
      from: '2026-08-28T10:00:00.000Z',
      to: '2026-08-28T11:00:00.000Z',
      route: 'PROXY',
      filters: { host: 'github.com' },
      limit: 50,
      offset: 0,
    };
    const key1 = ['connections', baseParams.from, baseParams.to, baseParams.route, baseParams.filters, baseParams.limit, baseParams.offset];
    const keyNextPage = ['connections', baseParams.from, baseParams.to, baseParams.route, baseParams.filters, baseParams.limit, 50];
    const keyDiffHost = ['connections', baseParams.from, baseParams.to, baseParams.route, { host: 'google.com' }, baseParams.limit, baseParams.offset];
    const keyDiffRoute = ['connections', baseParams.from, baseParams.to, 'DIRECT', baseParams.filters, baseParams.limit, baseParams.offset];

    assert.notDeepEqual(key1, keyNextPage);
    assert.notDeepEqual(key1, keyDiffHost);
    assert.notDeepEqual(key1, keyDiffRoute);
  });
});

describe('Temporal process comparison query policy', () => {
  it('keys by source identity, all effective boundaries, and limit', () => {
    const comparison = deriveTemporalComparisonRange('today', {
      from: '2026-09-06T00:00:00.000Z',
      to: '2026-09-06T13:45:00.000Z',
    });
    const key = processChangesQueryKey('today', comparison, 20);
    assert.deepEqual(key, [
      'processChanges',
      'today',
      comparison.baselineEffective?.from,
      comparison.baselineEffective?.to,
      comparison.recentEffective?.from,
      comparison.recentEffective?.to,
      20,
    ]);
    assert.notDeepEqual(key, processChangesQueryKey('today', comparison, 50));
    assert.notDeepEqual(key, processChangesQueryKey('7d', comparison, 20));
  });
});

describe('Generic temporal findings query policy', () => {
  it('keys by source identity, all effective boundaries, and limit', () => {
    const comparison = deriveTemporalComparisonRange('today', {
      from: '2026-09-06T00:00:00.000Z',
      to: '2026-09-06T13:45:00.000Z',
    });
    const key = temporalFindingsQueryKey('today', comparison, 20);
    assert.deepEqual(key, [
      'temporalFindings',
      'today',
      comparison.baselineEffective?.from,
      comparison.baselineEffective?.to,
      comparison.recentEffective?.from,
      comparison.recentEffective?.to,
      20,
    ]);
    assert.notDeepEqual(key, temporalFindingsQueryKey('today', comparison, 50));
    assert.notDeepEqual(key, temporalFindingsQueryKey('7d', comparison, 20));
  });
});
