import { useQuery } from '@tanstack/react-query';
import { QueryApiClient } from './client';

export function useMetaQuery(client: QueryApiClient | null) {
  return useQuery({
    queryKey: ['meta'],
    queryFn: () => client!.getMeta(),
    enabled: !!client,
    refetchInterval: 3000,
  });
}

export function useSummaryQuery(client: QueryApiClient | null, route?: string) {
  return useQuery({
    queryKey: ['summary', route],
    queryFn: () => client!.getSummary(undefined, undefined, route),
    enabled: !!client,
    refetchInterval: 5000,
  });
}

export function useTopProcessesQuery(client: QueryApiClient | null, limit: number = 5) {
  return useQuery({
    queryKey: ['topProcesses', limit],
    queryFn: () => client!.getTopProcesses(undefined, undefined, undefined, limit),
    enabled: !!client,
  });
}

export function useCoverageQuery(client: QueryApiClient | null) {
  return useQuery({
    queryKey: ['coverage'],
    queryFn: () => client!.getCoverage(),
    enabled: !!client,
  });
}

export function useConnectionsQuery(client: QueryApiClient | null, limit: number = 10, offset: number = 0) {
  return useQuery({
    queryKey: ['connections', limit, offset],
    queryFn: () => client!.getConnections({ limit, offset }),
    enabled: !!client,
  });
}
