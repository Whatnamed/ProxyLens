import React from 'react';
import { CoverageSegment, SEGMENT_META, segmentsSpan } from '../../lib/coverage';
import { Tip } from '../../components/ui/primitives';
import { formatDuration } from '../../utils/format';
import { formatLocalDateTimeCompact } from '../../utils/time';

/**
 * The coverage timeline distinguishes four visually different conditions:
 * covered, controller gap, collector offline, and outside monitored history.
 * The last one is rendered as "not observed" rather than as a coverage gap.
 */

interface Props {
  segments: CoverageSegment[];
  selectedGapIndex: number | null;
  onSelectGap: (index: number | null) => void;
}

export const CoverageTimeline: React.FC<Props> = ({
  segments,
  selectedGapIndex,
  onSelectGap
}) => {
  const span = segmentsSpan(segments);
  if (segments.length === 0 || span === 0) {
    return (
      <div className="dimmer" style={{ fontSize: 'var(--fs-12)' }}>
        No coverage timeline can be drawn for this window.
      </div>
    );
  }

  const start = Math.min(...segments.map((s) => s.start));
  const end = Math.max(...segments.map((s) => s.end));

  return (
    <div className="cov-timeline">
      <div className={`cov-track ${selectedGapIndex !== null ? 'is-dimmed' : ''}`}>
        {segments.map((s, i) => {
          const pct = (s.ms / span) * 100;
          const meta = SEGMENT_META[s.type];
          const isGap = s.gapIndex !== undefined;
          const isSelected = isGap && s.gapIndex === selectedGapIndex;

          return (
            <Tip
              key={`${s.type}-${i}`}
              content={
                <>
                  <div style={{ fontWeight: 600, marginBottom: 4 }}>{meta.label}</div>
                  <div style={{ color: 'var(--text-secondary)', marginBottom: 4 }}>
                    {meta.explanation}
                  </div>
                  <div className="tip-row">
                    <span className="tip-label">From</span>
                    <span>{formatLocalDateTimeCompact(new Date(s.start).toISOString())}</span>
                  </div>
                  <div className="tip-row">
                    <span className="tip-label">To</span>
                    <span>{formatLocalDateTimeCompact(new Date(s.end).toISOString())}</span>
                  </div>
                  <div className="tip-row">
                    <span className="tip-label">Duration</span>
                    <span>{formatDuration(s.ms)}</span>
                  </div>
                </>
              }
            >
              <div
                className={`cov-seg cov-seg--${s.type} ${isSelected ? 'is-selected' : ''}`}
                style={{ width: `${pct}%` }}
                onClick={() => isGap ? onSelectGap(isSelected ? null : s.gapIndex!) : undefined}
                role={isGap ? 'button' : undefined}
                tabIndex={isGap ? 0 : undefined}
                onKeyDown={(e) => {
                  if (isGap && (e.key === 'Enter' || e.key === ' ')) {
                    e.preventDefault();
                    onSelectGap(isSelected ? null : s.gapIndex!);
                  }
                }}
              />
            </Tip>
          );
        })}
      </div>

      <div className="cov-axis">
        <span>{formatLocalDateTimeCompact(new Date(start).toISOString())}</span>
        <span>{formatLocalDateTimeCompact(new Date(end).toISOString())}</span>
      </div>
    </div>
  );
};

export const CoverageLegend: React.FC = () => (
  <div className="cov-legend">
    {(['covered', 'controller_stream', 'collector_session_boundary', 'outside'] as const).map(
      (t) => {
        const meta = SEGMENT_META[t];
        return (
          <span key={t} className="cov-legend-item">
            <span
              className="cov-legend-swatch"
              style={{
                background:
                  t === 'collector_session_boundary'
                    ? 'repeating-linear-gradient(135deg, rgba(255,255,255,.35) 0 2px, transparent 2px 5px), var(--cov-collector)'
                    : t === 'outside'
                      ? 'repeating-linear-gradient(45deg, rgba(255,255,255,.05) 0 3px, transparent 3px 6px), var(--cov-outside)'
                      : meta.color
              }}
            />
            {meta.label}
          </span>
        );
      }
    )}
  </div>
);
