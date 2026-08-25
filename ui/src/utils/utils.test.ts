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
  it('computes today and yesterday boundaries correctly', () => {
    const fixedNow = new Date('2026-08-25T14:30:00.000Z');
    const today = getQuickWindow('today', fixedNow);
    assert.ok(today.from.length > 0);
    assert.ok(today.to.length > 0);
    assert.ok(new Date(today.from) <= new Date(today.to));

    const yday = getQuickWindow('yesterday', fixedNow);
    assert.ok(new Date(yday.from) <= new Date(yday.to));
  });

  it('computes 7d and 30d relative windows correctly', () => {
    const fixedNow = new Date('2026-08-25T14:30:00.000Z');
    const w7d = getQuickWindow('7d', fixedNow);
    const diff7d = new Date(w7d.to).getTime() - new Date(w7d.from).getTime();
    assert.equal(diff7d, 7 * 86400000);

    const w30d = getQuickWindow('30d', fixedNow);
    const diff30d = new Date(w30d.to).getTime() - new Date(w30d.from).getTime();
    assert.equal(diff30d, 30 * 86400000);
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
