import { describe, it } from 'node:test';
import assert from 'node:assert/strict';

import { normalizeRoute, routeBytes, routeComposition } from './route.ts';
import { causalChain, egressNode, intermediateHops, topPolicyGroup } from './chains.ts';
import {
  attributionState,
  coverageState,
  freshnessState,
  overallTrust,
  residualState,
} from './integrity.ts';
import { estimateShare, dominantTone, ruleAuditFlags } from './rules.ts';
import type { CoverageSummary, UsageSummary } from '../api/types';

/**
 * These tests lock down the semantics the UI promises the user, not the rendering.
 * If a refactor changes how a missing hop, a gap or a route is interpreted, it should
 * fail here first.
 */

describe('route semantics', () => {
  it('normalises backend route strings into the closed UI set', () => {
    assert.equal(normalizeRoute('PROXY'), 'PROXY');
    assert.equal(normalizeRoute('direct'), 'DIRECT');
    assert.equal(normalizeRoute('Reject'), 'REJECT');
    // Anything unrecognised must surface as UNKNOWN, never silently as PROXY.
    assert.equal(normalizeRoute(''), 'UNKNOWN');
    assert.equal(normalizeRoute(undefined), 'UNKNOWN');
    assert.equal(normalizeRoute('SOMETHING_ELSE'), 'UNKNOWN');
  });

  it('keeps DIRECT separated from PROXY in the byte split', () => {
    const summary: Partial<UsageSummary> = {
      proxyUpload: 100,
      proxyDownload: 900,
      directUpload: 5000,
      directDownload: 5000,
    };
    assert.equal(routeBytes(summary, 'PROXY').total, 1000);
    assert.equal(routeBytes(summary, 'DIRECT').total, 10000);
    // The whole point of scenario C: DIRECT must never leak into the proxy figure.
    assert.notEqual(routeBytes(summary, 'PROXY').total, 11000);
  });

  it('emits only non-zero route segments and never drops UNKNOWN', () => {
    const summary: Partial<UsageSummary> = {
      proxyUpload: 1,
      proxyDownload: 1,
      unknownRouteUpload: 5,
      unknownRouteDownload: 5,
    };
    const segs = routeComposition(summary);
    assert.deepEqual(
      segs.map((s) => s.route),
      ['PROXY', 'UNKNOWN']
    );
  });
});

describe('chain hop ordering', () => {
  // Mihomo delivers chains[0] = egress, chains[last] = rule target group.
  const chains = ['Node-HK-01', 'AutoSelect', 'ProxyGroup'];

  it('reads egress from index 0 and the rule target from the last index', () => {
    assert.equal(egressNode(chains), 'Node-HK-01');
    assert.equal(topPolicyGroup(chains), 'ProxyGroup');
  });

  it('renders in causal order (rule target -> selectors -> egress)', () => {
    assert.deepEqual(causalChain(chains), ['ProxyGroup', 'AutoSelect', 'Node-HK-01']);
  });

  it('handles short and empty chains without inventing hops', () => {
    assert.deepEqual(causalChain(['DIRECT']), ['DIRECT']);
    assert.deepEqual(intermediateHops(['DIRECT']), []);
    assert.deepEqual(causalChain([]), []);
    assert.equal(egressNode([]), null);
    assert.deepEqual(causalChain(undefined), []);
  });

  it('extracts intermediate hops only when there are three or more', () => {
    assert.deepEqual(intermediateHops(chains), ['AutoSelect']);
    assert.deepEqual(intermediateHops(['Node', 'Group']), []);
  });
});

