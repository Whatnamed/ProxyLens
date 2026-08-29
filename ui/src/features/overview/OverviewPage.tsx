import React from 'react';
import { QueryApiClient } from '../../api/client';
import { MetaResponse } from '../../api/types';
import { useAuditContext, useLocale } from '../../state/AuditContext';
import {
  useCoverageQuery,
  useProtocolsQuery,
  useSummaryQuery,
  useTopFinalProxiesQuery,
  useTopHostsQuery,
  useTopProcessesQuery,
  useTopRulesQuery,
} from '../../api/queries';
import { PageGate, isNoAccountingRunError } from '../common/PageGate';
import {
  EmptyState,
  ErrorState,
  RouteBadge,
  Section,
  SkeletonRows,
  StatusIndicator,
} from '../../components/ui/primitives';
import { RouteControl, TimeRangeControl } from '../../components/audit/AuditContextBar';
import { formatBytes } from '../../utils/format';
import { IconArrowRight } from '../../components/ui/icons';
import { TopRuleItem } from '../../api/types';

function formatDuration(ms: number, t: (key: string, vars?: Record<string, string | number>) => string): string {
  if (!ms || ms <= 0) return t('common.durationMinutes', { value: 0 });
  const totalSec = Math.round(ms / 1000);
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  if (h > 0) return `${t('common.durationHours', { value: h })} ${t('common.durationMinutes', { value: m })}`;
  if (m > 0) return `${t('common.durationMinutes', { value: m })} ${t('common.durationSeconds', { value: s })}`;
  return t('common.durationSeconds', { value: s });
}

const TotalBlock: React.FC<{ label: string; route?: string; up: number; down: number }> = ({
  label,
  route,
  up,
  down,
}) => {
  const { t } = useLocale();
  return (
    <div className="pl-total-block">
      <div className="pl-total-block__label">
        {route ? <RouteBadge route={route} /> : <span className="pl-eyebrow">{label}</span>}
      </div>
      <div className="pl-total-block__value">{formatBytes(up + down)}</div>
      <div className="pl-total-block__parts">
        <span title={t('overview.uploadTitle')}>↑ {formatBytes(up)}</span>
        <span title={t('overview.downloadTitle')}>↓ {formatBytes(down)}</span>
      </div>
    </div>
  );
};

interface RankBase {
  uploadBytes: number;
  downloadBytes: number;
  totalBytes: number;
  connectionCount: number;
}

const RankingList = <T extends RankBase>({
  items,
  renderLabel,
  onDrill,
  drillHint,
}: {
  items: T[];
  renderLabel: (item: T) => React.ReactNode;
  onDrill?: (item: T) => void;
  drillHint?: string;
}) => {
  const { t } = useLocale();
  if (items.length === 0) {
    return <div className="pl-muted pl-small" style={{ padding: '4px 8px' }}>{t('overview.noTraffic')}</div>;
  }
  const max = Math.max(...items.map((i) => i.totalBytes), 1);
  return (
    <div>
      {items.map((item, idx) => {
        const pct = Math.max(2, Math.round((item.totalBytes / max) * 100));
        const clickable = !!onDrill;
        return (
          <div
            key={idx}
            className={`pl-rank pl-rank--with-bar${clickable ? ' pl-rank--link' : ''}`}
            onClick={clickable ? () => onDrill!(item) : undefined}
            title={clickable ? drillHint : undefined}
            role={clickable ? 'button' : undefined}
            tabIndex={clickable ? 0 : undefined}
            onKeyDown={
              clickable
                ? (e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      onDrill!(item);
                    }
                  }
                : undefined
            }
          >
            <span className="pl-rank__index">{idx + 1}</span>
            <span className="pl-rank__label">{renderLabel(item)}</span>
            <span className="pl-rank__value" title={t('overview.exactIntegerTitle', { upload: item.uploadBytes, download: item.downloadBytes })}>
              {formatBytes(item.totalBytes)}
            </span>
            <span className="pl-rank__count" title={t('overview.connectionCountTitle')}>
              {t('overview.connectionCount', { count: item.connectionCount })}
            </span>
            <span className="pl-rank__bar">
              <span className="pl-rank__bar-fill" style={{ width: `${pct}%` }} />
            </span>
          </div>
        );
      })}
    </div>
  );
};

