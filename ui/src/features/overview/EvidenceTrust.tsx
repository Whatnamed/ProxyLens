import React from 'react';
import { CoverageSummary, UsageSummary, AccountingFreshness } from '../../api/types';
import { ByteValue, Tip } from '../../components/ui/primitives';
import { formatCount, formatPercent } from '../../utils/format';
import { formatLocalDateTimeCompact } from '../../utils/time';
import { Notice } from '../../components/ui/states';
import { IconChevronRight } from '../../components/ui/icons';

/**
 * Evidence trust sits beside the traffic totals, not on a separate status
 * page. A total without trust context is incomplete: "3.2 GiB PROXY" means
 * something very different at 99.8% coverage than it does at 62%.
 */

interface Props {
  coverage: CoverageSummary | undefined;
  routeSummary: UsageSummary | undefined;
  trafficSummary: UsageSummary | undefined;
  freshness: AccountingFreshness | undefined;
  onOpenCoverage: () => void;
}

type Tone = 'ok' | 'warn' | 'danger' | 'neutral';

const DOT: Record<Tone, string> = {
  ok: 'var(--ok)',
  warn: 'var(--warn)',
  danger: 'var(--danger)',
  neutral: 'var(--neutral)'
};

const Row: React.FC<{
  tone: Tone;
  label: string;
  explanation: React.ReactNode;
  children: React.ReactNode;
  onClick?: () => void;
}> = ({ tone, label, explanation, children, onClick }) => (
  <div className="trust-row">
    <span className="trust-label">
      <span className="trust-dot" style={{ background: DOT[tone] }} />
      <Tip content={explanation}>
        <span style={{ borderBottom: '1px dotted var(--border-strong)', cursor: 'help' }}>{label}</span>
      </Tip>
    </span>
    <span className="trust-value">
      {children}
      {onClick && (
        <button
          className="btn btn--ghost btn--icon btn--sm"
          onClick={onClick}
          title="Open Coverage"
          aria-label="Open Coverage"
        >
          <IconChevronRight size={12} />
        </button>
      )}
    </span>
  </div>
);