describe('integrity layer', () => {
  it('treats a null coverage ratio as uncomputable rather than 0%', () => {
    const coverage: Partial<CoverageSummary> = {
      coverageRatio: undefined,
      outsideKnownScopeMs: 3600_000,
      coveredDurationMs: 0,
      uncoveredDurationMs: 0,
    };
    const state = coverageState(coverage as CoverageSummary);
    assert.equal(state.ratio, null);
    assert.equal(state.level, 'unavailable');
    assert.equal(state.outsideKnownScope, true);
  });

  it('grades coverage by ratio', () => {
    assert.equal(coverageState({ coverageRatio: 1 } as CoverageSummary).level, 'healthy');
    assert.equal(coverageState({ coverageRatio: 0.9 } as CoverageSummary).level, 'partial');
    assert.equal(coverageState({ coverageRatio: 0.2 } as CoverageSummary).level, 'degraded');
  });

  it('reports attribution completeness against raw observed bytes', () => {
    const summary: Partial<UsageSummary> = {
      rawObservedUpload: 500,
      rawObservedDownload: 500,
      missingAttributionUpload: 100,
      missingAttributionDownload: 100,
    };
    const state = attributionState(summary as UsageSummary);
    assert.equal(state.ratio, 0.8);
    assert.equal(state.missingBytes, 200);
    assert.equal(state.level, 'partial');
  });

  it('flags a large sampling residual as degraded rather than hiding it', () => {
    const summary: Partial<UsageSummary> = {
      uniqueObservedUpload: 100,
      uniqueObservedDownload: 100,
      samplingResidualUpload: 200,
      samplingResidualDownload: 200,
    };
    const state = residualState(summary as UsageSummary);
    assert.equal(state.level, 'degraded');
    assert.equal(state.residualBytes, 400);
  });

  it('maps accounting freshness lag onto a trust level', () => {
    assert.equal(freshnessState({ isFresh: true, lagEvents: 0 } as never).level, 'healthy');
    assert.equal(freshnessState({ isFresh: false, lagEvents: 42 } as never).level, 'partial');
    assert.equal(freshnessState(null).level, 'unavailable');
  });

  it('takes the worst actionable signal as the headline verdict', () => {
    const summary: Partial<UsageSummary> = {
      rawObservedUpload: 100,
      rawObservedDownload: 100,
      coverage: { coverageRatio: 0.4 } as CoverageSummary,
    };
    const verdict = overallTrust(summary as UsageSummary, { isFresh: true, lagEvents: 0 } as never);
    assert.equal(verdict.level, 'degraded');
    assert.match(verdict.detail, /监控状态/);
  });

  it('reports a clean window as complete', () => {
    const summary: Partial<UsageSummary> = {
      rawObservedUpload: 100,
      rawObservedDownload: 100,
      coverage: { coverageRatio: 1 } as CoverageSummary,
    };
    const verdict = overallTrust(summary as UsageSummary, { isFresh: true, lagEvents: 0 } as never);
    assert.equal(verdict.level, 'healthy');
  });
});

describe('rule audit flags', () => {
  it('flags MATCH fallback traffic', () => {
    const flags = ruleAuditFlags({ rule: 'Match', rulePayload: '' } as never);
    assert.ok(flags.some((f) => f.id === 'match-fallback'));
    assert.equal(dominantTone(flags), 'warn');
  });

  it('flags a broad NETWORK,udp rule', () => {
    const flags = ruleAuditFlags({ rule: 'Network', rulePayload: 'udp' } as never);
    assert.ok(flags.some((f) => f.id === 'broad-udp'));
  });

  it('flags a row with no rule as danger, and explains why', () => {
    const flags = ruleAuditFlags({ rule: '', rulePayload: '' } as never);
    const missing = flags.find((f) => f.id === 'missing-rule');
    assert.ok(missing);
    assert.equal(missing.tone, 'danger');
    assert.ok(missing.reason.length > 0, 'every flag must carry a reason');
  });

  it('does not flag a normal specific rule', () => {
    const flags = ruleAuditFlags({ rule: 'DomainSuffix', rulePayload: 'github.com' } as never);
    assert.equal(flags.length, 0);
    assert.equal(dominantTone(flags), null);
  });

  it('computes the interval-derived share of a row', () => {
    const share = estimateShare({ totalBytes: 1000, estimatedUploadBytes: 100, estimatedDownloadBytes: 150 });
    assert.equal(share, 0.25);
    // A row with no total must not divide by zero.
    assert.equal(estimateShare({ totalBytes: 0, estimatedUploadBytes: 5 }), 0);
  });
});