export const OverviewPage: React.FC<{
  client: QueryApiClient | null;
  sessionError: string | null;
  meta: MetaResponse | undefined;
}> = ({ client, sessionError, meta }) => {
  const { t } = useLocale();
  const { resolvedRange, routeFocus, drillToHistory } = useAuditContext();
  const { from, to } = resolvedRange;

  const summaryQ = useSummaryQuery(client, from, to, routeFocus);
  const coverageQ = useCoverageQuery(client, from, to);
  const processesQ = useTopProcessesQuery(client, from, to, routeFocus);
  const hostsQ = useTopHostsQuery(client, from, to, routeFocus);
  const rulesQ = useTopRulesQuery(client, from, to, routeFocus);
  const proxiesQ = useTopFinalProxiesQuery(client, from, to, routeFocus);
  const protocolsQ = useProtocolsQuery(client, from, to, routeFocus);

  const summary = summaryQ.data;
  const freshness = summary?.freshness ?? meta?.freshness;
  const coverage = coverageQ.data ?? summary?.coverage;
  const noRun = isNoAccountingRunError(summaryQ.error);

  return (
    <PageGate client={client} sessionError={sessionError} meta={meta}>
      <div className="pl-page">
        <div className="pl-page__header">
          <h1 className="pl-page__title">{t('overview.title')}</h1>
          <div className="pl-page__header-right">
            {freshness && (
              <StatusIndicator
                kind={freshness.isFresh ? 'fresh' : 'stale'}
                label={freshness.isFresh ? t('overview.accountingFresh') : t('overview.asOfLastRun', { count: freshness.lagEvents })}
                title={
                  freshness.isFresh
                    ? t('overview.accountingFreshTitle')
                    : t('overview.accountingStaleTitle', { count: freshness.lagEvents })
                }
              />
            )}
          </div>
        </div>

        <div className="pl-context-bar">
          <TimeRangeControl />
          <div className="pl-context-bar__sep" />
          <RouteControl />
        </div>

        <div className="pl-page__scroll">
          <div className="pl-overview">
            {noRun ? (
              <EmptyState
                title={t('history.noRecordedTitle')}
                body={
                  <>{t('overview.noCompletedRunBody')}</>
                }
              />
            ) : summaryQ.isLoading ? (
              <SkeletonRows rows={8} />
            ) : summaryQ.isError && !noRun ? (
              <ErrorState
                title={t('overview.summaryQueryFailed')}
                body={String((summaryQ.error as Error)?.message ?? summaryQ.error)}
              />
            ) : summary ? (
              <>
                <Section
                  title={t('overview.trafficSummary')}
                  sub={t('overview.trafficSummarySub', { focus: routeFocus === 'ALL' ? t('overview.allRoutes') : t('overview.routeFocus', { route: routeFocus }) })}
                >
                  <div className="pl-overview__totals">
                    <TotalBlock label={t('route.proxy')} route="PROXY" up={summary.proxyUpload} down={summary.proxyDownload} />
                    <TotalBlock label={t('route.direct')} route="DIRECT" up={summary.directUpload} down={summary.directDownload} />
                    <TotalBlock label={t('route.reject')} route="REJECT" up={summary.rejectUpload} down={summary.rejectDownload} />
                  </div>
                  {routeFocus !== 'ALL' && (
                    <div className="pl-evidence-fact" style={{ marginTop: 'var(--pl-space-4)', maxWidth: 420 }}>
                      <span className="pl-evidence-fact__label">{t('overview.inScopeTotal', { route: routeFocus })}</span>
                      <span className="pl-evidence-fact__value">
                        {formatBytes(
                          (routeFocus === 'PROXY'
                            ? summary.proxyUpload + summary.proxyDownload
                            : routeFocus === 'DIRECT'
                              ? summary.directUpload + summary.directDownload
                              : summary.rejectUpload + summary.rejectDownload)
                        )}
                      </span>
                    </div>
                  )}
                </Section>

                <Section
                  title={t('overview.evidenceTrust')}
                  sub={t('overview.evidenceTrustSub')}
                >
                  <div className="pl-evidence-facts">
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">{t('overview.monitoringCoverage')}</span>
                      <span className="pl-evidence-fact__value">
                        {coverage?.coverageRatio !== undefined
                          ? `${(coverage.coverageRatio * 100).toFixed(1)}%`
                          : t('common.notAvailable')}
                      </span>
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">{t('overview.coveredUncovered')}</span>
                      <span className="pl-evidence-fact__value">
                        {coverage ? `${formatDuration(coverage.coveredDurationMs, t)} / ${formatDuration(coverage.uncoveredDurationMs, t)}` : '-'}
                      </span>
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">{t('overview.missingAttribution')}</span>
                      <span className="pl-evidence-fact__value" title={t('overview.trafficObservedWithoutProcess')}>
                        {formatBytes(summary.missingAttributionUpload + summary.missingAttributionDownload)}
                      </span>
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">{t('overview.ambiguousRelay')}</span>
                      <span className="pl-evidence-fact__value" title={t('overview.relayCandidates')}>
                        {formatBytes(summary.ambiguousRelayUpload + summary.ambiguousRelayDownload)}
                      </span>
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">{t('overview.samplingResidual')}</span>
                      <span className="pl-evidence-fact__value" title={t('overview.samplingPhase')}>
                        {formatBytes(summary.samplingResidualUpload + summary.samplingResidualDownload)}
                      </span>
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">
                        {t('overview.gapPhysicalTraffic')}
                        <span className="pl-evidence-chip pl-evidence-chip--estimated" style={{ marginLeft: 6 }}>{t('common.estimated')}</span>
                      </span>
                      <span className="pl-evidence-fact__value" title={t('overview.gapPhysicalTrafficTitle')}>
                        {formatBytes(summary.controllerGapPhysicalUpload + summary.controllerGapPhysicalDownload)}
                      </span>
                    </div>
                  </div>
                </Section>

                <div className="pl-rankings-grid">
                  <Section title={t('overview.topProcesses')} sub={t('overview.topProcessesSub')}>
                    {processesQ.isLoading ? (
                      <SkeletonRows rows={5} />
                    ) : (
                      <RankingList
                        items={processesQ.data?.items ?? []}
                        renderLabel={(i) => <>{i.key || <span className="pl-muted">{t('overview.missingProcess')}</span>}</>}
                        onDrill={(i) => i.key && drillToHistory({ process: i.key })}
                        drillHint={t('overview.drillHintProcess')}
                      />
                    )}
                  </Section>

                  <Section title={t('overview.topHosts')} sub={t('overview.topHostsSub')}>
                    {hostsQ.isLoading ? (
                      <SkeletonRows rows={5} />
                    ) : (
                      <RankingList
                        items={hostsQ.data?.items ?? []}
                        renderLabel={(i) => <>{i.key || <span className="pl-muted">{t('overview.ipOnlyDestination')}</span>}</>}
                        onDrill={(i) => i.key && drillToHistory({ host: i.key })}
                        drillHint={t('overview.drillHintHost')}
                      />
                    )}
                  </Section>

                  <Section title={t('overview.topRules')} sub={t('overview.topRulesSub')}>
                    {rulesQ.isLoading ? (
                      <SkeletonRows rows={5} />
                    ) : (
                      <RankingList
                        items={rulesQ.data?.items ?? []}
                        renderLabel={(i) => {
                          const r = i as TopRuleItem;
                          return (
                            <>
                              <span className="pl-mono">{r.rule}</span>
                              {r.rulePayload && <span className="pl-rank__sub pl-mono">{r.rulePayload}</span>}
                              <span className="pl-rank__sub"><RouteBadge route={r.route} quiet /></span>
                            </>
                          );
                        }}
                      />
                    )}
                  </Section>

                  <Section title={t('overview.topFinalProxies')} sub={t('overview.topFinalProxiesSub')}>
                    {proxiesQ.isLoading ? (
                      <SkeletonRows rows={5} />
                    ) : (
                      <RankingList
                        items={proxiesQ.data?.items ?? []}
                        renderLabel={(i) => <span className="pl-mono">{i.key || t('overview.none')}</span>}
                      />
                    )}
                  </Section>
                </div>

                <Section title={t('overview.protocols')} sub={t('overview.protocolsSub')}>
                  {protocolsQ.isLoading ? (
                    <SkeletonRows rows={2} />
                  ) : (protocolsQ.data?.items ?? []).length === 0 ? (
                    <div className="pl-muted pl-small">{t('overview.noProtocolBreakdown')}</div>
                  ) : (
                    <div style={{ display: 'flex', gap: 'var(--pl-space-6)', flexWrap: 'wrap' }}>
                      {(protocolsQ.data?.items ?? []).map((p, idx) => (
                        <div key={idx} style={{ display: 'flex', alignItems: 'baseline', gap: 'var(--pl-space-2)' }}>
                          <span className="pl-net-token">{p.key}</span>
                          <span className="pl-rank__value">{formatBytes(p.totalBytes)}</span>
                          <span className="pl-rank__count">{t('overview.connectionCount', { count: p.connectionCount })}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </Section>

                <div className="pl-muted pl-small" style={{ display: 'flex', alignItems: 'center', gap: 6, paddingBottom: 8 }}>
                  <IconArrowRight />
                  {t('overview.drillNote')}
                </div>
              </>
            ) : null}
          </div>
        </div>
      </div>
    </PageGate>
  );
};
