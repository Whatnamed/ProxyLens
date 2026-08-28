import { useQuery, UseQueryResult } from '@tanstack/react-query';
import { QueryApiClient } from './client';
import {
  MetaResponse,
  UsageSummary,
  TopDimensionResponse,
  TopRulesResponse,
  CoverageSummary,
  ConnectionsListResponse,
  ConnectionDetailResponse,
  ConnectionTrafficResponse,
} from './types';
import type { RouteScope } from '../domain/route';

/**
 * Every analytics endpoint in the v1 contract accepts `from` / `to` / `route`.
 * The previous hooks ignored all three and therefore always asked the server for
 * "everything, all routes", which is not a view a user can reason about.
 * These hooks thread the real window and route scope through so the UI answers
 * "what happened in THIS window through THIS route".
 */

export interface WindowFilter {
  from: string;
  to: string;
}

/** Route scope '' means "all routes" in the Query API's own vocabulary. */
function apiRoute(route: RouteScope): string | undefined {
  return route === 'ALL' ? undefined : route;
}

function useWindowQuery<T>(
  client: QueryApiClient | null,
  key: (string | number | undefined)[],
  fetcher: () => Promise<T>,
  options: { enabled?: boolean; refetchInterval?: number | false } = {}
): UseQueryResult<T, Error> {
  return useQuery<T, Error>({
    queryKey: key,
    queryFn: fetcher,
    enabled: !!client && (options.enabled ?? true),
    refetchInterval: options.refetchInterval ?? false,
    // Audited figures are a snapshot of a completed accounting run; re-fetching on every
    // focus event would thrash a read-only SQLite file for no new information.
    refetchOnWindowFocus: false,
    retry: 1,
    staleTime: 10_000,
    // The window slides as time passes, so query keys change. Without this the whole
    // surface would collapse back to skeletons on every tick; keeping the previous
    // result visible makes a window change read as a refresh, not a reset.
    placeholderData: (previous) => previous,
  });
}

export function useMetaQuery(
  client: QueryApiClient | null,
  refetchInterval: number | false = 10_000
): UseQueryResult<MetaResponse, Error> {
  return useQuery<MetaResponse, Error>({
    queryKey: ['meta'],
    queryFn: () => client!.getMeta(),
    enabled: !!client,
    refetchInterval,
    refetchOnWindowFocus: false,
    retry: 1,
    staleTime: 5_000,
  });
}

export function useSummaryQuery(
  client: QueryApiClient | null,
  window: WindowFilter | null,
  route: RouteScope,
  refetchInterval: number | false = false
): UseQueryResult<UsageSummary, Error> {
  return useWindowQuery<UsageSummary>(
    client,
    ['summary', window?.from, window?.to, route],
    () => client!.getSummary(window!.from, window!.to, apiRoute(route)),
    { enabled: !!window, refetchInterval }
  );
}

export function useTopProcessesQuery(
  client: QueryApiClient | null,
  window: WindowFilter | null,
  route: RouteScope,
  limit = 20
): UseQueryResult<TopDimensionResponse, Error> {
  return useWindowQuery<TopDimensionResponse>(
    client,
    ['topProcesses', window?.from, window?.to, route, limit],
    () => client!.getTopProcesses(window!.from, window!.to, apiRoute(route), limit),
    { enabled: !!window }
  );
}

export function useTopHostsQuery(
  client: QueryApiClient | null,
  window: WindowFilter | null,
  route: RouteScope,
  limit = 20
): UseQueryResult<TopDimensionResponse, Error> {
  return useWindowQuery<TopDimensionResponse>(
    client,
    ['topHosts', window?.from, window?.to, route, limit],
    () => client!.getTopHosts(window!.from, window!.to, apiRoute(route), limit),
    { enabled: !!window }
  );
}

export function useTopFinalProxiesQuery(
  client: QueryApiClient | null,
  window: WindowFilter | null,
  route: RouteScope,
  limit = 20
): UseQueryResult<TopDimensionResponse, Error> {
  return useWindowQuery<TopDimensionResponse>(
    client,
    ['topFinalProxies', window?.from, window?.to, route, limit],
    () => client!.getTopFinalProxies(window!.from, window!.to, apiRoute(route), limit),
    { enabled: !!window }
  );
}

export function useProtocolsQuery(
  client: QueryApiClient | null,
  window: WindowFilter | null,
  route: RouteScope,
  limit = 20
): UseQueryResult<TopDimensionResponse, Error> {
  return useWindowQuery<TopDimensionResponse>(
    client,
    ['protocols', window?.from, window?.to, route, limit],
    () => client!.getProtocols(window!.from, window!.to, apiRoute(route), limit),
    { enabled: !!window }
  );
}

export function useTopRulesQuery(
  client: QueryApiClient | null,
  window: WindowFilter | null,
  route: RouteScope,
  limit = 40,
  refetchInterval: number | false = false
): UseQueryResult<TopRulesResponse, Error> {
  return useWindowQuery<TopRulesResponse>(
    client,
    ['topRules', window?.from, window?.to, route, limit],
    () => client!.getTopRules(window!.from, window!.to, apiRoute(route), limit),
    { enabled: !!window, refetchInterval }
  );
}

export function useCoverageQuery(
  client: QueryApiClient | null,
  window: WindowFilter | null
): UseQueryResult<CoverageSummary, Error> {
  return useWindowQuery<CoverageSummary>(
    client,
    ['coverage', window?.from, window?.to],
    () => client!.getCoverage(window!.from, window!.to),
    { enabled: !!window }
  );
}

export interface ConnectionQueryParams {
  process?: string;
  host?: string;
  destinationIp?: string;
  network?: string;
  limit?: number;
  offset?: number;
}

export function useConnectionsQuery(
  client: QueryApiClient | null,
  window: WindowFilter | null,
  route: RouteScope,
  params: ConnectionQueryParams = {},
  refetchInterval: number | false = false
): UseQueryResult<ConnectionsListResponse, Error> {
  const { process, host, destinationIp, network, limit = 50, offset = 0 } = params;
  return useWindowQuery<ConnectionsListResponse>(
    client,
    ['connections', window?.from, window?.to, route, process, host, destinationIp, network, limit, offset],
    () =>
      client!.getConnections({
        from: window!.from,
        to: window!.to,
        route: apiRoute(route),
        process,
        host,
        destinationIp,
        network,
        limit,
        offset,
      }),
    { enabled: !!window, refetchInterval }
  );
}

export function useConnectionDetailQuery(
  client: QueryApiClient | null,
  identity: { sessionId: string; epochId: number; connectionId: string } | null
): UseQueryResult<ConnectionDetailResponse, Error> {
  return useWindowQuery<ConnectionDetailResponse>(
    client,
    ['connectionDetail', identity?.sessionId, identity?.epochId, identity?.connectionId],
    () => client!.getConnectionDetail(identity!.sessionId, identity!.epochId, identity!.connectionId),
    { enabled: !!identity }
  );
}

export function useConnectionTrafficQuery(
  client: QueryApiClient | null,
  identity: { sessionId: string; epochId: number; connectionId: string } | null
): UseQueryResult<ConnectionTrafficResponse, Error> {
  return useWindowQuery<ConnectionTrafficResponse>(
    client,
    ['connectionTraffic', identity?.sessionId, identity?.epochId, identity?.connectionId],
    () => client!.getConnectionTraffic(identity!.sessionId, identity!.epochId, identity!.connectionId),
    { enabled: !!identity }
  );
}
