import React from 'react';
import { QueryApiClient } from '../../api/client';
import { MetaResponse, CoverageSummary, MergedGap } from '../../api/types';
import { useAuditContext, useLocale } from '../../state/AuditContext';
import { useCoverageQuery } from '../../api/queries';
import { PageGate } from '../common/PageGate';
import { EmptyState, ErrorState, Section, SkeletonRows, StatusIndicator } from '../../components/ui/primitives';
import { TimeRangeControl } from '../../components/audit/AuditContextBar';
import { formatLocalDateTime } from '../../utils/time';
import { classifyGapSources, GapProvenance } from '../../utils/coverage';
import { IconArrowRight } from '../../components/ui/icons';

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

const gapProvenance = (g: MergedGap): GapProvenance => classifyGapSources(g.sources ?? [g.source]);

function provenanceLabel(kind: GapProvenance['kind'], t: (key: string) => string): string {
  if (kind === 'controller') return t('coverage.legendController');
  if (kind === 'mixed') return t('coverage.legendMixed');
  return t('coverage.legendCollector');
}

interface TimelineSegment {
  kind: 'covered' | 'controller' | 'collector' | 'mixed' | 'outside';
  from: number;
  to: number;
  title: string;
}

function buildSegments(
  coverage: CoverageSummary,
  windowStart: number,
  windowEnd: number,
  locale: 'en' | 'zh-CN',
  t: (key: string, vars?: Record<string, string | number>) => string,
): TimelineSegment[] {
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
      title: t('coverage.outsideSegment'),
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
        segments.push({ kind: 'covered', from: cursor, to: gs, title: t('coverage.continuous') });
      }
      const src = gapProvenance(g);
      const reason = g.reason || g.reasons?.join('; ') || t('coverage.noReason');
      const from = formatLocalDateTime(g.startedAt, locale);
      const to = formatLocalDateTime(g.endedAt, locale);
      const titleKey = src.kind === 'controller' ? 'coverage.controllerTitle' : src.kind === 'mixed' ? 'coverage.mixedTitle' : 'coverage.collectorTitle';
      segments.push({
        kind: src.kind,
        from: gs,
        to: Math.max(ge, gs + (windowEnd - windowStart) * 0.004),
        title: t(titleKey, { reason, from, to }),
      });
      cursor = Math.max(cursor, ge);
    }
    if (cursor < Math.min(effEnd, windowEnd)) {
      segments.push({ kind: 'covered', from: cursor, to: Math.min(effEnd, windowEnd), title: t('coverage.continuous') });
    }
  }

  return segments;
}

