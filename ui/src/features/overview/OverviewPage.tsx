import React from 'react';
import { QueryApiClient } from '../../api/client';
import { MetaResponse } from '../../api/types';
import { useAuditContext } from '../../state/AuditContext';
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

function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return '0m';
  const totalSec = Math.round(ms / 1000);
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

const TotalBlock: React.FC<{ label: string; route?: string; up: number; down: number }> = ({
  label,
  route,
  up,
  down,
}) => (
  <div className="pl-total-block">
    <div className="pl-total-block__label">
      {route ? <RouteBadge route={route} /> : <span className="pl-eyebrow">{label}</span>}
    </div>
    <div className="pl-total-block__value">{formatBytes(up + down)}</div>
    <div className="pl-total-block__parts">
      <span title="Upload">↑ {formatBytes(up)}</span>
      <span title="Download">↓ {formatBytes(down)}</span>
    </div>
  </div>
);

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
  if (items.length === 0) {
    return <div className="pl-muted pl-small" style={{ padding: '4px 8px' }}>No recorded traffic in this scope.</div>;
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
            <span className="pl-rank__value" title={`Upload ${item.uploadBytes} B / Download ${item.downloadBytes} B (exact integers)`}>
              {formatBytes(item.totalBytes)}
            </span>
            <span className="pl-rank__count" title="Connection count">
              {item.connectionCount} cn
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
          <h1 className="pl-page__title">Overview</h1>
          <div className="pl-page__header-right">
            {freshness && (
              <StatusIndicator
                kind={freshness.isFresh ? 'fresh' : 'stale'}
                label={freshness.isFresh ? 'Accounting fresh' : `As of last run · ${freshness.lagEvents} events behind`}
                title={
                  freshness.isFresh
                    ? 'The latest completed accounting run covers all recorded journal events.'
                    : `isFresh=false is not a failure: ${freshness.lagEvents} journal events are still awaiting the next accounting rebuild. Values are as of the last completed run.`
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
                title="No completed accounting run"
                body={
                  <>
                    This database has no completed accounting run yet, so reconciled traffic
                    summaries are not available. Once the Collector records events and accounting
                    completes, this page will populate automatically.
                  </>
                }
              />
            ) : summaryQ.isLoading ? (
              <SkeletonRows rows={8} />
            ) : summaryQ.isError && !noRun ? (
              <ErrorState
                title="Summary query failed"
                body={String((summaryQ.error as Error)?.message ?? summaryQ.error)}
              />
            ) : summary ? (
              <>
                <Section
                  title="Traffic summary"
                  sub={`Routing outcome totals for ${routeFocus === 'ALL' ? 'all routes' : `route focus: ${routeFocus}`} · reconciled accounted bytes`}
                >
                  <div className="pl-overview__totals">
                    <TotalBlock label="Proxy" route="PROXY" up={summary.proxyUpload} down={summary.proxyDownload} />
                    <TotalBlock label="Direct" route="DIRECT" up={summary.directUpload} down={summary.directDownload} />
                    <TotalBlock label="Reject" route="REJECT" up={summary.rejectUpload} down={summary.rejectDownload} />
                  </div>
                  {routeFocus !== 'ALL' && (
                    <div className="pl-evidence-fact" style={{ marginTop: 'var(--pl-space-4)', maxWidth: 420 }}>
                      <span className="pl-evidence-fact__label">In-scope total ({routeFocus})</span>
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
                  title="Evidence trust"
                  sub="Explicit evidence-quality facts — no composite score"
                >
                  <div className="pl-evidence-facts">
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">Monitoring coverage</span>
                      <span className="pl-evidence-fact__value">
                        {coverage?.coverageRatio !== undefined
                          ? `${(coverage.coverageRatio * 100).toFixed(1)}%`
                          : 'n/a'}
                      </span>
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">Covered / uncovered</span>
                      <span className="pl-evidence-fact__value">
                        {coverage ? `${formatDuration(coverage.coveredDurationMs)} / ${formatDuration(coverage.uncoveredDurationMs)}` : '-'}
                      </span>
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">Missing attribution</span>
                      <span className="pl-evidence-fact__value" title="Traffic observed without process attribution">
                        {formatBytes(summary.missingAttributionUpload + summary.missingAttributionDownload)}
                      </span>
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">Ambiguous relay</span>
                      <span className="pl-evidence-fact__value" title="Relay candidates not confidently paired; kept, never silently dropped">
                        {formatBytes(summary.ambiguousRelayUpload + summary.ambiguousRelayDownload)}
                      </span>
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">Sampling residual</span>
                      <span className="pl-evidence-fact__value" title="Phase difference between global counters and per-connection sums">
                        {formatBytes(summary.samplingResidualUpload + summary.samplingResidualDownload)}
                      </span>
                    </div>
                    <div className="pl-evidence-fact">
                      <span className="pl-evidence-fact__label">
                        Gap physical traffic
                        <span className="pl-evidence-chip pl-evidence-chip--estimated" style={{ marginLeft: 6 }}>estimated</span>
                      </span>
                      <span className="pl-evidence-fact__value" title="Interval-derived estimate from global counter deltas across controller gaps — not exact per-connection traffic">
                        {formatBytes(summary.controllerGapPhysicalUpload + summary.controllerGapPhysicalDownload)}
                      </span>
                    </div>
                  </div>
                </Section>

                <div className="pl-rankings-grid">
                  <Section title="Top processes" sub="Ranked by exact + estimated bytes in scope">
                    {processesQ.isLoading ? (
                      <SkeletonRows rows={5} />
                    ) : (
                      <RankingList
                        items={processesQ.data?.items ?? []}
                        renderLabel={(i) => <>{i.key || <span className="pl-muted">(missing process)</span>}</>}
                        onDrill={(i) => i.key && drillToHistory({ process: i.key })}
                        drillHint="Open History filtered by this process"
                      />
                    )}
                  </Section>

                  <Section title="Top hosts" sub="Destination domains ranked by traffic">
                    {hostsQ.isLoading ? (
                      <SkeletonRows rows={5} />
                    ) : (
                      <RankingList
                        items={hostsQ.data?.items ?? []}
                        renderLabel={(i) => <>{i.key || <span className="pl-muted">(IP-only destination)</span>}</>}
                        onDrill={(i) => i.key && drillToHistory({ host: i.key })}
                        drillHint="Open History filtered by this host"
                      />
                    )}
                  </Section>

                  <Section title="Top rules" sub="Rule + payload ranked by traffic">
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

                  <Section title="Top final proxies" sub="Physical egress nodes ranked by traffic">
                    {proxiesQ.isLoading ? (
                      <SkeletonRows rows={5} />
                    ) : (
                      <RankingList
                        items={proxiesQ.data?.items ?? []}
                        renderLabel={(i) => <span className="pl-mono">{i.key || '(none)'}</span>}
                      />
                    )}
                  </Section>
                </div>

                <Section title="Protocols & networks" sub="Secondary distribution">
                  {protocolsQ.isLoading ? (
                    <SkeletonRows rows={2} />
                  ) : (protocolsQ.data?.items ?? []).length === 0 ? (
                    <div className="pl-muted pl-small">No protocol breakdown available in this scope.</div>
                  ) : (
                    <div style={{ display: 'flex', gap: 'var(--pl-space-6)', flexWrap: 'wrap' }}>
                      {(protocolsQ.data?.items ?? []).map((p, idx) => (
                        <div key={idx} style={{ display: 'flex', alignItems: 'baseline', gap: 'var(--pl-space-2)' }}>
                          <span className="pl-net-token">{p.key}</span>
                          <span className="pl-rank__value">{formatBytes(p.totalBytes)}</span>
                          <span className="pl-rank__count">{p.connectionCount} cn</span>
                        </div>
                      ))}
                    </div>
                  )}
                </Section>

                <div className="pl-muted pl-small" style={{ display: 'flex', alignItems: 'center', gap: 6, paddingBottom: 8 }}>
                  <IconArrowRight />
                  Top Processes and Top Hosts drill down into a filtered History; rule/egress drill-down is not
                  supported by the current read-only API and is intentionally omitted.
                </div>
              </>
            ) : null}
          </div>
        </div>
      </div>
    </PageGate>
  );
};
