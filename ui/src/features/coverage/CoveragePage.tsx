import React from 'react';
import { QueryApiClient } from '../../api/client';
import { MetaResponse, CoverageSummary, MergedGap } from '../../api/types';
import { useAuditContext, useLocale } from '../../state/AuditContext';
import { useCoverageQuery, useSummaryQuery } from '../../api/queries';
import { PageGate } from '../common/PageGate';
import { EmptyState, ErrorState, Section, SkeletonRows, StatusIndicator } from '../../components/ui/primitives';
import { TimeRangeControl } from '../../components/audit/AuditContextBar';
import { formatLocalDateTime } from '../../utils/time';
import { formatBytes } from '../../utils/format';
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

export function gapIdentity(gap: Pick<MergedGap, 'source' | 'sources' | 'startedAt' | 'endedAt'>): string {
  const sources = (gap.sources ?? [gap.source]).filter(Boolean).slice().sort().join('+');
  return `${sources}|${gap.startedAt}|${gap.endedAt}`;
}

export interface TimelineSegment {
  kind: 'covered' | 'controller' | 'collector' | 'mixed' | 'outside' | 'future';
  from: number;
  to: number;
  title: string;
  gapId?: string;
}

export function buildSegments(
  coverage: CoverageSummary,
  windowStart: number,
  windowEnd: number,
  locale: 'en' | 'zh-CN',
  t: (key: string, vars?: Record<string, string | number>) => string,
): TimelineSegment[] {
  const segments: TimelineSegment[] = [];
  if (windowEnd <= windowStart) return segments;

  const futureMs = Math.max(0, coverage.futureDurationMs ?? 0);
  const futureStart = futureMs > 0 ? Math.max(windowStart, windowEnd - futureMs) : windowEnd;
  const effectiveWindowEnd = Math.min(windowEnd, futureStart);

  // If the entire window is in the future
  if (futureStart <= windowStart) {
    segments.push({
      kind: 'future',
      from: windowStart,
      to: windowEnd,
      title: t('coverage.futureSegment'),
    });
    return segments;
  }

  const effStart = coverage.effectiveScopeStart ? new Date(coverage.effectiveScopeStart).getTime() : null;
  const effEnd = coverage.effectiveScopeEnd ? new Date(coverage.effectiveScopeEnd).getTime() : null;

  // Case A: No known scope
  if (effStart === null || effEnd === null) {
    segments.push({
      kind: 'outside',
      from: windowStart,
      to: effectiveWindowEnd,
      title: t('coverage.outsideSegment'),
    });
  } else {
    // Outside monitored history: requested window before known scope
    if (effStart > windowStart) {
      segments.push({
        kind: 'outside',
        from: windowStart,
        to: Math.min(effStart, effectiveWindowEnd),
        title: t('coverage.outsideSegment'),
      });
    }

    if (effEnd > windowStart && effStart < effectiveWindowEnd) {
      const gaps = [...(coverage.mergedGaps ?? [])]
        .map((g) => ({
          g,
          start: new Date(g.startedAt).getTime(),
          end: new Date(g.endedAt).getTime(),
        }))
        .filter((x) => x.end > windowStart && x.start < effectiveWindowEnd)
        .sort((a, b) => a.start - b.start);

      let cursor = Math.max(effStart, windowStart);
      for (const { g, start, end } of gaps) {
        const gs = Math.max(start, windowStart);
        const ge = Math.min(end, effectiveWindowEnd);
        if (gs > cursor) {
          segments.push({ kind: 'covered', from: cursor, to: gs, title: t('coverage.continuous') });
        }
        const src = gapProvenance(g);
        const reason = g.reason || g.reasons?.join('; ') || t('coverage.noReason');
        const fromStr = formatLocalDateTime(g.startedAt, locale);
        const toStr = formatLocalDateTime(g.endedAt, locale);
        const titleKey =
          src.kind === 'controller'
            ? 'coverage.controllerTitle'
            : src.kind === 'mixed'
            ? 'coverage.mixedTitle'
            : 'coverage.collectorTitle';
        segments.push({
          kind: src.kind,
          from: gs,
          to: Math.max(ge, gs + (windowEnd - windowStart) * 0.004),
          title: t(titleKey, { reason, from: fromStr, to: toStr }),
          gapId: gapIdentity(g),
        });
        cursor = Math.max(cursor, ge);
      }
      if (cursor < Math.min(effEnd, effectiveWindowEnd)) {
        segments.push({
          kind: 'covered',
          from: cursor,
          to: Math.min(effEnd, effectiveWindowEnd),
          title: t('coverage.continuous'),
        });
      }
    }
  }

  // Future segment at the end if futureMs > 0
  if (futureMs > 0 && futureStart < windowEnd) {
    segments.push({
      kind: 'future',
      from: futureStart,
      to: windowEnd,
      title: t('coverage.futureSegment'),
    });
  }

  return segments;
}

