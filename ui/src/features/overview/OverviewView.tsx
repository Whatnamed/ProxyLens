import React from 'react';
import { QueryApiClient } from '../../api/client';
import {
  useCoverage,
  useMeta,
  useRouteSummary,
  useTopDimensions,
  useTrafficSummary
} from '../../api/queries';
import { useAudit, DrillLimitations } from '../../state/AuditContext';
import { TopDimensionItem, TopRuleItem } from '../../api/types';
import { Panel } from '../../components/ui/primitives';
import { ErrorState, Notice, SkeletonLine } from '../../components/ui/states';
import { toProductError, ProductError } from '../../lib/apiError';
import { formatBytes, formatCount, formatPercent } from '../../utils/format';
import { ROUTE_META } from '../../lib/semantics';
import { RouteTiles } from './RouteTiles';
import { EvidenceTrust } from './EvidenceTrust';
import { RankPanel, RankRow, SplitLegend } from './RankPanel';

/**
 * Overview answers: how much, who, where, why, through what — and whether the
 * evidence behind those numbers can be trusted.
 *
 * It is an entry surface for investigation, not a decorative dashboard: every
 * aggregate row drills down into the underlying History evidence.
 */

interface Props {
  client: QueryApiClient | null;
}

function toRow(
  item: TopDimensionItem,
  index: number,
  keyOf: (i: TopDimensionItem) => string
): RankRow {
  return {
    id: `${keyOf(item)}#${index}`,
    label: item.key === '' ? '(no value recorded)' : item.key,
    isEmptyKey: item.key === '',
    uploadBytes: item.uploadBytes,
    downloadBytes: item.downloadBytes,
    totalBytes: item.totalBytes,
    connectionCount: item.connectionCount,
    exactBytes: item.exactUploadBytes + item.exactDownloadBytes,
    estimatedBytes: item.estimatedUploadBytes + item.estimatedDownloadBytes
  };
}

function ruleToRow(item: TopRuleItem, index: number): RankRow {
  return {
    id: `${item.rule}|${item.rulePayload}|${index}`,
    label: item.rule === '' ? 'Match' : item.rule,
    sub: item.rulePayload === '' ? '(final fallback)' : item.rulePayload,
    isEmptyKey: item.rule === '',
    uploadBytes: item.uploadBytes,
    downloadBytes: item.downloadBytes,
    totalBytes: item.totalBytes,
    connectionCount: item.connectionCount,
    exactBytes: item.exactUploadBytes + item.exactDownloadBytes,
    estimatedBytes: item.estimatedUploadBytes + item.estimatedDownloadBytes
  };
}