const GapRow: React.FC<{ gap: MergedGap; onInspect: () => void }> = ({ gap, onInspect }) => {
  const { locale, t } = useLocale();
  const src = gapProvenance(gap);
  return (
    <tr>
      <td>
        <StatusIndicator
          kind={src.kind === 'controller' ? 'gap' : 'offline'}
          label={provenanceLabel(src.kind, t)}
          title={t('coverage.provenanceSource', { source: (gap.sources ?? [gap.source]).join(', ') })}
        />
      </td>
      <td><span className="pl-mono pl-small">{formatLocalDateTime(gap.startedAt, locale)}</span></td>
      <td><span className="pl-mono pl-small">{formatLocalDateTime(gap.endedAt, locale)}</span></td>
      <td className="pl-cell-num">{formatDuration(gap.durationMs, t)}</td>
      <td>
        <span className="pl-truncate" style={{ display: 'block' }} title={(gap.reasons ?? [gap.reason]).join('; ')}>
          {gap.reason || t('coverage.noReason')}
        </span>
      </td>
      <td>
        <button className="pl-btn pl-btn--quiet pl-btn--compact" onClick={onInspect}>
          {t('coverage.inspectGap')}
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
  const { locale, t } = useLocale();
  const { resolvedRange, inspectAroundGap } = useAuditContext();
  const { from, to } = resolvedRange;
  const coverageQ = useCoverageQuery(client, from, to);
  const coverage = coverageQ.data;

  const windowStart = new Date(from).getTime();
  const windowEnd = new Date(to).getTime();
  const segments = coverage ? buildSegments(coverage, windowStart, windowEnd, locale, t) : [];
  const totalSpan = Math.max(1, windowEnd - windowStart);
  const outsideScope = (coverage?.outsideKnownScopeMs ?? 0) > 0;
  const gaps = coverage?.mergedGaps ?? [];

  return (
    <PageGate client={client} sessionError={sessionError} meta={meta}>
      <div className="pl-page">
        <div className="pl-page__header">
          <h1 className="pl-page__title">{t('coverage.title')}</h1>
          <div className="pl-page__header-right">
            <span className="pl-muted pl-small">{t('coverage.headerSub')}</span>
          </div>
        </div>

        <div className="pl-context-bar">
          <TimeRangeControl />
          <div className="pl-context-bar__sep" />
          <span className="pl-muted pl-small">{t('coverage.contextSub')}</span>
        </div>

        <div className="pl-page__scroll">
          <div className="pl-coverage">
            {coverageQ.isLoading ? (
              <SkeletonRows rows={8} />
            ) : coverageQ.isError ? (
              <ErrorState
                title={t('coverage.queryFailed')}
                body={String((coverageQ.error as Error)?.message ?? coverageQ.error)}
              />
            ) : coverage ? (
              <>
                <Section title={t('coverage.summary')} sub={t('coverage.requestedWindow', { from: formatLocalDateTime(from, locale), to: formatLocalDateTime(to, locale) })}>
                  {outsideScope && !coverage.knownScopeStart && (
                    <EmptyState
                      title={t('coverage.outsideTitle')}
                      body={
                        <>{t('coverage.outsideBody')}</>
                      }
                    />
                  )}
                  <div className="pl-coverage__facts">
                    <div>
                      <div className="pl-coverage-fact__value">
                        {coverage.coverageRatio !== undefined ? `${(coverage.coverageRatio * 100).toFixed(1)}%` : '—'}
                      </div>
                      <div className="pl-coverage-fact__label">
                        {t('coverage.ratio')}
                        {coverage.coverageRatio === undefined && ` (${t('coverage.outsideKnownScope')})`}
                      </div>
                    </div>
                    <div>
                      <div className="pl-coverage-fact__value">{formatDuration(coverage.coveredDurationMs, t)}</div>
                      <div className="pl-coverage-fact__label">{t('coverage.covered')}</div>
                    </div>
                    <div>
                      <div className="pl-coverage-fact__value">{formatDuration(coverage.uncoveredDurationMs, t)}</div>
                      <div className="pl-coverage-fact__label">{t('coverage.knownGaps')}</div>
                    </div>
                    <div>
                      <div className="pl-coverage-fact__value">{formatDuration(coverage.outsideKnownScopeMs, t)}</div>
                      <div className="pl-coverage-fact__label">{t('coverage.outsideHistory')}</div>
                    </div>
                    <div>
                      <div className="pl-coverage-fact__value">{formatDuration(coverage.controllerGapDurationMs, t)}</div>
                      <div className="pl-coverage-fact__label">{t('coverage.controllerGapTime')}</div>
                    </div>
                    <div>
                      <div className="pl-coverage-fact__value">{formatDuration(coverage.collectorOfflineDurationMs, t)}</div>
                      <div className="pl-coverage-fact__label">{t('coverage.collectorOfflineTime')}</div>
                    </div>
                  </div>
                  {outsideScope && coverage.knownScopeStart && (
                    <div className="pl-muted pl-small" style={{ marginTop: 8 }}>
                      {t('coverage.predatesSession', { time: formatLocalDateTime(coverage.knownScopeStart, locale) })}
                    </div>
                  )}
                </Section>

                <Section title={t('coverage.timeline')} sub={t('coverage.timelineSub')}>
                  {segments.length === 0 ? (
                    <div className="pl-muted pl-small">{t('coverage.noTimeline')}</div>
                  ) : (
                    <>
                      <div className="pl-timeline" role="img" aria-label={t('coverage.timelineAria')}>
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
                          <span className="pl-legend__label pl-compact-label">{t('coverage.legendCovered')}</span>
                        </span>
                        <span className="pl-legend__item">
                          <span className="pl-legend__swatch" style={{ background: 'repeating-linear-gradient(-45deg, var(--pl-status-gap-soft), var(--pl-status-gap-soft) 3px, var(--pl-status-gap) 3px, var(--pl-status-gap) 4px)' }} />
                          <span className="pl-legend__label pl-compact-label">{t('coverage.legendController')}</span>
                        </span>
                        <span className="pl-legend__item">
                          <span className="pl-legend__swatch" style={{ background: 'var(--pl-status-offline)', opacity: 0.65 }} />
                          <span className="pl-legend__label pl-compact-label">{t('coverage.legendCollector')}</span>
                        </span>
                        <span className="pl-legend__item">
                          <span className="pl-legend__swatch" style={{ background: 'repeating-linear-gradient(90deg, transparent, transparent 2px, var(--pl-border-muted) 2px, var(--pl-border-muted) 3px)' }} />
                          <span className="pl-legend__label pl-compact-label">{t('coverage.legendOutside')}</span>
                        </span>
                      </div>
                    </>
                  )}
                </Section>

                <Section
                  title={t('coverage.gaps')}
                  sub={
                    gaps.length === 0
                      ? undefined
                      : t('coverage.gapsSub')
                  }
                >
                  {gaps.length === 0 ? (
                      <div className="pl-muted pl-small">
                      {t('coverage.noGaps')}
                    </div>
                  ) : (
                    <table className="pl-table" aria-label={t('coverage.tableAria')} style={{ tableLayout: 'auto' }}>
                      <thead>
                        <tr>
                          <th style={{ width: 140 }}>{t('coverage.type')}</th>
                          <th style={{ width: 150 }}>{t('coverage.start')}</th>
                          <th style={{ width: 150 }}>{t('coverage.end')}</th>
                          <th className="pl-num" style={{ width: 80 }}>{t('coverage.duration')}</th>
                          <th>{t('coverage.reason')}</th>
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
                    {t('coverage.inspectNote')}
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
