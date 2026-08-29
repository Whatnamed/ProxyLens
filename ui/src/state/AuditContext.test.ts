import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { resolveTimeRange } from './AuditContext.js';
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
});
