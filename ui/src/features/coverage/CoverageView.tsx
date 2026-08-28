import React, { useState } from 'react';
import { QueryApiClient } from '../../api/client';
import { useAudit } from '../../state/AuditContext';
import { useCoverage, useTrafficSummary } from '../../api/queries';
import { Panel, Tip, ByteValue, Badge } from '../../components/ui/primitives';
import { ErrorState, EmptyState, Notice, SkeletonLine } from '../../components/ui/states';
import { toProductError } from '../../lib/apiError';
import { buildSegments, windowAroundGap, SEGMENT_META } from '../../lib/coverage';
import { describeGapSource } from '../../lib/semantics';
import { formatDuration, formatPercent } from '../../utils/format';
import { formatLocalDateTimeCompact } from '../../utils/time';
import { IconArrowRight, IconExternal } from '../../components/ui/icons';
import { CoverageTimeline, CoverageLegend } from './CoverageTimeline';

/**
 * Coverage is a data-trust surface, not a system-health dashboard.
 *
 * The question it answers is: can the numbers in the selected window be
 * trusted? It is deliberately independent of route classification, because
 * monitoring gaps affect every routing class equally.
 */

interface Props {
  client: QueryApiClient | null;
}

const Figure: React.FC<{
  label: string;
  value: string;
  sub?: string;
  variant?: 'ratio' | 'undefined';
  color?: string;
}> = ({ label, value, sub, variant, color }) => (
  <div className={`cov-figure ${variant === 'ratio' ? 'cov-figure--ratio' : ''} ${variant === 'undefined' ? 'cov-figure--undefined' : ''}`}>
    <div className="cov-figure-label">{label}</div>
    <div className="cov-figure-value" style={color ? { color } : undefined}>
      {value}
    </div>
    {sub && <div className="cov-figure-sub">{sub}</div>}
  </div>
);

