import React from 'react';
import { QueryApiClient } from '../api/client';
import { useMetaQuery, useSummaryQuery } from '../api/queries';
import { SurfaceId, useAppFilters } from '../state/AppFilters';
import { overallTrust } from '../domain/integrity';
import { NavRail } from './NavRail';
import { TopBar } from './TopBar';
import { OverviewSurface } from '../surfaces/OverviewSurface';
import { AttributionSurface } from '../surfaces/AttributionSurface';
import { RulesSurface } from '../surfaces/RulesSurface';
import { HistorySurface } from '../surfaces/HistorySurface';
import { CoverageSurface } from '../surfaces/CoverageSurface';
import { SystemSurface } from '../surfaces/SystemSurface';

/**
 * Desktop window composition.
 *
 * A fixed left rail plus a fixed command bar, with one scrolling content column. The rail and
 * bar are persistent because the route scope and time window are global lenses — if they moved
 * between surfaces, switching tabs could silently change what a number means.
 */

interface Props {
  client: QueryApiClient | null;
  isTauri: boolean;
  sessionError: string | null;
}

export const Shell: React.FC<Props> = ({ client, isTauri, sessionError }) => {
  const { surface, setSurface, timeWindow, routeScope, liveMode } = useAppFilters();

  // The command bar needs the trust verdict and a liveness signal. These use the same query
  // keys as the surfaces, so React Query serves them from one shared cache entry rather than
  // issuing a second round of requests.
  const metaQuery = useMetaQuery(client, liveMode ? 15_000 : false);
  const summaryQuery = useSummaryQuery(client, timeWindow, routeScope, liveMode ? 15_000 : false);

  const trust = overallTrust(summaryQuery.data, summaryQuery.data?.freshness ?? metaQuery.data?.freshness);

  const connectionState: 'connecting' | 'ready' | 'failed' =
    sessionError || metaQuery.isError ? 'failed' : client && metaQuery.data ? 'ready' : 'connecting';

  const isFetching = summaryQuery.isFetching || metaQuery.isFetching;

  const renderSurface = (id: SurfaceId) => {
    switch (id) {
      case 'overview':
        return <OverviewSurface client={client} />;
      case 'attribution':
        return <AttributionSurface client={client} />;
      case 'rules':
        return <RulesSurface client={client} />;
      case 'history':
        return <HistorySurface client={client} />;
      case 'coverage':
        return <CoverageSurface client={client} />;
      case 'system':
        return <SystemSurface client={client} isTauri={isTauri} sessionError={sessionError} />;
    }
  };

  return (
    <div className="pl-app">
      <NavRail active={surface} onSelect={setSurface} />
      <div className="pl-main">
        <TopBar trust={trust} isFetching={isFetching} connectionState={connectionState} />
        <main className="pl-content" key={surface}>
          {renderSurface(surface)}
        </main>
      </div>
    </div>
  );
};
