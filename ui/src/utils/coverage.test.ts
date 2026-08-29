import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { classifyGapSources } from './coverage.js';

describe('Coverage gap provenance classification', () => {
  it('classifies controller_stream as Controller gap', () => {
    const p = classifyGapSources(['controller_stream']);
    assert.equal(p.kind, 'controller');
    assert.equal(p.label, 'Controller gap');
  });

  it('classifies collector_session_boundary as Collector offline', () => {
    const p = classifyGapSources(['collector_session_boundary']);
    assert.equal(p.kind, 'collector');
    assert.equal(p.label, 'Collector offline');
  });

  // Regression: runtime heartbeat-stale gaps were previously mislabeled as
  // Controller gap because only collector_session_boundary was whitelisted.
  it('classifies collector_runtime_liveness as Collector offline', () => {
    const p = classifyGapSources(['collector_runtime_liveness']);
    assert.equal(p.kind, 'collector');
    assert.equal(p.label, 'Collector offline');
  });

  it('classifies any other collector-side source as Collector offline', () => {
    const p = classifyGapSources(['collector_heartbeat_stale']);
    assert.equal(p.kind, 'collector');
  });

  it('reports mixed provenance when a merged gap spans controller and collector sources', () => {
    const p = classifyGapSources(['controller_stream', 'collector_session_boundary']);
    assert.equal(p.kind, 'mixed');
    assert.match(p.label, /mixed/i);
  });

  it('treats empty or missing sources as collector-side (never controller)', () => {
    assert.equal(classifyGapSources([]).kind, 'collector');
    assert.equal(classifyGapSources(null).kind, 'collector');
    assert.equal(classifyGapSources(undefined).kind, 'collector');
  });

  it('ignores null, undefined and empty entries in the source list', () => {
    assert.equal(classifyGapSources([null, undefined, '', 'controller_stream']).kind, 'controller');
    assert.equal(classifyGapSources([null, '', 'collector_runtime_liveness']).kind, 'collector');
  });
});
