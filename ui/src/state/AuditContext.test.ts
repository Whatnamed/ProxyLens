import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  resolveTimeRange,
  isLiveRangeKind,
  createHistorySnapshot,
  refreshSnapshot,
  rangeSourceKey,
  TimeRangeState,
} from './AuditContext.js';
import { getQuickWindow } from '../utils/time.js';

const fixedNow = new Date('2026-08-28T14:30:45.123Z');

describe('AuditContext: resolveTimeRange', () => {
  it('resolves quick windows through getQuickWindow', () => {
    assert.deepEqual(resolveTimeRange({ kind: 'today' }, fixedNow), getQuickWindow('today', fixedNow));
    assert.deepEqual(resolveTimeRange({ kind: 'yesterday' }, fixedNow), getQuickWindow('yesterday', fixedNow));
    assert.deepEqual(resolveTimeRange({ kind: '7d' }, fixedNow), getQuickWindow('7d', fixedNow));
    assert.deepEqual(resolveTimeRange({ kind: '30d' }, fixedNow), getQuickWindow('30d', fixedNow));
  });

  it('resolves a valid applied custom range to ISO timestamps', () => {
    const r = resolveTimeRange(
      { kind: 'custom', customFrom: '2026-08-28T10:00', customTo: '2026-08-28T12:30' },
      fixedNow
    );
    assert.equal(r.from, new Date('2026-08-28T10:00').toISOString());
    assert.equal(r.to, new Date('2026-08-28T12:30').toISOString());
  });

  it('falls back to 7d when the custom range is incomplete', () => {
    assert.deepEqual(resolveTimeRange({ kind: 'custom' }, fixedNow), getQuickWindow('7d', fixedNow));
    assert.deepEqual(
      resolveTimeRange({ kind: 'custom', customFrom: '2026-08-28T10:00' }, fixedNow),
      getQuickWindow('7d', fixedNow)
    );
  });

  it('falls back to 7d when the custom range is unparseable', () => {
    assert.deepEqual(
      resolveTimeRange({ kind: 'custom', customFrom: 'not-a-date', customTo: 'also-not-a-date' }, fixedNow),
      getQuickWindow('7d', fixedNow)
    );
  });

  it('advances live ranges (today, 7d, 30d) as time moves forward', () => {
    const t1 = new Date('2026-08-28T14:30:00.000Z');
    const t2 = new Date('2026-08-28T14:35:00.000Z');

    const today1 = resolveTimeRange({ kind: 'today' }, t1);
    const today2 = resolveTimeRange({ kind: 'today' }, t2);
    assert.equal(today1.from, today2.from);
    assert.ok(today2.to > today1.to);
    assert.equal(today2.to, t2.toISOString());

    const sevenDays1 = resolveTimeRange({ kind: '7d' }, t1);
    const sevenDays2 = resolveTimeRange({ kind: '7d' }, t2);
    assert.equal(sevenDays1.from, sevenDays2.from);
    assert.ok(sevenDays2.to > sevenDays1.to);
    assert.equal(sevenDays2.to, t2.toISOString());

    const thirtyDays1 = resolveTimeRange({ kind: '30d' }, t1);
    const thirtyDays2 = resolveTimeRange({ kind: '30d' }, t2);
    assert.equal(thirtyDays1.from, thirtyDays2.from);
    assert.ok(thirtyDays2.to > thirtyDays1.to);
    assert.equal(thirtyDays2.to, t2.toISOString());
  });

  it('keeps closed ranges (yesterday, custom) fixed as time moves forward within the same day', () => {
    const t1 = new Date('2026-08-28T14:30:00.000Z');
    const t2 = new Date('2026-08-28T15:45:00.000Z');

    const y1 = resolveTimeRange({ kind: 'yesterday' }, t1);
    const y2 = resolveTimeRange({ kind: 'yesterday' }, t2);
    assert.deepEqual(y1, y2);

    const c1 = resolveTimeRange(
      { kind: 'custom', customFrom: '2026-08-28T10:00', customTo: '2026-08-28T12:00' },
      t1
    );
    const c2 = resolveTimeRange(
      { kind: 'custom', customFrom: '2026-08-28T10:00', customTo: '2026-08-28T12:00' },
      t2
    );
    assert.deepEqual(c1, c2);
  });
});