export const OverviewView: React.FC<Props> = ({ client }) => {
  const { range, route, nonce, navigate, drillToHistory, setRoute } = useAudit();

  const traffic = useTrafficSummary(client, range, nonce);
  const routeSummary = useRouteSummary(client, range, route, nonce);
  const tops = useTopDimensions(client, range, route, nonce);
  const coverage = useCoverage(client, range, nonce);
  const meta = useMeta(client);

  // A failure of the foundational summary blocks the whole surface; individual
  // panels below handle their own failures independently.
  const blocking: ProductError | null = traffic.isError ? toProductError(traffic.error) : null;

  if (blocking) {
    return (
      <div style={{ padding: 'var(--sp-8)', display: 'flex', justifyContent: 'center' }}>
        <div style={{ width: '100%', maxWidth: 620 }}>
          <ErrorState error={blocking} onRetry={() => traffic.refetch()} />
        </div>
      </div>
    );
  }

  const allTotal =
    (traffic.data?.proxyUpload ?? 0) +
    (traffic.data?.proxyDownload ?? 0) +
    (traffic.data?.directUpload ?? 0) +
    (traffic.data?.directDownload ?? 0) +
    (traffic.data?.rejectUpload ?? 0) +
    (traffic.data?.rejectDownload ?? 0);

  const processRows = (tops.processes.data?.items ?? []).map((i, n) => toRow(i, n, (x) => x.key));
  const hostRows = (tops.hosts.data?.items ?? []).map((i, n) => toRow(i, n, (x) => x.key));
  const ruleRows = (tops.rules.data?.items ?? []).map(ruleToRow);
  const proxyRows = (tops.finalProxies.data?.items ?? []).map((i, n) => toRow(i, n, (x) => x.key));

  const netItems = tops.protocols.data?.items ?? [];
  const netTotal = netItems.reduce((s, i) => s + i.totalBytes, 0);

  /** Drilling into a dimension the Query API cannot filter must still land the
   *  user in evidence — but it must say so rather than imply a filter. */
  const drillProcess = (row: RankRow) => {
    if (row.isEmptyKey) {
      const limit: DrillLimitations = {
        unsupported: ['Process'],
        detail:
          'This aggregate has no process value, so it cannot be filtered. History was opened with the current time range and route focus only.'
      };
      drillToHistory(undefined, limit);
      return;
    }
    drillToHistory({ process: row.label });
  };

  const drillHost = (row: RankRow) => {
    if (row.isEmptyKey) {
      const limit: DrillLimitations = {
        unsupported: ['Host'],
        detail:
          'This aggregate has no host value, so it cannot be filtered. History was opened with the current time range and route focus only.'
      };
      drillToHistory(undefined, limit);
      return;
    }
    drillToHistory({ host: row.label });
  };

  const drillUnsupported = (dimension: string) => () => {
    const limit: DrillLimitations = {
      unsupported: [dimension],
      detail: `Query API v1 cannot filter History by ${dimension.toLowerCase()}. History was opened with the current time range and route focus only — no ${dimension.toLowerCase()} filter was applied.`
    };
    drillToHistory(undefined, limit);
  };

  return (
    <div className="overview">
      <div className="overview-primary">
        <Panel
          title="Traffic summary"
          note={`All routes: ${formatBytes(allTotal)}`}
          actions={<SplitLegend />}
        >
          <RouteTiles
            summary={traffic.data}
            isLoading={traffic.isLoading}
            route={route}
            onFocus={setRoute}
          />
          <div style={{ marginTop: 'var(--sp-5)' }}>
            <Notice tone="neutral">
              Route focus is <strong>{ROUTE_META[route].label}</strong> — rankings below follow it,
              while the totals above always show every routing class independently. Click a tile to
              move focus.
            </Notice>
          </div>
        </Panel>

        <Panel
          title="Evidence trust"
          note="Can these numbers be trusted?"
        >
          {coverage.isError ? (
            <ErrorState error={toProductError(coverage.error)} onRetry={() => coverage.refetch()} compact />
          ) : (
            <EvidenceTrust
              coverage={coverage.data}
              routeSummary={routeSummary.data}
              trafficSummary={traffic.data}
              freshness={meta.data?.freshness}
              onOpenCoverage={() => navigate('coverage')}
            />
          )}
        </Panel>
      </div>

      <div className="rank-grid">
        <RankPanel
          title="Top processes"
          question="Who"
          rows={processRows}
          isLoading={tops.processes.isLoading}
          drillFilter="process"
          onDrill={drillProcess}
        />
        <RankPanel
          title="Top hosts"
          question="Where"
          rows={hostRows}
          isLoading={tops.hosts.isLoading}
          drillFilter="host"
          onDrill={drillHost}
        />
        <RankPanel
          title="Top rules"
          question="Why"
          rows={ruleRows}
          isLoading={tops.rules.isLoading}
          drillFilter={null}
          unsupportedNote="Query API v1 cannot filter History by rule or rule payload. Clicking a rule opens History with the current time range and route focus, without a rule filter."
          onDrill={drillUnsupported('Rule')}
        />
        <RankPanel
          title="Top egress nodes"
          question="Through what"
          rows={proxyRows}
          isLoading={tops.finalProxies.isLoading}
          drillFilter={null}
          unsupportedNote="Query API v1 cannot filter History by final proxy. Clicking a node opens History with the current time range and route focus, without an egress filter."
          onDrill={drillUnsupported('Final proxy')}
        />
      </div>

      <Panel
        title="Network breakdown"
        note={`${formatBytes(netTotal)} across ${netItems.length} network type(s)`}
      >
        {tops.protocols.isLoading ? (
          <SkeletonLine height={22} />
        ) : netItems.length === 0 ? (
          <div className="dimmer" style={{ fontSize: 'var(--fs-12)' }}>
            No network breakdown available for this window.
          </div>
        ) : (
          <>
            <div className="net-split">
              {netItems.map((item) => {
                const pct = netTotal > 0 ? (item.totalBytes / netTotal) * 100 : 0;
                return (
                  <div
                    key={item.key}
                    className="net-seg"
                    style={{
                      width: `${pct}%`,
                      background:
                        item.key.toLowerCase() === 'udp'
                          ? 'var(--route-all)'
                          : 'var(--route-proxy)'
                    }}
                    title={`${item.key}: ${formatBytes(item.totalBytes)}`}
                  >
                    {pct > 12 ? `${item.key.toUpperCase()} ${formatPercent(pct, 0)}` : ''}
                  </div>
                );
              })}
            </div>
            <div className="net-legend">
              {netItems.map((item) => (
                <span key={item.key} className="net-legend-item">
                  <span
                    className="net-legend-swatch"
                    style={{
                      background:
                        item.key.toLowerCase() === 'udp'
                          ? 'var(--route-all)'
                          : 'var(--route-proxy)'
                    }}
                  />
                  <span className="mono">{item.key.toUpperCase()}</span>
                  <span className="mono dim">{formatBytes(item.totalBytes)}</span>
                  <span className="dimmer">{formatCount(item.connectionCount)} conn</span>
                </span>
              ))}
            </div>
          </>
        )}
      </Panel>
    </div>
  );
};
