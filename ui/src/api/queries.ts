import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { QueryApiClient } from './client';
import { ConnectionKey } from '../state/AuditContext';
import { HistoryFilters } from '../state/AuditContext';

export function useMetaQuery(client: QueryApiClient | null) {
  return useQuery({
    queryKey: ['meta'],
    queryFn: () => client!.getMeta(),
    enabled: !!client,
    refetchInterval: 3000,
  });
}

/**
 * Preserves previous analytical query data ONLY during live time boundary advancements (to >= prevTo),
 * within the exact same semantic scope (identical from, identical route).
 *
 * If the user switches Route Focus (e.g. PROXY -> DIRECT) or changes the Time Window (e.g. Today -> 30d),
 * this returns `undefined` so the UI enters an explicit loading/transition state and avoids
 * showing stale evidence under a newly active scope.
 */
export function keepLiveTickOnly<TData>(
  currentFrom: string,
  currentTo: string,
  currentScope?: unknown
): (previousData: TData | undefined, previousQuery?: { queryKey: readonly unknown[] }) => TData | undefined {
  return (previousData, previousQuery) => {
    if (!previousData || !previousQuery) return undefined;
    const prevKey = previousQuery.queryKey;
    if (!Array.isArray(prevKey) || prevKey.length < 3) return undefined;
    const prevFrom = prevKey[1];
    const prevTo = prevKey[2];
    const prevScope = prevKey[3];

    // Semantic scope guards:
    // 1. Window start must be identical (no range kind or custom start change)
    if (prevFrom !== currentFrom) return undefined;
    // 2. Secondary scope (e.g. route focus) must be identical
    if (prevScope !== currentScope) return undefined;
    // 3. Window end must be monotonic (live tick moving forward or same)
    if (typeof prevTo !== 'string' || currentTo < prevTo) return undefined;

    return previousData;
  };
}

export function useSummaryQuery(
  client: QueryApiClient | null,
  from: string,
  to: string,
  route?: string,
  options?: { enabled?: boolean }
) {
  return useQuery({
    queryKey: ['summary', from, to, route],
    queryFn: () => client!.getSummary(from, to, route),
    enabled: !!client && (options?.enabled ?? true),
    refetchInterval: 10000,
    placeholderData: keepLiveTickOnly(from, to, route),
  });
}

export function useTopProcessesQuery(client: QueryApiClient | null, from: string, to: string, route: string, limit = 10) {
  return useQuery({
    queryKey: ['topProcesses', from, to, route, limit],
    queryFn: () => client!.getTopProcesses(from, to, route, limit),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to, route),
  });
}

export function useTopHostsQuery(client: QueryApiClient | null, from: string, to: string, route: string, limit = 10) {
  return useQuery({
    queryKey: ['topHosts', from, to, route, limit],
    queryFn: () => client!.getTopHosts(from, to, route, limit),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to, route),
  });
}

export function useTopRulesQuery(client: QueryApiClient | null, from: string, to: string, route: string, limit = 10) {
  return useQuery({
    queryKey: ['topRules', from, to, route, limit],
    queryFn: () => client!.getTopRules(from, to, route, limit),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to, route),
  });
}

export function useTopFinalProxiesQuery(client: QueryApiClient | null, from: string, to: string, route: string, limit = 10) {
  return useQuery({
    queryKey: ['topFinalProxies', from, to, route, limit],
    queryFn: () => client!.getTopFinalProxies(from, to, route, limit),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to, route),
  });
}

export function useProtocolsQuery(client: QueryApiClient | null, from: string, to: string, route: string, limit = 10) {
  return useQuery({
    queryKey: ['protocols', from, to, route, limit],
    queryFn: () => client!.getProtocols(from, to, route, limit),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to, route),
  });
}

export function useCoverageQuery(client: QueryApiClient | null, from: string, to: string) {
  return useQuery({
    queryKey: ['coverage', from, to],
    queryFn: () => client!.getCoverage(from, to),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to),
  });
}

export function useConnectionsQuery(
  client: QueryApiClient | null,
  params: {
    from: string;
    to: string;
    route: string;
    filters: HistoryFilters;
    limit: number;
    offset: number;
  }
) {
  const { from, to, route, filters, limit, offset } = params;
  return useQuery({
    queryKey: ['connections', from, to, route, filters, limit, offset],
    queryFn: () =>
      client!.getConnections({
        from,
        to,
        route,
        process: filters.process,
        host: filters.host,
        destinationIp: filters.destinationIp,
        network: filters.network,
        limit,
        offset,
      }),
    enabled: !!client,
    placeholderData: keepPreviousData,
  });
}

export function useConnectionDetailQuery(client: QueryApiClient | null, key: ConnectionKey | null) {
  return useQuery({
    queryKey: ['connectionDetail', key?.sessionId, key?.epochId, key?.connectionId],
    queryFn: () => client!.getConnectionDetail(key!.sessionId, key!.epochId, key!.connectionId),
    enabled: !!client && !!key,
  });
}

export function useConnectionTrafficQuery(client: QueryApiClient | null, key: ConnectionKey | null, enabled: boolean) {
  return useQuery({
    queryKey: ['connectionTraffic', key?.sessionId, key?.epochId, key?.connectionId],
    queryFn: () => client!.getConnectionTraffic(key!.sessionId, key!.epochId, key!.connectionId),
    enabled: !!client && !!key && enabled,
  });
}
