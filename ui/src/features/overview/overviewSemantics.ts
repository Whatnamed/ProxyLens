import { UsageSummary } from '../../api/types';
import { RouteFocus } from '../../state/AuditContext';

export interface OverviewTotals {
  proxyUp: number;
  proxyDown: number;
  proxyTotal: number;

  directUp: number;
  directDown: number;
  directTotal: number;

  rejectUp: number;
  rejectDown: number;
  rejectTotal: number;

  inScopeTotal: number | null;
}

export interface OverviewEvidenceMetrics {
  missingAttributionBytes: number;
  missingAttributionScoped: boolean;

  ambiguousRelayBytes: number;
  ambiguousRelayScoped: boolean;

  samplingResidualBytes: number;
  gapPhysicalTrafficBytes: number;

  unknownRouteBytes: number;
  showUnknownRoute: boolean;
}

export function deriveOverviewTotals(
  allSummary: UsageSummary | undefined,
  routeFocus: RouteFocus
): OverviewTotals {
  const proxyUp = allSummary?.proxyUpload ?? 0;
  const proxyDown = allSummary?.proxyDownload ?? 0;
  const proxyTotal = proxyUp + proxyDown;

  const directUp = allSummary?.directUpload ?? 0;
  const directDown = allSummary?.directDownload ?? 0;
  const directTotal = directUp + directDown;

  const rejectUp = allSummary?.rejectUpload ?? 0;
  const rejectDown = allSummary?.rejectDownload ?? 0;
  const rejectTotal = rejectUp + rejectDown;

  let inScopeTotal: number | null = null;
  if (routeFocus === 'PROXY') {
    inScopeTotal = proxyTotal;
  } else if (routeFocus === 'DIRECT') {
    inScopeTotal = directTotal;
  } else if (routeFocus === 'REJECT') {
    inScopeTotal = rejectTotal;
  }

  return {
    proxyUp,
    proxyDown,
    proxyTotal,
    directUp,
    directDown,
    directTotal,
    rejectUp,
    rejectDown,
    rejectTotal,
    inScopeTotal,
  };
}

export function deriveOverviewEvidence(
  allSummary: UsageSummary | undefined,
  scopedSummary: UsageSummary | undefined,
  routeFocus: RouteFocus
): OverviewEvidenceMetrics {
  const isRouteScoped = routeFocus !== 'ALL';
  const effectiveScoped = isRouteScoped ? scopedSummary : allSummary;

  const missingAttributionBytes =
    (effectiveScoped?.missingAttributionUpload ?? 0) +
    (effectiveScoped?.missingAttributionDownload ?? 0);

  const ambiguousRelayBytes =
    (effectiveScoped?.ambiguousRelayUpload ?? 0) +
    (effectiveScoped?.ambiguousRelayDownload ?? 0);

  const samplingResidualBytes =
    (allSummary?.samplingResidualUpload ?? 0) +
    (allSummary?.samplingResidualDownload ?? 0);

  const gapPhysicalTrafficBytes =
    (allSummary?.controllerGapPhysicalUpload ?? 0) +
    (allSummary?.controllerGapPhysicalDownload ?? 0);

  const unknownRouteBytes =
    (allSummary?.unknownRouteUpload ?? 0) +
    (allSummary?.unknownRouteDownload ?? 0);

  return {
    missingAttributionBytes,
    missingAttributionScoped: isRouteScoped,
    ambiguousRelayBytes,
    ambiguousRelayScoped: isRouteScoped,
    samplingResidualBytes,
    gapPhysicalTrafficBytes,
    unknownRouteBytes,
    showUnknownRoute: unknownRouteBytes > 0,
  };
}
