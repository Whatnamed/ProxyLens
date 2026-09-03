import React, { useEffect, useMemo } from 'react';
import { QueryApiClient } from '../../api/client';
import { MetaResponse, ConnectionRecord } from '../../api/types';
import { useAuditContext, HistoryFilters, useLocale } from '../../state/AuditContext';
import { useConnectionsQuery } from '../../api/queries';
import { PageGate, isNoAccountingRunError, errorCodeOf } from '../common/PageGate';
import { EmptyState, ErrorState, RouteBadge, SkeletonRows, EvidenceChip, SelectMenu } from '../../components/ui/primitives';
import { RouteControl, TimeRangeControl, HistorySnapshotControl } from '../../components/audit/AuditContextBar';
import { ConnectionInspector } from './ConnectionInspector';
import { formatBytes } from '../../utils/format';
import { formatLocalDateTime, formatLocalDateTimeCompact } from '../../utils/time';
import { IconClose } from '../../components/ui/icons';

const NETWORK_OPTIONS = [
  { value: '' },
  { value: 'tcp' },
  { value: 'udp' },
];

interface RowEvidence {
  kind: 'estimated' | 'ambiguous' | 'missing' | 'neutral';
  label: string;
  title: string;
}

function rowEvidence(c: ConnectionRecord, t: (key: string, vars?: Record<string, string | number>) => string): RowEvidence | null {
  const missing: string[] = [];
  if (!c.metadata?.process) missing.push(t('inspector.missingProcess'));
  if (!c.metadata?.host && !c.metadata?.sniffHost) missing.push(t('inspector.missingHost'));
  if (!c.rule) missing.push(t('inspector.missingRule'));
  if (!c.chains || c.chains.length === 0) missing.push(t('inspector.missingChain'));
  const cls = c.latestAttributionClass ?? '';
  if (cls.includes('ambiguous') || cls.includes('unpaired') || cls.includes('relay_candidate')) {
    return {
      kind: 'ambiguous',
      label: t('history.ambiguousRelay'),
      title: t('history.ambiguousRelayTitle', { cls }),
    };
  }
  if (missing.length > 0) {
    return { kind: 'missing', label: t('history.evidenceGap'), title: t('history.evidenceGapTitle', { details: missing.join(', ') }) };
  }
  if (c.preexistingAtStart) {
    return {
      kind: 'estimated',
      label: t('history.preexisting'),
      title: t('history.preexistingTitle'),
    };
  }
  if (c.possibleUnobservedTail) {
    return {
      kind: 'estimated',
      label: t('history.unobservedTail'),
      title: t('history.unobservedTailTitle'),
    };
  }
  return null;
}

function destinationOf(c: ConnectionRecord, t: (key: string) => string): { primary: string; secondary: string } {
  const host = c.metadata?.host || c.metadata?.sniffHost;
  const ip = c.metadata?.destinationIP;
  const port = c.metadata?.destinationPort;
  if (host) {
    return { primary: host, secondary: ip ? `${ip}${port ? `:${port}` : ''}` : '' };
  }
  if (ip) {
    return { primary: `${ip}${port ? `:${port}` : ''}`, secondary: t('history.ipOnly') };
  }
  return { primary: t('history.unknownDestination'), secondary: '' };
}

function egressOf(c: ConnectionRecord, t: (key: string) => string): string {
  const route = (c.route || '').toUpperCase();
  if (route === 'DIRECT') return 'DIRECT';
  if (route === 'REJECT') return 'REJECT';
  return c.chains?.[0] ?? `(${t('common.unknown').toLowerCase()})`;
}

