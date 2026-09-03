import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { keepLiveTickOnly } from './queries.js';

describe('Query caching: keepLiveTickOnly semantic scope guard', () => {
  const previousData = { proxyBytes: 1024, directBytes: 2048 };

  it('keeps previous data during live tick when window start and route focus are identical', () => {
    const from = '2026-08-28T00:00:00.000Z';
    const prevTo = '2026-08-28T10:00:00.000Z';
    const nextTo = '2026-08-28T10:00:30.000Z'; // 30s tick
    const route = 'PROXY';

    const keeper = keepLiveTickOnly(from, nextTo, route);
    const result = keeper(previousData, {
      queryKey: ['summary', from, prevTo, route],
    });
    assert.equal(result, previousData);
  });

  it('clears previous data when Route Focus changes (e.g. PROXY -> DIRECT) to avoid stale evidence', () => {
    const from = '2026-08-28T00:00:00.000Z';
    const to = '2026-08-28T10:00:00.000Z';

    const keeper = keepLiveTickOnly(from, to, 'DIRECT');
    const result = keeper(previousData, {
      queryKey: ['summary', from, to, 'PROXY'],
    });
    assert.equal(result, undefined);
  });

  it('clears previous data when Route Focus changes from specific route to ALL', () => {
    const from = '2026-08-28T00:00:00.000Z';
    const to = '2026-08-28T10:00:00.000Z';

    const keeper = keepLiveTickOnly(from, to, 'ALL');
    const result = keeper(previousData, {
      queryKey: ['summary', from, to, 'PROXY'],
    });
    assert.equal(result, undefined);
  });

  it('clears previous data when Time Range changes (e.g. Today -> 30d)', () => {
    const prevFrom = '2026-08-28T00:00:00.000Z';
    const newFrom = '2026-07-29T00:00:00.000Z';
    const to = '2026-08-28T10:00:00.000Z';
    const route = 'PROXY';

    const keeper = keepLiveTickOnly(newFrom, to, route);
    const result = keeper(previousData, {
      queryKey: ['summary', prevFrom, to, route],
    });
    assert.equal(result, undefined);
  });

  it('clears previous data when Time Range moves backwards', () => {
    const from = '2026-08-28T00:00:00.000Z';
    const prevTo = '2026-08-28T10:00:30.000Z';
    const backwardTo = '2026-08-28T09:00:00.000Z';
    const route = 'PROXY';

    const keeper = keepLiveTickOnly(from, backwardTo, route);
    const result = keeper(previousData, {
      queryKey: ['summary', from, prevTo, route],
    });
    assert.equal(result, undefined);
  });

  it('works for unscoped queries such as Coverage (no route scope)', () => {
    const from = '2026-08-28T00:00:00.000Z';
    const prevTo = '2026-08-28T10:00:00.000Z';
    const nextTo = '2026-08-28T10:00:30.000Z';

    const keeper = keepLiveTickOnly(from, nextTo);
    const result = keeper(previousData, {
      queryKey: ['coverage', from, prevTo],
    });
    assert.equal(result, previousData);

    // If window start changes (e.g. Yesterday)
    const yesterdayFrom = '2026-08-27T00:00:00.000Z';
    const keeperYesterday = keepLiveTickOnly(yesterdayFrom, nextTo);
    const yesterdayResult = keeperYesterday(previousData, {
      queryKey: ['coverage', from, prevTo],
    });
    assert.equal(yesterdayResult, undefined);
  });
});
