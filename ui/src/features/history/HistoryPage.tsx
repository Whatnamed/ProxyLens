import React, { useEffect, useMemo } from 'react';
import { QueryApiClient } from '../../api/client';
import { MetaResponse, ConnectionRecord } from '../../api/types';
import { useAuditContext, HistoryFilters } from '../../state/AuditContext';
import { useConnectionsQuery } from '../../api/queries';
import { PageGate, isNoAccountingRunError, errorCodeOf } from '../common/PageGate';
import { EmptyState, ErrorState, RouteBadge, SkeletonRows, EvidenceChip } from '../../components/ui/primitives';
import { RouteControl, TimeRangeControl, HistorySnapshotControl } from '../../components/audit/AuditContextBar';
import { ConnectionInspector } from './ConnectionInspector';
import { formatBytes } from '../../utils/format';
import { IconClose } from '../../components/ui/icons';

const NETWORK_OPTIONS = [
  { value: '', label: 'All networks' },
  { value: 'tcp', label: 'TCP' },
  { value: 'udp', label: 'UDP' },
];

function formatRowTime(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

interface RowEvidence {
  kind: 'estimated' | 'ambiguous' | 'missing' | 'neutral';
  label: string;
  title: string;
}

function rowEvidence(c: ConnectionRecord): RowEvidence | null {
  const missing: string[] = [];
  if (!c.metadata?.process) missing.push('missing process');
  if (!c.metadata?.host && !c.metadata?.sniffHost) missing.push('missing host (IP-only)');
  if (!c.rule) missing.push('missing rule');
  if (!c.chains || c.chains.length === 0) missing.push('missing chain');
  const cls = c.latestAttributionClass ?? '';
  if (cls.includes('ambiguous') || cls.includes('unpaired') || cls.includes('relay_candidate')) {
    return {
      kind: 'ambiguous',
      label: 'Ambiguous relay',
      title: `Attribution class ${cls}: relay candidate without a confirmed pairing. Kept as its own evidence, never merged into an unknown bucket.`,
    };
  }
  if (missing.length > 0) {
    return { kind: 'missing', label: 'Evidence gap', title: `Explainable unknown: ${missing.join(', ')}` };
  }
  if (c.preexistingAtStart) {
    return {
      kind: 'estimated',
      label: 'Preexisting',
      title: 'Connection existed before this collector session started; its pre-session baseline is excluded from in-session increments.',
    };
  }
  if (c.possibleUnobservedTail) {
    return {
      kind: 'estimated',
      label: 'Unobserved tail',
      title: 'Connection disappeared from snapshots; final traffic after the last observation may be unrecorded.',
    };
  }
  return null;
}

function destinationOf(c: ConnectionRecord): { primary: string; secondary: string } {
  const host = c.metadata?.host || c.metadata?.sniffHost;
  const ip = c.metadata?.destinationIP;
  const port = c.metadata?.destinationPort;
  if (host) {
    return { primary: host, secondary: ip ? `${ip}${port ? `:${port}` : ''}` : '' };
  }
  if (ip) {
    return { primary: `${ip}${port ? `:${port}` : ''}`, secondary: 'IP-only destination' };
  }
  return { primary: '(unknown destination)', secondary: '' };
}

function egressOf(c: ConnectionRecord): string {
  const route = (c.route || '').toUpperCase();
  if (route === 'DIRECT') return 'DIRECT';
  if (route === 'REJECT') return 'REJECT';
  return c.chains?.[0] ?? '(unknown)';
}

const FilterChipRow: React.FC = () => {
  const { filters, setFilter, clearFilters } = useAuditContext();
  const active = Object.entries(filters).filter(([, v]) => v) as [keyof HistoryFilters, string][];
  if (active.length === 0) return null;
  const labels: Record<keyof HistoryFilters, string> = {
    process: 'Process',
    host: 'Host',
    destinationIp: 'Dest IP',
    network: 'Network',
  };
  return (
    <>
      {active.map(([key, value]) => (
        <span className="pl-chip" key={key}>
          <span className="pl-chip__label">{labels[key]}:</span>
          <span className="pl-mono">{value}</span>
          <button className="pl-chip__remove" aria-label={`Remove ${labels[key]} filter`} onClick={() => setFilter(key, '')}>
            <IconClose size={10} />
          </button>
        </span>
      ))}
      <button className="pl-btn pl-btn--quiet pl-btn--compact" onClick={clearFilters}>
        Clear all
      </button>
    </>
  );
};

export const HistoryPage: React.FC<{
  client: QueryApiClient | null;
  sessionError: string | null;
  meta: MetaResponse | undefined;
}> = ({ client, sessionError, meta }) => {
  const {
    routeFocus,
    filters,
    setFilter,
    page,
    setPage,
    pageSize,
    setPageSize,
    snapshot,
    selected,
    setSelected,
  } = useAuditContext();

  const from = snapshot?.from ?? '';
  const to = snapshot?.to ?? '';

  const connectionsQ = useConnectionsQuery(client, {
    from,
    to,
    route: routeFocus,
    filters,
    limit: pageSize,
    offset: page * pageSize,
  });

  const items = connectionsQ.data?.items ?? [];
  const hasMore = connectionsQ.data?.hasMore ?? false;
  const noRun = isNoAccountingRunError(connectionsQ.error);
  const errCode = errorCodeOf(connectionsQ.error);

  useEffect(() => {
    if (selected && !items.some((i) => i.sessionId === selected.sessionId && i.epochId === selected.epochId && i.connectionId === selected.connectionId)) {
      if (!connectionsQ.isLoading && !connectionsQ.isPlaceholderData) {
        setSelected(null);
      }
    }
  }, [items, selected, connectionsQ.isLoading, connectionsQ.isPlaceholderData, setSelected]);

  const rangeStart = page * pageSize;
  const rangeEnd = rangeStart + items.length;

  const table = useMemo(() => {
    if (noRun) {
      return (
        <EmptyState
          title="No recorded history yet"
          body={
            <>
              No completed accounting run exists in this database. Once the Collector records
              connections and accounting completes, history will appear here.
            </>
          }
        />
      );
    }
    if (connectionsQ.isLoading && !connectionsQ.data) {
      return <SkeletonRows rows={12} />;
    }
    if (connectionsQ.isError && !noRun) {
      return (
        <ErrorState
          title="History query failed"
          body={String((connectionsQ.error as Error)?.message ?? connectionsQ.error)}
          code={errCode}
        />
      );
    }
    if (items.length === 0) {
      const hasFilters = Object.values(filters).some(Boolean);
      return (
        <EmptyState
          title={hasFilters ? 'No connections match the current filters' : 'No connections in this window'}
          body={
            hasFilters ? (
              <>Filters are preserved. Clear them to widen the query.</>
            ) : (
              <>No connections were observed in the frozen snapshot window for this route focus.</>
            )
          }
        />
      );
    }
    return (
      <table className="pl-table" aria-label="Historical connections">
        <colgroup>
          <col style={{ width: 128 }} />
          <col style={{ width: 140 }} />
          <col />
          <col style={{ width: 56 }} />
          <col style={{ width: 84 }} />
          <col style={{ width: 150 }} />
          <col style={{ width: 120 }} />
          <col style={{ width: 108 }} />
          <col style={{ width: 108 }} />
        </colgroup>
        <thead>
          <tr>
            <th>Time</th>
            <th>Process</th>
            <th>Destination</th>
            <th>Net</th>
            <th>Route</th>
            <th>Rule</th>
            <th>Egress</th>
            <th className="pl-num">Traffic</th>
            <th>Evidence</th>
          </tr>
        </thead>
        <tbody>
          {items.map((c) => {
            const key = `${c.sessionId}/${c.epochId}/${c.connectionId}`;
            const dest = destinationOf(c);
            const evidence = rowEvidence(c);
            const isSelected =
              selected?.sessionId === c.sessionId &&
              selected?.epochId === c.epochId &&
              selected?.connectionId === c.connectionId;
            const up = c.monitoredUploadTotal;
            const down = c.monitoredDownloadTotal;
            return (
              <tr
                key={key}
                className={isSelected ? 'pl-row--selected' : ''}
                aria-selected={isSelected}
                tabIndex={0}
                onClick={() => setSelected({ sessionId: c.sessionId, epochId: c.epochId, connectionId: c.connectionId })}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    setSelected({ sessionId: c.sessionId, epochId: c.epochId, connectionId: c.connectionId });
                  }
                }}
              >
                <td>
                  <span className="pl-mono pl-small" title={`First observed ${c.firstObservedAt}`}>
                    {formatRowTime(c.firstObservedAt)}
                  </span>
                  {c.observationActive && <span className="pl-cell-sub">active</span>}
                </td>
                <td>
                  <span className="pl-truncate" style={{ display: 'block' }} title={c.metadata?.processPath || c.metadata?.process}>
                    {c.metadata?.process || <span className="pl-muted">—</span>}
                  </span>
                </td>
                <td>
                  <span className="pl-truncate" style={{ display: 'block' }} title={`${dest.primary}${dest.secondary ? ` · ${dest.secondary}` : ''}`}>
                    {dest.primary}
                  </span>
                  {dest.secondary && <span className="pl-cell-sub pl-mono">{dest.secondary}</span>}
                </td>
                <td>
                  <span className="pl-net-token">{c.metadata?.network ?? '?'}</span>
                </td>
                <td>
                  <RouteBadge route={c.route} />
                </td>
                <td>
                  <span className="pl-truncate pl-mono pl-small" style={{ display: 'block' }} title={c.rule ? `${c.rule}${c.rulePayload ? ` · ${c.rulePayload}` : ''}` : 'No rule recorded'}>
                    {c.rule || <span className="pl-muted">—</span>}
                    {c.rulePayload ? <span className="pl-muted"> {c.rulePayload}</span> : null}
                  </span>
                </td>
                <td>
                  <span className="pl-truncate pl-mono pl-small" style={{ display: 'block' }} title={egressOf(c)}>
                    {egressOf(c)}
                  </span>
                </td>
                <td className="pl-cell-num">
                  <span title={`Upload ${up} B · Download ${down} B (monitored totals)`}>{formatBytes(up + down)}</span>
                  <span className="pl-cell-sub" style={{ textAlign: 'right' }}>
                    ↑{formatBytes(up)} ↓{formatBytes(down)}
                  </span>
                </td>
                <td>{evidence ? <EvidenceChip kind={evidence.kind} label={evidence.label} title={evidence.title} /> : null}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    );
  }, [items, selected, connectionsQ, noRun, filters, errCode, setSelected]);

  return (
    <PageGate client={client} sessionError={sessionError} meta={meta}>
      <div className="pl-page">
        <div className="pl-page__header">
          <h1 className="pl-page__title">History</h1>
          <div className="pl-page__header-right">
            <span className="pl-muted pl-small">Newest first · frozen snapshot</span>
          </div>
        </div>

        <div className="pl-context-bar">
          <TimeRangeControl />
          <div className="pl-context-bar__sep" />
          <RouteControl />
          <HistorySnapshotControl />
        </div>

        <div className="pl-history">
          <div className="pl-history__main">
            <div className="pl-history-toolbar">
              <input
                className="pl-input pl-history-toolbar__input"
                placeholder="Filter process…"
                aria-label="Filter by process name"
                value={filters.process ?? ''}
                onChange={(e) => setFilter('process', e.target.value)}
              />
              <input
                className="pl-input pl-history-toolbar__input"
                placeholder="Filter host…"
                aria-label="Filter by host"
                value={filters.host ?? ''}
                onChange={(e) => setFilter('host', e.target.value)}
              />
              <input
                className="pl-input pl-history-toolbar__input pl-history-toolbar__input--ip pl-input--mono"
                placeholder="Dest IP…"
                aria-label="Filter by destination IP"
                value={filters.destinationIp ?? ''}
                onChange={(e) => setFilter('destinationIp', e.target.value)}
              />
              <select
                className="pl-select"
                aria-label="Filter by network"
                value={filters.network ?? ''}
                onChange={(e) => setFilter('network', e.target.value)}
              >
                {NETWORK_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>{o.label}</option>
                ))}
              </select>
              <FilterChipRow />
            </div>

            <div className="pl-history__table-wrap">{table}</div>

            <div className="pl-history__footer">
              {items.length > 0 && (
                <span className="pl-mono pl-small">
                  Showing {rangeStart + 1}–{rangeEnd}
                </span>
              )}
              <div className="pl-history__footer-right">
                <select
                  className="pl-select"
                  aria-label="Page size"
                  value={pageSize}
                  onChange={(e) => setPageSize(Number(e.target.value))}
                >
                  {[50, 100, 200].map((s) => (
                    <option key={s} value={s}>{s} / page</option>
                  ))}
                </select>
                <div className="pl-pagination">
                  <button
                    className="pl-btn pl-btn--quiet pl-btn--compact"
                    disabled={page === 0}
                    onClick={() => setPage(Math.max(0, page - 1))}
                  >
                    Previous
                  </button>
                  <span className="pl-pagination__page">Page {page + 1}</span>
                  <button
                    className="pl-btn pl-btn--quiet pl-btn--compact"
                    disabled={!hasMore}
                    onClick={() => setPage(page + 1)}
                    title={hasMore ? undefined : 'No more rows reported by the API (hasMore=false)'}
                  >
                    Next
                  </button>
                </div>
              </div>
            </div>
          </div>

          <ConnectionInspector client={client} />
        </div>
      </div>
    </PageGate>
  );
};