const FilterChipRow: React.FC = () => {
  const { t } = useLocale();
  const { filters, setFilter, clearFilters } = useAuditContext();
  const active = Object.entries(filters).filter(([, v]) => v) as [keyof HistoryFilters, string][];
  if (active.length === 0) return null;
  const labels: Record<keyof HistoryFilters, string> = {
    process: t('history.filterLabelProcess'),
    host: t('history.filterLabelHost'),
    destinationIp: t('history.filterLabelDestIp'),
    network: t('history.filterLabelNetwork'),
  };
  return (
    <>
      {active.map(([key, value]) => (
        <span className="pl-chip" key={key}>
          <span className="pl-chip__label">{labels[key]}:</span>
          <span className="pl-mono">{value}</span>
          <button className="pl-chip__remove" aria-label={t('history.removeFilter', { label: labels[key] })} onClick={() => setFilter(key, '')}>
            <IconClose size={10} />
          </button>
        </span>
      ))}
      <button className="pl-btn pl-btn--quiet pl-btn--compact" onClick={clearFilters}>
        {t('common.clearAll')}
      </button>
    </>
  );
};

export const HistoryPage: React.FC<{
  client: QueryApiClient | null;
  sessionError: string | null;
  meta: MetaResponse | undefined;
}> = ({ client, sessionError, meta }) => {
  const { locale, t } = useLocale();
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

  const selectedIndex = selected
    ? items.findIndex(
        (i) =>
          i.sessionId === selected.sessionId &&
          i.epochId === selected.epochId &&
          i.connectionId === selected.connectionId
      )
    : -1;
  const hasPrev = selectedIndex > 0;
  const hasNext = selectedIndex !== -1 && selectedIndex < items.length - 1;

  const onNavigatePrev = () => {
    if (hasPrev) {
      const prev = items[selectedIndex - 1];
      setSelected({ sessionId: prev.sessionId, epochId: prev.epochId, connectionId: prev.connectionId });
    }
  };

  const onNavigateNext = () => {
    if (hasNext) {
      const next = items[selectedIndex + 1];
      setSelected({ sessionId: next.sessionId, epochId: next.epochId, connectionId: next.connectionId });
    }
  };

  useEffect(() => {
    if (!selected) return;
    const selectedEl = document.querySelector('.pl-row--selected');
    if (selectedEl) {
      selectedEl.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }, [selected]);

  const rangeStart = page * pageSize;
  const rangeEnd = rangeStart + items.length;

  const table = useMemo(() => {
    if (noRun) {
      return (
        <EmptyState
          title={t('history.noRecordedTitle')}
          body={
            <>{t('history.noRecordedBody')}</>
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
          title={t('history.queryFailed')}
          body={String((connectionsQ.error as Error)?.message ?? connectionsQ.error)}
          code={errCode}
        />
      );
    }
    if (items.length === 0) {
      const hasFilters = Object.values(filters).some(Boolean);
      return (
        <EmptyState
          title={hasFilters ? t('history.noMatchesTitle') : t('history.noWindowTitle')}
          body={
            hasFilters ? (
              <>{t('history.filtersPreserved')}</>
            ) : (
              <>{t('history.noConnectionsWindow')}</>
            )
          }
        />
      );
    }
    return (
      <table className="pl-table" aria-label={t('history.tableAria')}>
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
            <th>{t('history.time')}</th>
            <th>{t('history.process')}</th>
            <th>{t('history.destination')}</th>
            <th>{t('history.net')}</th>
            <th>{t('history.route')}</th>
            <th>{t('history.rule')}</th>
            <th>{t('history.egress')}</th>
            <th className="pl-num">{t('history.traffic')}</th>
            <th>{t('history.evidence')}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((c) => {
            const key = `${c.sessionId}/${c.epochId}/${c.connectionId}`;
            const dest = destinationOf(c, t);
            const evidence = rowEvidence(c, t);
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
                  <span className="pl-mono pl-small" title={t('history.firstObserved', { time: formatLocalDateTime(c.firstObservedAt, locale) })}>
                    {formatLocalDateTimeCompact(c.firstObservedAt, locale)}
                  </span>
                  {c.observationActive && <span className="pl-cell-sub">{t('history.active')}</span>}
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
                  <span className="pl-net-token"><span className="pl-net-token__label pl-compact-label">{c.metadata?.network ?? '?'}</span></span>
                </td>
                <td>
                  <RouteBadge route={c.route} />
                </td>
                <td>
                  <span className="pl-truncate pl-mono pl-small" style={{ display: 'block' }} title={c.rule ? `${c.rule}${c.rulePayload ? ` · ${c.rulePayload}` : ''}` : t('history.noRuleRecorded')}>
                    {c.rule || <span className="pl-muted">—</span>}
                    {c.rulePayload ? <span className="pl-muted"> {c.rulePayload}</span> : null}
                  </span>
                </td>
                <td>
                  <span className="pl-truncate pl-mono pl-small" style={{ display: 'block' }} title={egressOf(c, t)}>
                    {egressOf(c, t)}
                  </span>
                </td>
                <td className="pl-cell-num">
                  <span title={t('history.monitoredTotals', { upload: up, download: down })}>{formatBytes(up + down)}</span>
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
  }, [items, selected, connectionsQ, noRun, filters, errCode, setSelected, locale, t]);

  return (
    <PageGate client={client} sessionError={sessionError} meta={meta}>
      <div className="pl-page">
        <div className="pl-page__header">
          <h1 className="pl-page__title">{t('history.title')}</h1>
          <div className="pl-page__header-right">
            <span className="pl-muted pl-small">{t('history.subtitle')}</span>
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
                placeholder={t('history.filterProcessPlaceholder')}
                aria-label={t('history.filterProcessAria')}
                value={filters.process ?? ''}
                onChange={(e) => setFilter('process', e.target.value)}
              />
              <input
                className="pl-input pl-history-toolbar__input"
                placeholder={t('history.filterHostPlaceholder')}
                aria-label={t('history.filterHostAria')}
                value={filters.host ?? ''}
                onChange={(e) => setFilter('host', e.target.value)}
              />
              <input
                className="pl-input pl-history-toolbar__input pl-history-toolbar__input--ip pl-input--mono"
                placeholder={t('history.filterIpPlaceholder')}
                aria-label={t('history.filterIpAria')}
                value={filters.destinationIp ?? ''}
                onChange={(e) => setFilter('destinationIp', e.target.value)}
              />
              <SelectMenu
                className="pl-select-menu--network"
                ariaLabel={t('history.filterNetworkAria')}
                options={NETWORK_OPTIONS.map((option) => ({
                  value: option.value,
                  label: option.value === '' ? t('history.allNetworks') : option.value === 'tcp' ? t('history.networkTcp') : t('history.networkUdp'),
                }))}
                value={filters.network ?? ''}
                onChange={(value) => setFilter('network', value)}
              />
              <FilterChipRow />
            </div>

            <div className="pl-history__table-wrap">{table}</div>

            <div className="pl-history__footer">
              {items.length > 0 && (
                <span className="pl-mono pl-small">
                  {t('common.showingRange', { from: rangeStart + 1, to: rangeEnd })}
                </span>
              )}
              <div className="pl-history__footer-right">
                <SelectMenu
                  className="pl-select-menu--page-size"
                  placement="up"
                  ariaLabel={t('history.pageSizeAria')}
                  options={[50, 100, 200].map((size) => ({ value: size, label: t('history.pageSize', { size }) }))}
                  value={pageSize}
                  onChange={setPageSize}
                />
                <div className="pl-pagination">
                  <button
                    className="pl-btn pl-btn--quiet pl-btn--compact"
                    disabled={page === 0}
                    onClick={() => setPage(Math.max(0, page - 1))}
                  >
                    {t('common.previous')}
                  </button>
                  <span className="pl-pagination__page">{t('common.page', { page: page + 1 })}</span>
                  <button
                    className="pl-btn pl-btn--quiet pl-btn--compact"
                    disabled={!hasMore}
                    onClick={() => setPage(page + 1)}
                    title={hasMore ? undefined : t('common.noMoreRows')}
                  >
                    {t('common.next')}
                  </button>
                </div>
              </div>
            </div>
          </div>

          <ConnectionInspector
            client={client}
            hasPrev={hasPrev}
            hasNext={hasNext}
            onNavigatePrev={onNavigatePrev}
            onNavigateNext={onNavigateNext}
          />
        </div>
      </div>
    </PageGate>
  );
};