export const CoverageView: React.FC<Props> = ({ client }) => {
  const { range, nonce, navigate, setWindow } = useAudit();
  const [selectedGap, setSelectedGap] = useState<number | null>(null);
  const coverage = useCoverage(client, range, nonce);
  const summary = useTrafficSummary(client, range, nonce);

  const data = coverage.data;
  const segments = buildSegments(data);
  const gaps = data?.mergedGaps ?? [];

  if (coverage.isError) {
    return (
      <div style={{ padding: 'var(--sp-8)', display: 'flex', justifyContent: 'center' }}>
        <div style={{ width: '100%', maxWidth: 620 }}>
          <ErrorState error={toProductError(coverage.error)} onRetry={() => coverage.refetch()} />
        </div>
      </div>
    );
  }

  const ratioUndefined = data !== undefined && data.coverageRatio === undefined;
  const hasKnownScope = !!data?.knownScopeStart;

  const gapPhysical =
    (summary.data?.controllerGapPhysicalUpload ?? 0) +
    (summary.data?.controllerGapPhysicalDownload ?? 0);

  const inspectAroundGap = (index: number) => {
    const gap = gaps[index];
    if (!gap) return;
    const w = windowAroundGap(gap);
    setWindow('custom', w);
    navigate('history');
  };

  return (
    <div className="coverage">
      {/* ---- A. Coverage summary ---- */}
      {coverage.isLoading && !data ? (
        <div className="cov-summary">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="cov-figure">
              <SkeletonLine width="60%" height={9} />
              <div style={{ marginTop: 12 }}>
                <SkeletonLine width="80%" height={22} />
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="cov-summary">
          {ratioUndefined ? (
            <Figure
              variant="undefined"
              label="Coverage ratio"
              value={hasKnownScope ? 'Undefined' : 'Outside monitored history'}
              sub={
                hasKnownScope
                  ? 'The selected window has no effective monitored scope.'
                  : 'No Collector session has ever been recorded, so no coverage claim is possible.'
              }
            />
          ) : (
            <Figure
              variant="ratio"
              label="Coverage ratio"
              value={formatPercent(data?.coverageRatio, 2)}
              sub={`of ${formatDuration(
                (data?.coveredDurationMs ?? 0) + (data?.uncoveredDurationMs ?? 0)
              )} effective scope`}
              color={
                (data?.coverageRatio ?? 0) >= 0.995
                  ? 'var(--ok)'
                  : (data?.coverageRatio ?? 0) >= 0.9
                    ? 'var(--warn)'
                    : 'var(--danger)'
              }
            />
          )}

          <Figure
            label="Covered"
            value={formatDuration(data?.coveredDurationMs ?? 0)}
            sub="actively collected"
          />
          <Figure
            label="Uncovered"
            value={formatDuration(data?.uncoveredDurationMs ?? 0)}
            sub={`${gaps.length} merged gap(s)`}
            color={(data?.uncoveredDurationMs ?? 0) > 0 ? 'var(--warn)' : undefined}
          />
          <Figure
            label="Outside history"
            value={formatDuration(data?.outsideKnownScopeMs ?? 0)}
            sub="never monitored — not a gap"
          />
        </div>
      )}

      {/* ---- B. Coverage timeline ---- */}
      <Panel
        title="Coverage timeline"
        note="Select a gap to highlight it and reveal surrounding evidence"
      >
        {coverage.isLoading && !data ? (
          <SkeletonLine height={34} />
        ) : segments.length === 0 ? (
          <EmptyState
            glyph="info"
            title="No timeline available"
            desc="Coverage data could not be resolved for the selected window."
          />
        ) : (
          <CoverageTimeline
            segments={segments}
            selectedGapIndex={selectedGap}
            onSelectGap={setSelectedGap}
          />
        )}
        <div style={{ marginTop: 'var(--sp-5)' }}>
          <CoverageLegend />
        </div>
        {(data?.outsideKnownScopeMs ?? 0) > 0 && (
          <div style={{ marginTop: 'var(--sp-5)' }}>
            <Notice tone="info">
              Part of this window predates the first recorded Collector session. That interval is
              shown as <strong>outside monitored history</strong>, not as a monitoring gap — no
              coverage claim is made about it.
            </Notice>
          </div>
        )}
      </Panel>

      {/* ---- Gap source split ---- */}
      <Panel title="Uncovered time by source" note="Two independent interruption causes">
        <div className="trust-list">
          <div className="trust-row">
            <span className="trust-label">
              <span className="trust-dot" style={{ background: SEGMENT_META.controller_stream.color }} />
              <Tip content={SEGMENT_META.controller_stream.explanation}>
                <span style={{ borderBottom: '1px dotted var(--border-strong)', cursor: 'help' }}>
                  Controller stream
                </span>
              </Tip>
            </span>
            <span className="trust-value">{formatDuration(data?.controllerGapDurationMs ?? 0)}</span>
          </div>
          <div className="trust-row">
            <span className="trust-label">
              <span
                className="trust-dot"
                style={{ background: SEGMENT_META.collector_session_boundary.color }}
              />
              <Tip content={SEGMENT_META.collector_session_boundary.explanation}>
                <span style={{ borderBottom: '1px dotted var(--border-strong)', cursor: 'help' }}>
                  Collector offline
                </span>
              </Tip>
            </span>
            <span className="trust-value">
              {formatDuration(data?.collectorOfflineDurationMs ?? 0)}
            </span>
          </div>
          <div className="trust-row">
            <span className="trust-label">
              <span className="trust-dot" style={{ background: 'var(--warn)' }} />
              <Tip
                content="Physical traffic estimated to have occurred during controller-stream gaps, derived from global counter deltas. This is a single window-level estimate: Query API v1 does not report it per gap."
              >
                <span style={{ borderBottom: '1px dotted var(--border-strong)', cursor: 'help' }}>
                  Gap traffic estimate (global)
                </span>
              </Tip>
            </span>
            <span className="trust-value">
              <ByteValue bytes={gapPhysical} estimated precision="interval_derived" />
            </span>
          </div>
        </div>
      </Panel>

      {/* ---- C. Gap list ---- */}
      <Panel
        title="Monitoring gaps"
        note={`${gaps.length} gap(s) in the selected window`}
        flush
      >
        {coverage.isLoading && !data ? (
          <div style={{ padding: 'var(--sp-5)' }}>
            <SkeletonLine height={90} />
          </div>
        ) : gaps.length === 0 ? (
          <EmptyState
            glyph="empty"
            title="No monitoring gaps"
            desc="Every moment inside the effective monitored scope was covered by the Collector. If part of the window lies outside known monitored history, that is shown separately above."
          />
        ) : (
          <div className="gap-table">
            <div
              className="gap-row"
              style={{
                background: 'var(--surface-2)',
                borderBottom: '1px solid var(--border)',
                cursor: 'default'
              }}
            >
              <span className="label">Source</span>
              <span className="label">Start</span>
              <span className="label">End</span>
              <span className="label">Duration</span>
              <span className="label">Reason</span>
              <span className="label" style={{ textAlign: 'right' }}>Est. traffic</span>
            </div>

            {gaps.map((g, i) => {
              const src = describeGapSource(g.source);
              const isSelected = selectedGap === i;
              return (
                <div
                  key={`${g.source}-${g.startedAt}-${i}`}
                  className={`gap-row ${isSelected ? 'is-selected' : ''}`}
                  onClick={() => setSelectedGap(isSelected ? null : i)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      setSelectedGap(isSelected ? null : i);
                    }
                  }}
                  style={{ cursor: 'pointer' }}
                >
                  <span className="gap-source">
                    <span
                      className="gap-swatch"
                      style={{
                        background:
                          g.source === 'collector_session_boundary'
                            ? 'repeating-linear-gradient(135deg, rgba(255,255,255,.35) 0 2px, transparent 2px 5px), var(--cov-collector)'
                            : SEGMENT_META.controller_stream.color
                      }}
                    />
                    <Badge tone={src.tone}>{src.label}</Badge>
                  </span>
                  <span className="gap-time" title={formatLocalDateTimeCompact(g.startedAt)}>
                    {formatLocalDateTimeCompact(g.startedAt)}
                  </span>
                  <span className="gap-time" title={formatLocalDateTimeCompact(g.endedAt)}>
                    {formatLocalDateTimeCompact(g.endedAt)}
                  </span>
                  <span className="gap-duration">{formatDuration(g.durationMs)}</span>
                  <Tip content={src.explanation}>
                    <span className="gap-reason" title={g.reason || src.explanation}>
                      {g.reason || '—'}
                    </span>
                  </Tip>
                  <span className="gap-estimate" style={{ textAlign: 'right' }}>
                    <Tip
                      align="right"
                      content="Query API v1 does not report estimated physical bytes per individual gap. Only a window-level estimate for controller-stream gaps is available, shown above."
                    >
                      <span className="dimmer" style={{ cursor: 'help' }}>
                        not reported
                      </span>
                    </Tip>
                    <span className="gap-actions" style={{ marginLeft: 'var(--sp-4)' }}>
                      <button
                        className="btn btn--sm"
                        onClick={(e) => {
                          e.stopPropagation();
                          inspectAroundGap(i);
                        }}
                        title="Open History around this gap boundary"
                      >
                        <IconExternal size={11} />
                        Inspect around
                        <IconArrowRight size={11} />
                      </button>
                    </span>
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </Panel>

      {gaps.length > 0 && (
        <Notice tone="neutral">
          Connections that occurred inside an unobserved gap cannot be reconstructed — the
          underlying evidence does not exist. <strong>Inspect around</strong> opens History at the
          gap boundary so you can examine what happened immediately before and after the
          interruption.
        </Notice>
      )}
    </div>
  );
};
