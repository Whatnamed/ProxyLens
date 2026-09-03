import { useQuery } from '@tanstack/react-query';
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

export function isLiveRangeKey(key?: string): boolean {
  return key === 'today' || key === '7d' || key === '30d';
}

/**
 * Preserves previous analytical query data ONLY during live time boundary advancements (to >= prevTo),
 * within the exact same semantic scope (identical from, identical route, identical live range kind).
 *
 * If the user switches Route Focus (e.g. PROXY -> DIRECT), changes the Time Window (e.g. Today -> 30d),
 * or manually edits a Custom range, this returns `undefined` so the UI enters an explicit loading/transition
 * state and avoids showing stale evidence under a newly active scope or custom interval.
 */
export function keepLiveTickOnly<TData>(
  currentFrom: string,
  currentTo: string,
  currentScope?: unknown,
  currentRangeKey?: string
): (previousData: TData | undefined, previousQuery?: { queryKey: readonly unknown[] }) => TData | undefined {
  return (previousData, previousQuery) => {
    if (!previousData || !previousQuery) return undefined;
    const prevKey = previousQuery.queryKey;
    if (!Array.isArray(prevKey) || prevKey.length < 3) return undefined;

    // 1. Live tick is ONLY valid for rolling range kinds ('today', '7d', '30d').
    // Closed ranges (yesterday, custom intervals) MUST enter explicit loading on edit/change.
    if (!isLiveRangeKey(currentRangeKey)) return undefined;

    // 2. The range identity itself must be identical (e.g. no switching from today to 7d or custom)
    const prevRangeKey = prevKey[prevKey.length - 1];
    if (prevRangeKey !== currentRangeKey) return undefined;

    const prevFrom = prevKey[1];
    const prevTo = prevKey[2];

    // 3. Window start must be identical
    if (prevFrom !== currentFrom) return undefined;
    // 4. Secondary scope (e.g. route focus) must be identical
    if (currentScope !== undefined && prevKey[3] !== currentScope) return undefined;
    // 5. Window end must be monotonic (live tick moving forward or same)
    if (typeof prevTo !== 'string' || currentTo < prevTo) return undefined;

    return previousData;
  };
}

export function useSummaryQuery(
  client: QueryApiClient | null,
  from: string,
  to: string,
  route?: string,
  rangeKey?: string,
  options?: { enabled?: boolean }
) {
  return useQuery({
    queryKey: ['summary', from, to, route, rangeKey],
    queryFn: () => client!.getSummary(from, to, route),
    enabled: !!client && (options?.enabled ?? true),
    refetchInterval: 10000,
    placeholderData: keepLiveTickOnly(from, to, route, rangeKey),
  });
}

export function useTopProcessesQuery(
  client: QueryApiClient | null,
  from: string,
  to: string,
  route: string,
  limit = 10,
  rangeKey?: string
) {
  return useQuery({
    queryKey: ['topProcesses', from, to, route, limit, rangeKey],
    queryFn: () => client!.getTopProcesses(from, to, route, limit),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to, route, rangeKey),
  });
}

export function useTopHostsQuery(
  client: QueryApiClient | null,
  from: string,
  to: string,
  route: string,
  limit = 10,
  rangeKey?: string
) {
  return useQuery({
    queryKey: ['topHosts', from, to, route, limit, rangeKey],
    queryFn: () => client!.getTopHosts(from, to, route, limit),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to, route, rangeKey),
  });
}

export function useTopRulesQuery(
  client: QueryApiClient | null,
  from: string,
  to: string,
  route: string,
  limit = 10,
  rangeKey?: string
) {
  return useQuery({
    queryKey: ['topRules', from, to, route, limit, rangeKey],
    queryFn: () => client!.getTopRules(from, to, route, limit),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to, route, rangeKey),
  });
}

export function useTopFinalProxiesQuery(
  client: QueryApiClient | null,
  from: string,
  to: string,
  route: string,
  limit = 10,
  rangeKey?: string
) {
  return useQuery({
    queryKey: ['topFinalProxies', from, to, route, limit, rangeKey],
    queryFn: () => client!.getTopFinalProxies(from, to, route, limit),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to, route, rangeKey),
  });
}

export function useProtocolsQuery(
  client: QueryApiClient | null,
  from: string,
  to: string,
  route: string,
  limit = 10,
  rangeKey?: string
) {
  return useQuery({
    queryKey: ['protocols', from, to, route, limit, rangeKey],
    queryFn: () => client!.getProtocols(from, to, route, limit),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to, route, rangeKey),
  });
}

export function useCoverageQuery(client: QueryApiClient | null, from: string, to: string, rangeKey?: string) {
  return useQuery({
    queryKey: ['coverage', from, to, rangeKey],
    queryFn: () => client!.getCoverage(from, to),
    enabled: !!client,
    placeholderData: keepLiveTickOnly(from, to, undefined, rangeKey),
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
