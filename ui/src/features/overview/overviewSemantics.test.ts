import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { UsageSummary } from '../../api/types.js';
import { deriveOverviewTotals, deriveOverviewEvidence } from './overviewSemantics.js';

const mockAllSummary: UsageSummary = {
  rawObservedUpload: 300000,
  rawObservedDownload: 1500000,
  uniqueObservedUpload: 250000,
  uniqueObservedDownload: 1300000,
  proxyUpload: 100000,
  proxyDownload: 500000,
  directUpload: 80000,
  directDownload: 600000,
  rejectUpload: 1000,
  rejectDownload: 2000,
  unknownRouteUpload: 500,
  unknownRouteDownload: 500,
  missingAttributionUpload: 20000,
  missingAttributionDownload: 40000,
  ambiguousRelayUpload: 5000,
  ambiguousRelayDownload: 10000,
  samplingResidualUpload: 3000,
  samplingResidualDownload: 7000,
  controllerGapPhysicalUpload: 12000,
  controllerGapPhysicalDownload: 18000,
  accountingVersion: 'v1.0.0',
};

const mockProxyScopedSummary: UsageSummary = {
  rawObservedUpload: 120000,
  rawObservedDownload: 550000,
  uniqueObservedUpload: 100000,
  uniqueObservedDownload: 500000,
  proxyUpload: 100000,
  proxyDownload: 500000,
  directUpload: 0,
  directDownload: 0,
  rejectUpload: 0,
  rejectDownload: 0,
  unknownRouteUpload: 0,
  unknownRouteDownload: 0,
  missingAttributionUpload: 8000,
  missingAttributionDownload: 12000,
  ambiguousRelayUpload: 2000,
  ambiguousRelayDownload: 3000,
  samplingResidualUpload: 3000,
  samplingResidualDownload: 7000,
  controllerGapPhysicalUpload: 12000,
  controllerGapPhysicalDownload: 18000,
  accountingVersion: 'v1.0.0',
};

describe('Overview Semantics: deriveOverviewTotals', () => {
  it('preserves all-route composition totals regardless of Route Focus', () => {
    const totalsAll = deriveOverviewTotals(mockAllSummary, 'ALL');
    assert.equal(totalsAll.proxyTotal, 600000);
    assert.equal(totalsAll.directTotal, 680000);
    assert.equal(totalsAll.rejectTotal, 3000);
    assert.equal(totalsAll.inScopeTotal, null);

    const totalsProxy = deriveOverviewTotals(mockAllSummary, 'PROXY');
    assert.equal(totalsProxy.proxyTotal, 600000);
    // DIRECT and REJECT remain non-zero and accurate to the overall time window:
    assert.equal(totalsProxy.directTotal, 680000);
    assert.equal(totalsProxy.rejectTotal, 3000);
    assert.equal(totalsProxy.inScopeTotal, 600000);

    const totalsDirect = deriveOverviewTotals(mockAllSummary, 'DIRECT');
    assert.equal(totalsDirect.directTotal, 680000);
    assert.equal(totalsDirect.inScopeTotal, 680000);
  });
});

describe('Overview Semantics: deriveOverviewEvidence', () => {
  it('provides global evidence metrics regardless of route focus', () => {
    const evProxy = deriveOverviewEvidence(mockAllSummary, mockProxyScopedSummary, 'PROXY');
    assert.equal(evProxy.samplingResidualBytes, 10000);
    assert.equal(evProxy.gapPhysicalTrafficBytes, 30000);
    assert.equal(evProxy.unknownRouteBytes, 1000);
    assert.equal(evProxy.showUnknownRoute, true);

    const evAll = deriveOverviewEvidence(mockAllSummary, undefined, 'ALL');
    assert.equal(evAll.samplingResidualBytes, 10000);
    assert.equal(evAll.gapPhysicalTrafficBytes, 30000);
    assert.equal(evAll.unknownRouteBytes, 1000);
  });

  it('scopes missing attribution and ambiguous relay when Route Focus is active', () => {
    const evProxy = deriveOverviewEvidence(mockAllSummary, mockProxyScopedSummary, 'PROXY');
    assert.equal(evProxy.missingAttributionBytes, 20000); // 8000 + 12000
    assert.equal(evProxy.missingAttributionScoped, true);
    assert.equal(evProxy.ambiguousRelayBytes, 5000); // 2000 + 3000
    assert.equal(evProxy.ambiguousRelayScoped, true);

    const evAll = deriveOverviewEvidence(mockAllSummary, undefined, 'ALL');
    assert.equal(evAll.missingAttributionBytes, 60000); // 20000 + 40000
    assert.equal(evAll.missingAttributionScoped, false);
    assert.equal(evAll.ambiguousRelayBytes, 15000); // 5000 + 10000
    assert.equal(evAll.ambiguousRelayScoped, false);
  });

  it('hides unknown route when zero', () => {
    const summaryNoUnknown = { ...mockAllSummary, unknownRouteUpload: 0, unknownRouteDownload: 0 };
    const ev = deriveOverviewEvidence(summaryNoUnknown, undefined, 'ALL');
    assert.equal(ev.unknownRouteBytes, 0);
    assert.equal(ev.showUnknownRoute, false);
  });
});