const GapRow: React.FC<{
  gap: MergedGap;
  gapId: string;
  isSelected: boolean;
  isHovered: boolean;
  onSelect: () => void;
  onHover: (active: boolean) => void;
  onInspect: (e: React.MouseEvent) => void;
}> = ({ gap, gapId, isSelected, isHovered, onSelect, onHover, onInspect }) => {
  const { locale, t } = useLocale();
  const src = gapProvenance(gap);
  return (
    <tr
      data-gap-id={gapId}
      className={`pl-gap-row${isSelected ? ' pl-row--selected' : ''}${isHovered && !isSelected ? ' pl-row--hovered' : ''}`}
      onClick={onSelect}
      onMouseEnter={() => onHover(true)}
      onMouseLeave={() => onHover(false)}
      onFocus={() => onHover(true)}
      onBlur={() => onHover(false)}
      tabIndex={0}
      role="row"
      aria-selected={isSelected}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          const target = e.target as HTMLElement | null;
          if (target?.tagName === 'BUTTON') return;
          e.preventDefault();
          onSelect();
        }
      }}
    >
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
        <button
          className="pl-btn pl-btn--quiet pl-btn--compact"
          onClick={(e) => {
            e.stopPropagation();
            onInspect(e);
          }}
        >
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
  const { resolvedRange, inspectAroundGap, rangeSourceKey } = useAuditContext();
  const { from, to } = resolvedRange;
  const coverageQ = useCoverageQuery(client, from, to, rangeSourceKey);
  const summaryQ = useSummaryQuery(client, from, to, 'ALL', rangeSourceKey);
  const coverage = coverageQ.data;
  const summary = summaryQ.data;

  const gapTrafficBytes = (summary?.controllerGapPhysicalUpload ?? 0) + (summary?.controllerGapPhysicalDownload ?? 0);

  const windowStart = new Date(from).getTime();
  const windowEnd = new Date(to).getTime();
  const segments = coverage ? buildSegments(coverage, windowStart, windowEnd, locale, t) : [];
  const totalSpan = Math.max(1, windowEnd - windowStart);
  const outsideScope = (coverage?.outsideKnownScopeMs ?? 0) > 0;
  const hasFuture = (coverage?.futureDurationMs ?? 0) > 0;
  const gaps = coverage?.mergedGaps ?? [];
  const [selectedGapId, setSelectedGapId] = React.useState<string | null>(null);
  const [hoveredGapId, setHoveredGapId] = React.useState<string | null>(null);

  // Clear selection if the selected gap is no longer present in the queried mergedGaps
  React.useEffect(() => {
    if (selectedGapId && !gaps.some((g) => gapIdentity(g) === selectedGapId)) {
      setSelectedGapId(null);
    }
  }, [gaps, selectedGapId]);

  const handleToggleSelectGap = (gapId: string) => {
    setSelectedGapId((prev) => {
      const next = prev === gapId ? null : gapId;
      if (next !== null) {
        const row = document.querySelector(`[data-gap-id="${CSS.escape(gapId)}"]`);
        if (row) {
          row.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
        }
      }
      return next;
    });
  };

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
                    {coverage.futureDurationMs > 0 && (
                      <div>
                        <div className="pl-coverage-fact__value">{formatDuration(coverage.futureDurationMs, t)}</div>
                        <div className="pl-coverage-fact__label">{t('coverage.future')}</div>
                      </div>
                    )}
                    {gapTrafficBytes > 0 && (
                      <div>
                        <div className="pl-coverage-fact__value">≈ {formatBytes(gapTrafficBytes)}</div>
                        <div className="pl-coverage-fact__label" title={t('coverage.gapTrafficEstimateTitle')}>
                          {t('coverage.gapTrafficEstimate')}
                          <span className="pl-evidence-chip pl-evidence-chip--estimated" style={{ marginLeft: 6 }}>
                            <span className="pl-evidence-chip__label pl-compact-label">{t('common.estimated')}</span>
                          </span>
                        </div>
                      </div>
                    )}
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
                      <div className="pl-timeline" role="region" aria-label={t('coverage.timelineAria')}>
                        {segments.map((seg) => {
                          const isGapSeg = !!seg.gapId;
                          const isSelected = isGapSeg && seg.gapId === selectedGapId;
                          const isHovered = isGapSeg && seg.gapId === hoveredGapId;
                          const segKey = seg.gapId ? `gap-${seg.gapId}` : `${seg.kind}-${seg.from}-${seg.to}`;
                          return (
                            <span
                              key={segKey}
                              className={`pl-timeline__seg pl-timeline__seg--${seg.kind}${isGapSeg ? ' pl-timeline__seg--interactive' : ''}${isSelected ? ' pl-timeline__seg--selected' : ''}${isHovered && !isSelected ? ' pl-timeline__seg--hovered' : ''}`}
                              style={{ width: `${Math.max(0.4, ((seg.to - seg.from) / totalSpan) * 100)}%` }}
                              title={seg.title}
                              role={isGapSeg ? 'button' : undefined}
                              tabIndex={isGapSeg ? 0 : undefined}
                              aria-pressed={isGapSeg ? isSelected : undefined}
                              aria-label={isGapSeg ? seg.title : undefined}
                              onClick={isGapSeg ? () => handleToggleSelectGap(seg.gapId!) : undefined}
                              onMouseEnter={isGapSeg ? () => setHoveredGapId(seg.gapId!) : undefined}
                              onMouseLeave={isGapSeg ? () => setHoveredGapId(null) : undefined}
                              onFocus={isGapSeg ? () => setHoveredGapId(seg.gapId!) : undefined}
                              onBlur={isGapSeg ? () => setHoveredGapId(null) : undefined}
                              onKeyDown={
                                isGapSeg
                                  ? (e) => {
                                      if (e.key === 'Enter' || e.key === ' ') {
                                        e.preventDefault();
                                        handleToggleSelectGap(seg.gapId!);
                                      }
                                    }
                                  : undefined
                              }
                            />
                          );
                        })}
                      </div>
                      <div className="pl-legend" style={{ marginTop: 8 }}>
                        <span className="pl-legend__item">
                          <span className="pl-legend__swatch pl-legend__swatch--covered" />
                          <span className="pl-legend__label pl-compact-label">{t('coverage.legendCovered')}</span>
                        </span>
                        <span className="pl-legend__item">
                          <span className="pl-legend__swatch pl-legend__swatch--controller" />
                          <span className="pl-legend__label pl-compact-label">{t('coverage.legendController')}</span>
                        </span>
                        <span className="pl-legend__item">
                          <span className="pl-legend__swatch pl-legend__swatch--collector" />
                          <span className="pl-legend__label pl-compact-label">{t('coverage.legendCollector')}</span>
                        </span>
                        <span className="pl-legend__item">
                          <span className="pl-legend__swatch pl-legend__swatch--mixed" />
                          <span className="pl-legend__label pl-compact-label">{t('coverage.legendMixed')}</span>
                        </span>
                        {outsideScope && (
                          <span className="pl-legend__item">
                            <span className="pl-legend__swatch pl-legend__swatch--outside" />
                            <span className="pl-legend__label pl-compact-label">{t('coverage.legendOutside')}</span>
                          </span>
                        )}
                        {hasFuture && (
                          <span className="pl-legend__item">
                            <span className="pl-legend__swatch pl-legend__swatch--future" />
                            <span className="pl-legend__label pl-compact-label">{t('coverage.legendFuture')}</span>
                          </span>
                        )}
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
                        {gaps.map((g) => {
                          const id = gapIdentity(g);
                          return (
                            <GapRow
                              key={id}
                              gapId={id}
                              gap={g}
                              isSelected={selectedGapId === id}
                              isHovered={hoveredGapId === id}
                              onSelect={() => handleToggleSelectGap(id)}
                              onHover={(hovering) => setHoveredGapId(hovering ? id : null)}
                              onInspect={() => inspectAroundGap(g.startedAt, g.endedAt)}
                            />
                          );
                        })}
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
