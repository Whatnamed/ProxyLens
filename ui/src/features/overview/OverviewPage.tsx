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
import { deriveOverviewTotals, deriveOverviewEvidence } from './overviewSemantics';

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
  const { resolvedRange, routeFocus, drillToHistory, rangeSourceKey } = useAuditContext();
  const { from, to } = resolvedRange;

  const isRouteScoped = routeFocus !== 'ALL';
  const allSummaryQ = useSummaryQuery(client, from, to, 'ALL', rangeSourceKey);
  const scopedSummaryQ = useSummaryQuery(client, from, to, routeFocus, rangeSourceKey, { enabled: isRouteScoped });
  const coverageQ = useCoverageQuery(client, from, to, rangeSourceKey);
  const processesQ = useTopProcessesQuery(client, from, to, routeFocus, 10, rangeSourceKey);
  const hostsQ = useTopHostsQuery(client, from, to, routeFocus, 10, rangeSourceKey);
  const rulesQ = useTopRulesQuery(client, from, to, routeFocus, 10, rangeSourceKey);
  const proxiesQ = useTopFinalProxiesQuery(client, from, to, routeFocus, 10, rangeSourceKey);
  const protocolsQ = useProtocolsQuery(client, from, to, routeFocus, 10, rangeSourceKey);

  const allSummary = allSummaryQ.data;
  const scopedSummary = isRouteScoped ? scopedSummaryQ.data : allSummary;
  const totals = deriveOverviewTotals(allSummary, routeFocus);
  const evidence = deriveOverviewEvidence(allSummary, scopedSummary, routeFocus);
  const freshness = allSummary?.freshness ?? meta?.freshness;
  const coverage = coverageQ.data ?? allSummary?.coverage;
  const noRun = isNoAccountingRunError(allSummaryQ.error);
  const isSummaryLoading = allSummaryQ.isLoading;
  const isSummaryError = allSummaryQ.isError && !noRun;
  const summaryError = allSummaryQ.error as Error | null;

  const isScopedLoading = isRouteScoped && scopedSummaryQ.isLoading;
  const isScopedError = isRouteScoped && scopedSummaryQ.isError;
  const scopedErrorMsg = scopedSummaryQ.error
    ? String((scopedSummaryQ.error as Error)?.message ?? scopedSummaryQ.error)
    : undefined;

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
            ) : isSummaryLoading ? (
              <SkeletonRows rows={8} />
            ) : isSummaryError ? (
              <ErrorState
                title={t('overview.summaryQueryFailed')}
                body={String(summaryError?.message ?? summaryError)}
              />
            ) : allSummary ? (
              <>
                <Section
                  title={t('overview.trafficSummary')}
                  sub={t('overview.trafficSummarySub')}
                >
                  <div className="pl-overview__totals">
                    <TotalBlock label={t('route.proxy')} route="PROXY" up={totals.proxyUp} down={totals.proxyDown} />
                    <TotalBlock label={t('route.direct')} route="DIRECT" up={totals.directUp} down={totals.directDown} />
                    <TotalBlock label={t('route.reject')} route="REJECT" up={totals.rejectUp} down={totals.rejectDown} />
                  </div>
                  {totals.inScopeTotal !== null && (
                    <div className="pl-evidence-fact" style={{ marginTop: 'var(--pl-space-4)', maxWidth: 420 }}>
                      <span className="pl-evidence-fact__label">{t('overview.inScopeTotal', { route: routeFocus })}</span>
                      <span className="pl-evidence-fact__value">
                        {formatBytes(totals.inScopeTotal)}
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
                      <span className="pl-evidence-fact__label">
                        {t('overview.missingAttribution')}
                        {evidence.missingAttributionScoped && <RouteBadge route={routeFocus} quiet />}
                      </span>
                      {isScopedLoading ? (
                        <span className="pl-evidence-fact__value pl-muted">…</span>
                      ) : isScopedError || evidence.missingAttributionBytes === null ? (
                        <span
                          className="pl-evidence-fact__value pl-muted"
                          title={scopedErrorMsg || t('common.notAvailable')}
                        >
                          {t('common.notAvailable')}
                        </span>
                      ) : (
                        <span
                          className="pl-evidence-fact__value"
                          title={
                            evidence.missingAttributionScoped
                              ? t('overview.trafficObservedWithoutProcessScoped', { route: routeFocus })
                              : t('overview.trafficObservedWithoutProcess')
                          }
                        >
                          {formatBytes(evidence.missingAttributionBytes)}
                        </span>
                      )}
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">
                        {t('overview.ambiguousRelay')}
                        {evidence.ambiguousRelayScoped && <RouteBadge route={routeFocus} quiet />}
                      </span>
                      {isScopedLoading ? (
                        <span className="pl-evidence-fact__value pl-muted">…</span>
                      ) : isScopedError || evidence.ambiguousRelayBytes === null ? (
                        <span
                          className="pl-evidence-fact__value pl-muted"
                          title={scopedErrorMsg || t('common.notAvailable')}
                        >
                          {t('common.notAvailable')}
                        </span>
                      ) : (
                        <span
                          className="pl-evidence-fact__value"
                          title={
                            evidence.ambiguousRelayScoped
                              ? t('overview.relayCandidatesScoped', { route: routeFocus })
                              : t('overview.relayCandidates')
                          }
                        >
                          {formatBytes(evidence.ambiguousRelayBytes)}
                        </span>
                      )}
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">{t('overview.samplingResidual')}</span>
                      <span className="pl-evidence-fact__value" title={t('overview.samplingPhase')}>
                        {formatBytes(evidence.samplingResidualBytes)}
                      </span>
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">
                        {t('overview.gapPhysicalTraffic')}
                        <span className="pl-evidence-chip pl-evidence-chip--estimated" style={{ marginLeft: 6 }}>
                          <span className="pl-evidence-chip__label pl-compact-label">{t('common.estimated')}</span>
                        </span>
                      </span>
                      <span className="pl-evidence-fact__value" title={t('overview.gapPhysicalTrafficTitle')}>
                        {formatBytes(evidence.gapPhysicalTrafficBytes)}
                      </span>
                    </div>
                    {evidence.showUnknownRoute && (
                      <div className="pl-evidence-fact">
                        <span className="pl-evidence-fact__label">
                          {t('overview.unknownRoute')}
                          <span className="pl-evidence-chip pl-evidence-chip--ambiguous" style={{ marginLeft: 6 }}>
                            <span className="pl-evidence-chip__label pl-compact-label">{t('common.exception')}</span>
                          </span>
                        </span>
                        <span className="pl-evidence-fact__value" title={t('overview.unknownRouteTitle')}>
                          {formatBytes(evidence.unknownRouteBytes)}
                        </span>
                      </div>
                    )}
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
                          <span className="pl-net-token"><span className="pl-net-token__label pl-compact-label">{p.key}</span></span>
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
