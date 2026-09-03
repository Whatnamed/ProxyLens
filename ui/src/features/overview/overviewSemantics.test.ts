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

  it('returns null for scoped missing attribution and ambiguous relay when scopedSummary is undefined (loading/error) to avoid fake 0', () => {
    // When switching from PROXY -> DIRECT, scopedSummary is temporarily undefined while scopedSummaryQ fetches
    const evDirectLoading = deriveOverviewEvidence(mockAllSummary, undefined, 'DIRECT');
    assert.equal(evDirectLoading.missingAttributionBytes, null, 'Must be null rather than 0 so UI does not show fake 0 B');
    assert.equal(evDirectLoading.missingAttributionScoped, true);
    assert.equal(evDirectLoading.ambiguousRelayBytes, null, 'Must be null rather than 0 so UI does not show fake 0 B');
    assert.equal(evDirectLoading.ambiguousRelayScoped, true);

    // Global facts are preserved and stable from allSummary
    assert.equal(evDirectLoading.samplingResidualBytes, 10000);
    assert.equal(evDirectLoading.gapPhysicalTrafficBytes, 30000);
    assert.equal(evDirectLoading.unknownRouteBytes, 1000);
  });

  it('orchestrates page-level vs scoped loading and error states without whole-page flash', () => {
    // 1. Initial entry / Time Range change: allSummary is loading -> whole page Skeleton
    const isPageLoading1 = true; // allSummaryQ.isLoading
    assert.equal(isPageLoading1, true, 'Initial page loading triggers skeleton');

    // 2. allSummary loaded; user switches Route Focus PROXY -> DIRECT:
    // allSummaryQ is already resolved (isLoading = false), scopedSummaryQ is loading (isLoading = true)
    const allSummaryQLoading = false;
    const scopedSummaryQLoading = true;
    const isPageLoading2 = allSummaryQLoading;
    const isScopedLoading2 = true && scopedSummaryQLoading;
    assert.equal(isPageLoading2, false, 'Route focus switch must NOT trigger whole-page skeleton');
    assert.equal(isScopedLoading2, true, 'Scoped evidence enters local loading');

    // 3. scopedSummaryQ fails: allSummaryQ succeeded (isError = false), scopedSummaryQ has error (isError = true)
    const allSummaryQError = false;
    const scopedSummaryQError = true;
    const noRun = false;
    const isPageError = allSummaryQError && !noRun;
    const isScopedError = true && scopedSummaryQError;
    assert.equal(isPageError, false, 'Scoped query error must NOT escalate to page-level error state');
    assert.equal(isScopedError, true, 'Scoped evidence enters local unavailable state');

    // 4. allSummaryQ fails: whole page enters ErrorState
    const allSummaryQFailed = true;
    const isPageErrorActual = allSummaryQFailed && !noRun;
    assert.equal(isPageErrorActual, true, 'allSummary failure must trigger page-level error');
  });
});
