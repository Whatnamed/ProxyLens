import React from 'react';
import { QueryApiClient } from '../../api/client';
import { MetaResponse, CoverageSummary, MergedGap } from '../../api/types';
import { useAuditContext } from '../../state/AuditContext';
import { useCoverageQuery } from '../../api/queries';
import { PageGate } from '../common/PageGate';
import { EmptyState, ErrorState, Section, SkeletonRows, StatusIndicator } from '../../components/ui/primitives';
import { TimeRangeControl } from '../../components/audit/AuditContextBar';
import { formatLocalDateTime } from '../../utils/time';
import { classifyGapSources, GapProvenance } from '../../utils/coverage';
import { IconArrowRight } from '../../components/ui/icons';

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

const gapProvenance = (g: MergedGap): GapProvenance => classifyGapSources(g.sources ?? [g.source]);

interface TimelineSegment {
  kind: 'covered' | 'controller' | 'collector' | 'mixed' | 'outside';
  from: number;
  to: number;
  title: string;
}

function buildSegments(coverage: CoverageSummary, windowStart: number, windowEnd: number): TimelineSegment[] {
  const segments: TimelineSegment[] = [];
  if (windowEnd <= windowStart) return segments;

  const effStart = coverage.effectiveScopeStart ? new Date(coverage.effectiveScopeStart).getTime() : null;
  const effEnd = coverage.effectiveScopeEnd ? new Date(coverage.effectiveScopeEnd).getTime() : null;

  // Outside monitored history: requested window before known scope
  if (effStart !== null && effStart > windowStart) {
    segments.push({
      kind: 'outside',
      from: windowStart,
      to: Math.min(effStart, windowEnd),
      title: 'Outside monitored history — no collector sessions are known for this interval',
    });
  }

  if (effStart !== null && effEnd !== null && effEnd > windowStart && effStart < windowEnd) {
    const gaps = [...(coverage.mergedGaps ?? [])]
      .map((g) => ({ g, start: new Date(g.startedAt).getTime(), end: new Date(g.endedAt).getTime() }))
      .filter((x) => x.end > windowStart && x.start < windowEnd)
      .sort((a, b) => a.start - b.start);

    let cursor = Math.max(effStart, windowStart);
    for (const { g, start, end } of gaps) {
      const gs = Math.max(start, windowStart);
      const ge = Math.min(end, windowEnd);
      if (gs > cursor) {
        segments.push({ kind: 'covered', from: cursor, to: gs, title: 'Covered — continuous monitoring' });
      }
      const src = gapProvenance(g);
      segments.push({
        kind: src.kind,
        from: gs,
        to: Math.max(ge, gs + (windowEnd - windowStart) * 0.004),
        title: `${src.label}: ${g.reason || g.reasons?.join('; ') || 'no reason recorded'} (${formatLocalDateTime(g.startedAt)} → ${formatLocalDateTime(g.endedAt)})`,
      });
      cursor = Math.max(cursor, ge);
    }
    if (cursor < Math.min(effEnd, windowEnd)) {
      segments.push({ kind: 'covered', from: cursor, to: Math.min(effEnd, windowEnd), title: 'Covered — continuous monitoring' });
    }
  }

  return segments;
}

const GapRow: React.FC<{ gap: MergedGap; onInspect: () => void }> = ({ gap, onInspect }) => {
  const src = gapProvenance(gap);
  return (
    <tr>
      <td>
        <StatusIndicator
          kind={src.kind === 'controller' ? 'gap' : 'offline'}
          label={src.label}
          title={`Provenance source: ${(gap.sources ?? [gap.source]).join(', ')}`}
        />
      </td>
      <td><span className="pl-mono pl-small">{formatLocalDateTime(gap.startedAt)}</span></td>
      <td><span className="pl-mono pl-small">{formatLocalDateTime(gap.endedAt)}</span></td>
      <td className="pl-cell-num">{formatDuration(gap.durationMs)}</td>
      <td>
        <span className="pl-truncate" style={{ display: 'block' }} title={(gap.reasons ?? [gap.reason]).join('; ')}>
          {gap.reason || '—'}
        </span>
      </td>
      <td>
        <button className="pl-btn pl-btn--quiet pl-btn--compact" onClick={onInspect}>
          Inspect around gap
          <IconArrowRight />
        </button>
      </td>
    </tr>
  );
};

