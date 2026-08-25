import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { formatBytes } from './format.js';
import { getQuickWindow } from './time.js';
import { mapApiErrorCode, formatErrorMessage } from './error.js';

describe('Utility: formatBytes', () => {
  it('handles zero, null, undefined, and NaN', () => {
    assert.equal(formatBytes(0), '0 B');
    assert.equal(formatBytes(null), '0 B');
    assert.equal(formatBytes(undefined), '0 B');
    assert.equal(formatBytes(NaN), '0 B');
  });

  it('formats exact bytes under 1024', () => {
    assert.equal(formatBytes(512), '512 B');
    assert.equal(formatBytes(1023), '1023 B');
  });

  it('formats binary byte units KiB, MiB, GiB', () => {
    assert.equal(formatBytes(1024), '1.00 KiB');
    assert.equal(formatBytes(1048576), '1.00 MiB');
    assert.equal(formatBytes(1073741824), '1.00 GiB');
    assert.equal(formatBytes(1572864), '1.50 MiB');
  });
});

describe('Utility: time windows', () => {
  it('computes today and yesterday exact local-day boundaries', () => {
    const fixedNow = new Date('2026-08-25T14:30:45.123Z');
    const today = getQuickWindow('today', fixedNow);
    const todayStartDate = new Date(today.from);

    assert.equal(todayStartDate.getHours(), 0);
    assert.equal(todayStartDate.getMinutes(), 0);
    assert.equal(todayStartDate.getSeconds(), 0);
    assert.equal(todayStartDate.getMilliseconds(), 0);
    assert.equal(today.to, fixedNow.toISOString());

    const yday = getQuickWindow('yesterday', fixedNow);
    const ydayStartDate = new Date(yday.from);
    const ydayEndDate = new Date(yday.to);

    assert.equal(ydayStartDate.getHours(), 0);
    assert.equal(ydayStartDate.getMinutes(), 0);
    assert.equal(ydayStartDate.getSeconds(), 0);
    assert.equal(ydayStartDate.getMilliseconds(), 0);
    // 昨天结束时间必须精确等于今天开始时间 (半开区间连续对齐)
    assert.equal(yday.to, today.from);
    assert.equal(ydayEndDate.getTime() - ydayStartDate.getTime(), 86400000);
  });

  it('computes 7d and 30d local-day aware start boundaries', () => {
    const fixedNow = new Date('2026-08-25T14:30:45.123Z');
    const w7d = getQuickWindow('7d', fixedNow);
    const start7d = new Date(w7d.from);

    assert.equal(start7d.getHours(), 0);
    assert.equal(start7d.getMinutes(), 0);
    assert.equal(start7d.getSeconds(), 0);
    assert.equal(start7d.getMilliseconds(), 0);
    assert.equal(w7d.to, fixedNow.toISOString());

    // 7d 包含今天 + 过去 6 个完整自然日 (start7d 到 today 零点正好是 6 天)
    const todayStart = new Date(getQuickWindow('today', fixedNow).from);
    assert.equal(todayStart.getTime() - start7d.getTime(), 6 * 86400000);

    const w30d = getQuickWindow('30d', fixedNow);
    const start30d = new Date(w30d.from);
    assert.equal(start30d.getHours(), 0);
    assert.equal(start30d.getMinutes(), 0);
    assert.equal(start30d.getSeconds(), 0);
    assert.equal(start30d.getMilliseconds(), 0);
    assert.equal(w30d.to, fixedNow.toISOString());

    // 30d 包含今天 + 过去 29 个完整自然日 (start30d 到 today 零点正好是 29 天)
    assert.equal(todayStart.getTime() - start30d.getTime(), 29 * 86400000);
  });
});

describe('Utility: error mapping', () => {
  it('maps known API error codes to human readable strings', () => {
    assert.equal(mapApiErrorCode('UNAUTHORIZED'), 'Session authentication failed. Please reopen ProxyLens.');
    assert.equal(mapApiErrorCode('DB_INCOMPATIBLE'), 'Database schema version does not match this ProxyLens version.');
    assert.equal(mapApiErrorCode('UNKNOWN_CODE'), 'An unexpected error occurred.');
  });

  it('formats structured API error responses safely', () => {
    const res = formatErrorMessage({
      error: { code: 'CONNECTION_NOT_FOUND', message: 'conn-123 does not exist' }
    });
    assert.match(res, /The requested connection was not found/);
    assert.match(res, /conn-123/);
  });
});
