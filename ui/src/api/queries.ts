import { useQueries, useQuery, UseQueryResult } from '@tanstack/react-query';
import { QueryApiClient } from './client';
import { ResolvedRange, HistoryFilters, PageSize, ConnectionRef } from '../state/AuditContext';
import { RouteKey } from '../lib/semantics';
import {
  ConnectionsListResponse,
  ConnectionDetailResponse,
  ConnectionTrafficResponse,
  CoverageSummary,
  MetaResponse,
  TopDimensionResponse,
  TopRulesResponse,
  UsageSummary
} from './types';

/**
 * All queries are keyed on the resolved `[from, to)` range and the manual
 * refresh nonce. Nothing here fabricates totals, sorting or pagination — the
 * Go Query API stays authoritative for ordering and for `hasMore`.
 */

const TOP_LIMIT = 12;

export function useMeta(client: QueryApiClient | null) {
  return useQuery<MetaResponse>({
    queryKey: ['meta'],
    queryFn: () => client!.getMeta(),
    enabled: !!client,
    refetchInterval: 15_000,
    retry: 1
  });
}

/** Route totals for PROXY / DIRECT / REJECT. Must be unfiltered by route. */
export function useTrafficSummary(client: QueryApiClient | null, range: ResolvedRange, nonce: number) {
  return useQuery<UsageSummary>({
    queryKey: ['summary-all', range.from, range.to, nonce],
    queryFn: () => client!.getSummary(range.from, range.to, undefined),
    enabled: !!client,
    retry: 1
  });
}

/**
 * Evidence-quality figures that are route-scoped by the backend
 * (unique observed, missing attribution, ambiguous relay).
 */
export function useRouteSummary(
  client: QueryApiClient | null,
  range: ResolvedRange,
  route: RouteKey,
  nonce: number
) {
  return useQuery<UsageSummary>({
    queryKey: ['summary-route', range.from, range.to, route, nonce],
    queryFn: () => client!.getSummary(range.from, range.to, route),
    enabled: !!client,
    retry: 1
  });
}

export interface TopQueries {
  processes: UseQueryResult<TopDimensionResponse>;
  hosts: UseQueryResult<TopDimensionResponse>;
  rules: UseQueryResult<TopRulesResponse>;
  finalProxies: UseQueryResult<TopDimensionResponse>;
  protocols: UseQueryResult<TopDimensionResponse>;
}

export function useTopDimensions(
  client: QueryApiClient | null,
  range: ResolvedRange,
  route: RouteKey,
  nonce: number
): TopQueries & { isLoading: boolean; hasAnyError: boolean } {
  const results = useQueries({
    queries: [
      {
        queryKey: ['top-processes', range.from, range.to, route, nonce],
        queryFn: () => client!.getTopProcesses(range.from, range.to, route, TOP_LIMIT),
        enabled: !!client,
        retry: 1
      },
      {
        queryKey: ['top-hosts', range.from, range.to, route, nonce],
        queryFn: () => client!.getTopHosts(range.from, range.to, route, TOP_LIMIT),
        enabled: !!client,
        retry: 1
      },
      {
        queryKey: ['top-rules', range.from, range.to, route, nonce],
        queryFn: () => client!.getTopRules(range.from, range.to, route, TOP_LIMIT),
        enabled: !!client,
        retry: 1
      },
      {
        queryKey: ['top-final-proxies', range.from, range.to, route, nonce],
        queryFn: () => client!.getTopFinalProxies(range.from, range.to, route, TOP_LIMIT),
        enabled: !!client,
        retry: 1
      },
      {
        queryKey: ['top-protocols', range.from, range.to, route, nonce],
        queryFn: () => client!.getProtocols(range.from, range.to, route, TOP_LIMIT),
        enabled: !!client,
        retry: 1
      }
    ]
  });

  const [processes, hosts, rules, finalProxies, protocols] = results as [
    UseQueryResult<TopDimensionResponse>,
    UseQueryResult<TopDimensionResponse>,
    UseQueryResult<TopRulesResponse>,
    UseQueryResult<TopDimensionResponse>,
    UseQueryResult<TopDimensionResponse>
  ];

  return {
    processes,
    hosts,
    rules,
    finalProxies,
    protocols,
    isLoading: results.some((r) => r.isLoading),
    hasAnyError: results.some((r) => r.isError)
  };
}

export function useCoverage(client: QueryApiClient | null, range: ResolvedRange, nonce: number) {
  return useQuery<CoverageSummary>({
    queryKey: ['coverage', range.from, range.to, nonce],
    queryFn: () => client!.getCoverage(range.from, range.to),
    enabled: !!client,
    retry: 1
  });
}

/**
 * One page of authoritative history: newest first, `limit` + `offset`, no
 * total count. `placeholderData` keeps the previous page visible while the
 * next one loads so the investigator never loses their place.
 */
export function useHistoryPage(
  client: QueryApiClient | null,
  range: ResolvedRange,
  route: RouteKey,
  filters: HistoryFilters,
  page: number,
  pageSize: PageSize,
  nonce: number
) {
  const offset = page * pageSize;
  return useQuery<ConnectionsListResponse>({
    queryKey: [
      'connections',
      range.from,
      range.to,
      route,
      filters.process ?? '',
      filters.host ?? '',
      filters.destinationIp ?? '',
      filters.network ?? '',
      pageSize,
      offset,
      nonce
    ],
    queryFn: () =>
      client!.getConnections({
        from: range.from,
        to: range.to,
        route,
        process: filters.process,
        host: filters.host,
        destinationIp: filters.destinationIp,
        network: filters.network,
        limit: pageSize,
        offset
      }),
    enabled: !!client,
    retry: 1,
    // Keep the previous page rendered while the next one loads so the
    // investigator never loses their place in the evidence.
    placeholderData: (prev: ConnectionsListResponse | undefined) => prev
  });
}

export function useConnectionDetail(client: QueryApiClient | null, ref: ConnectionRef | null) {
  return useQuery<ConnectionDetailResponse>({
    queryKey: ['connection', ref?.sessionId, ref?.epochId, ref?.connectionId],
    queryFn: () => client!.getConnectionDetail(ref!.sessionId, ref!.epochId, ref!.connectionId),
    enabled: !!client && !!ref,
    retry: 1
  });
}

/**
 * Raw traffic frames are advanced evidence: only fetched when the user opens
 * that section of the Inspector.
 */
export function useConnectionTraffic(
  client: QueryApiClient | null,
  ref: ConnectionRef | null,
  enabled: boolean
) {
  return useQuery<ConnectionTrafficResponse>({
    queryKey: ['connection-traffic', ref?.sessionId, ref?.epochId, ref?.connectionId],
    queryFn: () => client!.getConnectionTraffic(ref!.sessionId, ref!.epochId, ref!.connectionId),
    enabled: !!client && !!ref && enabled,
    retry: 1
  });
}