export const CoveragePage: React.FC<{
  client: QueryApiClient | null;
  sessionError: string | null;
  meta: MetaResponse | undefined;
}> = ({ client, sessionError, meta }) => {
  const { resolvedRange, inspectAroundGap } = useAuditContext();
  const { from, to } = resolvedRange;
  const coverageQ = useCoverageQuery(client, from, to);
  const coverage = coverageQ.data;

  const windowStart = new Date(from).getTime();
  const windowEnd = new Date(to).getTime();
  const segments = coverage ? buildSegments(coverage, windowStart, windowEnd) : [];
  const totalSpan = Math.max(1, windowEnd - windowStart);
  const outsideScope = (coverage?.outsideKnownScopeMs ?? 0) > 0;
  const gaps = coverage?.mergedGaps ?? [];

  return (
    <PageGate client={client} sessionError={sessionError} meta={meta}>
      <div className="pl-page">
        <div className="pl-page__header">
          <h1 className="pl-page__title">Coverage</h1>
          <div className="pl-page__header-right">
            <span className="pl-muted pl-small">Monitoring completeness · not a route metric</span>
          </div>
        </div>

        <div className="pl-context-bar">
          <TimeRangeControl />
          <div className="pl-context-bar__sep" />
          <span className="pl-muted pl-small">Coverage evaluates collector presence, independent of route focus</span>
        </div>

        <div className="pl-page__scroll">
          <div className="pl-coverage">
            {coverageQ.isLoading ? (
              <SkeletonRows rows={8} />
            ) : coverageQ.isError ? (
              <ErrorState
                title="Coverage query failed"
                body={String((coverageQ.error as Error)?.message ?? coverageQ.error)}
              />
            ) : coverage ? (
              <>
                <Section title="Coverage summary" sub={`Requested window: ${formatLocalDateTime(from)} → ${formatLocalDateTime(to)} (local time)`}>
                  {outsideScope && !coverage.knownScopeStart && (
                    <EmptyState
                      title="Outside monitored history"
                      body={
                        <>
                          No collector sessions are known at all, so this window lies entirely outside
                          monitored history. This is not a 0% coverage failure — there is simply no
                          monitoring evidence for this period.
                        </>
                      }
                    />
                  )}
                  <div className="pl-coverage__facts">
                    <div>
                      <div className="pl-coverage-fact__value">
                        {coverage.coverageRatio !== undefined ? `${(coverage.coverageRatio * 100).toFixed(1)}%` : '—'}
                      </div>
                      <div className="pl-coverage-fact__label">
                        Coverage ratio
                        {coverage.coverageRatio === undefined && ' (outside known scope)'}
                      </div>
                    </div>
                    <div>
                      <div className="pl-coverage-fact__value">{formatDuration(coverage.coveredDurationMs)}</div>
                      <div className="pl-coverage-fact__label">Covered by monitoring</div>
                    </div>
                    <div>
                      <div className="pl-coverage-fact__value">{formatDuration(coverage.uncoveredDurationMs)}</div>
                      <div className="pl-coverage-fact__label">Known gaps</div>
                    </div>
                    <div>
                      <div className="pl-coverage-fact__value">{formatDuration(coverage.outsideKnownScopeMs)}</div>
                      <div className="pl-coverage-fact__label">Outside monitored history</div>
                    </div>
                    <div>
                      <div className="pl-coverage-fact__value">{formatDuration(coverage.controllerGapDurationMs)}</div>
                      <div className="pl-coverage-fact__label">Controller gap time</div>
                    </div>
                    <div>
                      <div className="pl-coverage-fact__value">{formatDuration(coverage.collectorOfflineDurationMs)}</div>
                      <div className="pl-coverage-fact__label">Collector offline time</div>
                    </div>
                  </div>
                  {outsideScope && coverage.knownScopeStart && (
                    <div className="pl-muted pl-small" style={{ marginTop: 8 }}>
                      Part of this window predates the first known collector session (
                      {formatLocalDateTime(coverage.knownScopeStart)}). That portion is outside
                      monitored history and cannot be recovered; it is reported separately rather
                      than as a monitoring failure.
                    </div>
                  )}
                </Section>

                <Section title="Timeline" sub="Proportional view of the requested window">
                  {segments.length === 0 ? (
                    <div className="pl-muted pl-small">No timeline could be derived for this window.</div>
                  ) : (
                    <>
                      <div className="pl-timeline" role="img" aria-label="Monitoring coverage timeline">
                        {segments.map((seg, idx) => (
                          <span
                            key={idx}
                            className={`pl-timeline__seg pl-timeline__seg--${seg.kind === 'mixed' ? 'collector' : seg.kind}`}
                            style={{ width: `${Math.max(0.4, ((seg.to - seg.from) / totalSpan) * 100)}%` }}
                            title={seg.title}
                          />
                        ))}
                      </div>
                      <div className="pl-legend" style={{ marginTop: 8 }}>
                        <span className="pl-legend__item">
                          <span className="pl-legend__swatch" style={{ background: 'var(--pl-surface-inset)' }} />
                          Covered
                        </span>
                        <span className="pl-legend__item">
                          <span className="pl-legend__swatch" style={{ background: 'repeating-linear-gradient(-45deg, var(--pl-status-gap-soft), var(--pl-status-gap-soft) 3px, var(--pl-status-gap) 3px, var(--pl-status-gap) 4px)' }} />
                          Controller gap
                        </span>
                        <span className="pl-legend__item">
                          <span className="pl-legend__swatch" style={{ background: 'var(--pl-status-offline)', opacity: 0.65 }} />
                          Collector offline
                        </span>
                        <span className="pl-legend__item">
                          <span className="pl-legend__swatch" style={{ background: 'repeating-linear-gradient(90deg, transparent, transparent 2px, var(--pl-border-muted) 2px, var(--pl-border-muted) 3px)' }} />
                          Outside monitored history
                        </span>
                      </div>
                    </>
                  )}
                </Section>

                <Section
                  title="Monitoring gaps"
                  sub={
                    gaps.length === 0
                      ? undefined
                      : 'Estimated physical bytes for gap intervals are only available as window-level aggregates (see Overview evidence trust), not per gap'
                  }
                >
                  {gaps.length === 0 ? (
                    <div className="pl-muted pl-small">
                      No monitoring gaps recorded in this window. Continuous observation does not
                      guarantee every short connection was captured — snapshot sampling limits are
                      expressed through residual accounting, not gaps.
                    </div>
                  ) : (
                    <table className="pl-table" aria-label="Monitoring gaps" style={{ tableLayout: 'auto' }}>
                      <thead>
                        <tr>
                          <th style={{ width: 140 }}>Type</th>
                          <th style={{ width: 150 }}>Start</th>
                          <th style={{ width: 150 }}>End</th>
                          <th className="pl-num" style={{ width: 80 }}>Duration</th>
                          <th>Reason</th>
                          <th style={{ width: 170 }}></th>
                        </tr>
                      </thead>
                      <tbody>
                        {gaps.map((g, idx) => (
                          <GapRow key={idx} gap={g} onInspect={() => inspectAroundGap(g.startedAt, g.endedAt)} />
                        ))}
                      </tbody>
                    </table>
                  )}
                  <div className="pl-muted pl-small" style={{ marginTop: 8, display: 'flex', gap: 6, alignItems: 'center' }}>
                    <IconArrowRight />
                    Inspecting around a gap opens History with a window around the gap boundaries.
                    ProxyLens cannot reconstruct connection evidence inside a gap.
                  </div>
                </Section>
              </>
            ) : null}
          </div>
        </div>
      </div>
    </PageGate>
  );
};