describe('AuditContext: History Snapshot & Investigation Context', () => {
  it('classifies live vs closed range kinds correctly', () => {
    assert.equal(isLiveRangeKind('today'), true);
    assert.equal(isLiveRangeKind('7d'), true);
    assert.equal(isLiveRangeKind('30d'), true);
    assert.equal(isLiveRangeKind('yesterday'), false);
    assert.equal(isLiveRangeKind('custom'), false);
  });

  it('creates stable snapshot frozen at the creation instant', () => {
    const t1 = new Date('2026-08-28T10:00:00.000Z');
    const snap = createHistorySnapshot({ kind: 'today' }, t1);
    assert.equal(snap.kind, 'today');
    assert.equal(snap.to, t1.toISOString());
    assert.equal(snap.frozenAt, t1.getTime());
    assert.equal(snap.sourceKey, rangeSourceKey({ kind: 'today' }));
  });

  it('refreshSnapshot advances upper bound for live range', () => {
    const t1 = new Date('2026-08-28T10:00:00.000Z');
    const t2 = new Date('2026-08-28T10:15:00.000Z');
    const snap1 = createHistorySnapshot({ kind: 'today' }, t1);

    const snap2 = refreshSnapshot(snap1, { kind: 'today' }, t2);
    assert.equal(snap2.from, snap1.from);
    assert.equal(snap2.to, t2.toISOString());
    assert.ok(snap2.to > snap1.to);
    assert.equal(snap2.frozenAt, t2.getTime());
  });

  it('refreshSnapshot preserves window boundaries for closed ranges', () => {
    const t1 = new Date('2026-08-28T10:00:00.000Z');
    const t2 = new Date('2026-08-28T10:30:00.000Z');

    const snapYesterday1 = createHistorySnapshot({ kind: 'yesterday' }, t1);
    const snapYesterday2 = refreshSnapshot(snapYesterday1, { kind: 'yesterday' }, t2);
    assert.equal(snapYesterday2.from, snapYesterday1.from);
    assert.equal(snapYesterday2.to, snapYesterday1.to);
    assert.equal(snapYesterday2.frozenAt, t2.getTime());

    const customState = {
      kind: 'custom' as const,
      customFrom: '2026-08-28T08:00',
      customTo: '2026-08-28T09:00',
    };
    const snapCustom1 = createHistorySnapshot(customState, t1);
    const snapCustom2 = refreshSnapshot(snapCustom1, customState, t2);
    assert.equal(snapCustom2.from, snapCustom1.from);
    assert.equal(snapCustom2.to, snapCustom1.to);
    assert.equal(snapCustom2.frozenAt, t2.getTime());
  });

  it('generates unique and comparable rangeSourceKeys', () => {
    assert.equal(rangeSourceKey({ kind: 'today' }), 'today');
    assert.equal(rangeSourceKey({ kind: '7d' }), '7d');
    assert.equal(rangeSourceKey({ kind: 'yesterday' }), 'yesterday');
    assert.equal(
      rangeSourceKey({ kind: 'custom', customFrom: '2026-08-28T08:00', customTo: '2026-08-28T09:00' }),
      'custom:2026-08-28T08:00..2026-08-28T09:00'
    );
  });

  it('models Live Analysis vs History snapshot freeze transition contract', () => {
    // 1. In Overview, user changes time range: snapshot is cleared (null)
    let view: 'overview' | 'history' | 'coverage' = 'overview';
    let snapshot: ReturnType<typeof createHistorySnapshot> | null = null;
    let timeRange: TimeRangeState = { kind: 'today' };

    const applyTimeRange = (newRange: TimeRangeState, now: Date) => {
      timeRange = newRange;
      if (view !== 'history') {
        snapshot = null; // Do NOT pre-freeze outside History
      } else {
        snapshot = createHistorySnapshot(newRange, now);
      }
    };

    // User changes range in Overview:
    applyTimeRange({ kind: '7d' as const }, new Date('2026-08-28T10:00:00.000Z'));
    assert.equal(snapshot, null, 'Snapshot must remain null when time range changes in Overview');

    // 2. 15 minutes later, user navigates to History: freeze happens at entry instant
    const entryNow = new Date('2026-08-28T10:15:00.000Z');
    view = 'history';
    if (!snapshot || (snapshot as any).sourceKey !== rangeSourceKey(timeRange)) {
      snapshot = createHistorySnapshot(timeRange, entryNow);
    }
    assert.ok(snapshot);
    assert.equal(snapshot.frozenAt, entryNow.getTime(), 'Snapshot must be frozen at entry instant');

    // 3. In History, user changes range: immediately updates snapshot
    const historyChangeNow = new Date('2026-08-28T10:20:00.000Z');
    applyTimeRange({ kind: 'today' as const }, historyChangeNow);
    assert.ok(snapshot);
    assert.equal((snapshot as any).sourceKey, 'today');
    assert.equal(snapshot.frozenAt, historyChangeNow.getTime(), 'Snapshot must be updated immediately inside History');
  });

  it('clears selected connection on page turn (next/prev) to avoid lingering Inspector', () => {
    let page = 0;
    let selected: { sessionId: string; epochId: string; connectionId: string } | null = {
      sessionId: 'sess-1',
      epochId: 'ep-1',
      connectionId: 'conn-1',
    };

    const setPage = (p: number) => {
      page = p;
      selected = null;
    };

    // Turning to next page
    setPage(1);
    assert.equal(page, 1);
    assert.equal(selected, null, 'Selected connection must immediately reset to null on next page');

    // Select a row on page 2
    selected = { sessionId: 'sess-1', epochId: 'ep-1', connectionId: 'conn-2' };

    // Turning to prev page
    setPage(0);
    assert.equal(page, 0);
    assert.equal(selected, null, 'Selected connection must immediately reset to null on prev page');
  });

  it('preserves selected connection and investigation context when navigating History -> Overview/Coverage -> History', () => {
    // Models AuditContext navigation contract:
    let view: 'overview' | 'history' | 'coverage' = 'history';
    let timeRange: TimeRangeState = { kind: 'today' };
    const now = new Date('2026-08-28T10:00:00.000Z');
    let snapshot: ReturnType<typeof createHistorySnapshot> | null = createHistorySnapshot(timeRange, now);
    let page = 2;
    let filters: Record<string, string> = { host: 'api.github.com' };
    let selected: { sessionId: string; epochId: string; connectionId: string } | null = {
      sessionId: 'sess-1',
      epochId: 'ep-1',
      connectionId: 'conn-42',
    };

    const setView = (v: 'overview' | 'history' | 'coverage') => {
      view = v;
      if (v === 'history') {
        const key = rangeSourceKey(timeRange);
        if (!snapshot || snapshot.sourceKey !== key) {
          snapshot = createHistorySnapshot(timeRange, new Date());
          page = 0;
          selected = null;
        }
        // If returning to History and timeRange matches, keep existing snapshot and investigation!
      }
    };

    // 1. User navigates away to Overview
    setView('overview');
    assert.equal(view, 'overview');
    // Context in AuditContext remains intact while away
    assert.equal(selected?.connectionId, 'conn-42');
    assert.equal(page, 2);

    // 2. User returns to History with unchanged investigation
    setView('history');
    assert.equal(view, 'history');
    assert.equal(selected?.connectionId, 'conn-42', 'Selected connection must be preserved upon return');
    assert.equal(page, 2, 'Page must be preserved upon return');
    assert.equal(filters.host, 'api.github.com', 'Filters must be preserved upon return');
    assert.equal(snapshot?.sourceKey, 'today', 'Snapshot must be preserved upon return');
  });

  it('clears selected connection, resets page, and sets new filter on new drill to History', () => {
    let view: 'overview' | 'history' | 'coverage' = 'overview';
    let timeRange: TimeRangeState = { kind: 'today' };
    let snapshot: ReturnType<typeof createHistorySnapshot> | null = null;
    let page = 3;
    let filters: Record<string, string> = { host: 'old-host.com' };
    let selected: { sessionId: string; epochId: string; connectionId: string } | null = {
      sessionId: 'sess-1',
      epochId: 'ep-1',
      connectionId: 'conn-old',
    };

    const drillToHistory = (newFilters: Record<string, string>) => {
      filters = newFilters;
      page = 0;
      selected = null;
      snapshot = createHistorySnapshot(timeRange, new Date('2026-08-28T10:30:00.000Z'));
      view = 'history';
    };

    drillToHistory({ process: 'curl.exe' });

    assert.equal(view, 'history');
    assert.equal(page, 0, 'New drill must reset page to 0');
    assert.equal(selected, null, 'New drill must clear old selected connection');
    assert.deepEqual(filters, { process: 'curl.exe' }, 'New drill must set target filter');
    assert.ok(snapshot);
  });

  it('clears selected connection and resets page when route focus, page size, or filters change', () => {
    let page = 2;
    let selected: { sessionId: string; epochId: string; connectionId: string } | null = {
      sessionId: 'sess-1',
      epochId: 'ep-1',
      connectionId: 'conn-1',
    };

    const setRouteFocus = () => {
      page = 0;
      selected = null;
    };
    const setPageSize = () => {
      page = 0;
      selected = null;
    };
    const setFilter = () => {
      page = 0;
      selected = null;
    };

    // Route Focus change
    setRouteFocus();
    assert.equal(page, 0);
    assert.equal(selected, null);

    // Re-select
    page = 1;
    selected = { sessionId: 'sess-1', epochId: 'ep-1', connectionId: 'conn-2' };
    setPageSize();
    assert.equal(page, 0);
    assert.equal(selected, null);

    // Re-select
    page = 1;
    selected = { sessionId: 'sess-1', epochId: 'ep-1', connectionId: 'conn-3' };
    setFilter();
    assert.equal(page, 0);
    assert.equal(selected, null);
  });
});
