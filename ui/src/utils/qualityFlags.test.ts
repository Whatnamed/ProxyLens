import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { qualityFlagLabels } from './qualityFlags.js';

describe('qualityFlagLabels wire-shape normalization', () => {
  // Regression: the Query API returns qualityFlags as a boolean map
  // ({ missing_process: true, ... }), not a string list. Calling .map on the
  // object crashed the whole app (white screen).
  it('normalizes the boolean-map wire shape returned by the Query API', () => {
    assert.deepEqual(
      qualityFlagLabels({ missing_process: true, missing_host: false, stale_metadata: true }),
      ['missing_process', 'stale_metadata']
    );
  });

  it('returns an empty list for an empty boolean map', () => {
    assert.deepEqual(qualityFlagLabels({}), []);
  });

  it('passes through the string-list wire shape', () => {
    assert.deepEqual(qualityFlagLabels(['missing_process', 'ambiguous_relay']), ['missing_process', 'ambiguous_relay']);
  });

  it('filters empty or non-string entries in the string-list shape', () => {
    assert.deepEqual(qualityFlagLabels(['' , 'valid', null as unknown as string]), ['valid']);
  });

  it('returns an empty list for null or undefined flags', () => {
    assert.deepEqual(qualityFlagLabels(null), []);
    assert.deepEqual(qualityFlagLabels(undefined), []);
  });
});