export const EvidenceTrust: React.FC<Props> = ({
  coverage,
  routeSummary,
  trafficSummary,
  freshness,
  onOpenCoverage
}) => {
  const uniqueTotal =
    (routeSummary?.uniqueObservedUpload ?? 0) + (routeSummary?.uniqueObservedDownload ?? 0);

  const missing =
    (routeSummary?.missingAttributionUpload ?? 0) + (routeSummary?.missingAttributionDownload ?? 0);
  const ambiguous =
    (routeSummary?.ambiguousRelayUpload ?? 0) + (routeSummary?.ambiguousRelayDownload ?? 0);
  const residual =
    (trafficSummary?.samplingResidualUpload ?? 0) + (trafficSummary?.samplingResidualDownload ?? 0);
  const gapPhysical =
    (trafficSummary?.controllerGapPhysicalUpload ?? 0) +
    (trafficSummary?.controllerGapPhysicalDownload ?? 0);

  const missingShare = uniqueTotal > 0 ? missing / uniqueTotal : 0;
  const ambiguousShare = uniqueTotal > 0 ? ambiguous / uniqueTotal : 0;

  // Coverage ratio is undefined when the whole window lies outside known
  // monitored history. That is not 0% coverage.
  const outsideHistory = coverage !== undefined && coverage.coverageRatio === undefined;
  const ratio = coverage?.coverageRatio;

  const coverageTone: Tone = outsideHistory
    ? 'neutral'
    : ratio === undefined
      ? 'neutral'
      : ratio >= 0.995
        ? 'ok'
        : ratio >= 0.9
          ? 'warn'
          : 'danger';

  const shareTone = (share: number): Tone =>
    share <= 0 ? 'ok' : share < 0.02 ? 'warn' : 'danger';

  return (
    <div className="trust-list">
      {/* ---- Coverage ---- */}
      {outsideHistory ? (
        <div className="trust-row">
          <span className="trust-label">
            <span className="trust-dot" style={{ background: DOT.neutral }} />
            Monitoring coverage
          </span>
          <span className="trust-value">
            <span style={{ color: 'var(--text-secondary)' }}>Outside monitored history</span>
            <button
              className="btn btn--ghost btn--icon btn--sm"
              onClick={onOpenCoverage}
              title="Open Coverage"
              aria-label="Open Coverage"
            >
              <IconChevronRight size={12} />
            </button>
          </span>
        </div>
      ) : (
        <Row
          tone={coverageTone}
          label="Monitoring coverage"
          explanation={
            <>
              Share of the selected window that was actually observed.
              {coverage && (
                <>
                  <div className="tip-row">
                    <span className="tip-label">Covered</span>
                    <span>{(coverage.coveredDurationMs / 60000).toFixed(0)} min</span>
                  </div>
                  <div className="tip-row">
                    <span className="tip-label">Uncovered</span>
                    <span>{(coverage.uncoveredDurationMs / 60000).toFixed(0)} min</span>
                  </div>
                </>
              )}
            </>
          }
          onClick={onOpenCoverage}
        >
          {formatPercent(ratio)}
        </Row>
      )}

      {/* ---- Freshness ---- */}
      <Row
        tone={freshness?.isFresh === false ? 'neutral' : 'ok'}
        label="Accounting freshness"
        explanation={
          <>
            Whether the latest completed accounting run has processed every journal event.
            A lag is normal while new observations are being accounted; it is not a system
            failure.
          </>
        }
      >
        {freshness ? (
          <>
            {freshness.isFresh ? (
              <span style={{ color: 'var(--ok)' }}>Fresh</span>
            ) : (
              <span style={{ color: 'var(--text-secondary)' }}>
                As of {formatLocalDateTimeCompact(freshness.completedAt)}
              </span>
            )}
            {!freshness.isFresh && (
              <span className="trust-share">{formatCount(freshness.lagEvents)} behind</span>
            )}
          </>
        ) : (
          '—'
        )}
      </Row>

      {/* ---- Missing attribution ---- */}
      <Row
        tone={shareTone(missingShare)}
        label="Missing attribution"
        explanation={
          <>
            Traffic that was observed but could not be attributed to a local process. The
            destination and routing decision may still be fully known — this is not a generic
            "unknown" bucket.
          </>
        }
      >
        <ByteValue bytes={missing} />
        {uniqueTotal > 0 && <span className="trust-share">{formatPercent(missingShare)}</span>}
      </Row>

      {/* ---- Ambiguous relay ---- */}
      <Row
        tone={shareTone(ambiguousShare)}
        label="Ambiguous relay"
        explanation={
          <>
            Relay characteristics were detected but the pair could not be confirmed. These
            bytes are retained and never deducted, so they can overlap with application-side
            traffic.
          </>
        }
      >
        <ByteValue bytes={ambiguous} />
        {uniqueTotal > 0 && <span className="trust-share">{formatPercent(ambiguousShare)}</span>}
      </Row>

      {/* ---- Sampling residual ---- */}
      <Row
        tone={residual > 0 ? 'neutral' : 'ok'}
        label="Sampling residual"
        explanation={
          <>
            The small difference between global counters and the sum of per-connection values,
            caused by sampling phase. This is a global, route-independent measure and is always
            an estimate.
          </>
        }
      >
        <ByteValue bytes={residual} estimated precision="estimated" />
      </Row>

      {/* ---- Gap physical estimate ---- */}
      <Row
        tone={gapPhysical > 0 ? 'warn' : 'ok'}
        label="Gap traffic estimate"
        explanation={
          <>
            Physical traffic estimated to have occurred during controller-stream gaps, derived
            from global counter deltas. This is an estimate, never an exact measurement, and a
            single global figure for the window rather than a per-gap breakdown.
          </>
        }
      >
        <ByteValue bytes={gapPhysical} estimated precision="interval_derived" />
      </Row>

      {outsideHistory && (
        <div style={{ marginTop: 'var(--sp-4)' }}>
          <Notice tone="info">
            The selected window is entirely outside the period ProxyLens has ever monitored. That
            is not zero coverage — no coverage claim can be made about it at all.
          </Notice>
        </div>
      )}
    </div>
  );
};
