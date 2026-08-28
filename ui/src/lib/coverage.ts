import { CoverageSummary, MergedGap } from '../api/types';

/**
 * Builds the display segments for the coverage timeline.
 *
 * The backend returns durations and gap intervals, not a pre-computed
 * timeline, so the visual is reconstructed here from authoritative fields:
 * gap intervals come straight from `mergedGaps`, and everything between them
 * inside the effective scope is covered.
 */

export type SegmentType =
  | 'covered'
  | 'controller_stream'
  | 'collector_session_boundary'
  | 'outside'
  | 'future';

export interface CoverageSegment {
  type: SegmentType;
  start: number;
  end: number;
  ms: number;
  /** Index into `mergedGaps` when this segment is a real gap. */
  gapIndex?: number;
}

export const SEGMENT_META: Record<
  SegmentType,
  { label: string; color: string; explanation: string }
> = {
  covered: {
    label: 'Covered',
    color: 'var(--cov-covered)',
    explanation: 'ProxyLens was actively collecting during this interval.'
  },
  controller_stream: {
    label: 'Controller gap',
    color: 'var(--cov-controller)',
    explanation:
      'The Mihomo External Controller stream was interrupted or the system was suspended. Connections in this interval were not observed.'
  },
  collector_session_boundary: {
    label: 'Collector offline',
    color: 'var(--cov-collector)',
    explanation:
      'The ProxyLens Collector was not running. No collection happened at all during this interval.'
  },
  outside: {
    label: 'Outside monitored history',
    color: 'var(--cov-outside)',
    explanation:
      'This interval predates the first recorded Collector session. No coverage claim can be made about it — it is not the same as a monitoring gap.'
  },
  future: {
    label: 'Not yet reached',
    color: 'var(--cov-future)',
    explanation: 'This part of the requested window is in the future.'
  }
};

function push(
  out: CoverageSegment[],
  type: SegmentType,
  start: number,
  end: number,
  gapIndex?: number
) {
  if (end > start) out.push({ type, start, end, ms: end - start, gapIndex });
}

export function buildSegments(c: CoverageSummary | undefined): CoverageSegment[] {
  if (!c || !c.requestedStart || !c.requestedEnd) return [];

  const reqStart = new Date(c.requestedStart).getTime();
  const reqEnd = new Date(c.requestedEnd).getTime();
  if (isNaN(reqStart) || isNaN(reqEnd) || reqEnd <= reqStart) return [];

  const out: CoverageSegment[] = [];

  // No Collector session has ever been recorded: nothing in this window can be
  // characterised as covered or uncovered.
  if (!c.knownScopeStart) {
    push(out, 'outside', reqStart, reqEnd);
    return out;
  }

  const knownStart = new Date(c.knownScopeStart).getTime();
  let cursor = reqStart;

  if (c.outsideKnownScopeMs > 0 && knownStart > reqStart) {
    push(out, 'outside', reqStart, Math.min(knownStart, reqEnd));
    cursor = knownStart;
  }

  // The backend clips the effective end at "now" and reports the remainder as
  // future duration.
  const futureMs = c.futureDurationMs ?? 0;
  const effEnd = Math.max(cursor, reqEnd - futureMs);

  if (effEnd <= cursor) {
    if (out.length === 0) push(out, 'outside', reqStart, reqEnd);
    return out;
  }

  const gaps = [...(c.mergedGaps ?? [])]
    .map((g, i) => ({ g, i }))
    .sort((a, b) => new Date(a.g.startedAt).getTime() - new Date(b.g.startedAt).getTime());

  for (const { g, i } of gaps) {
    const gs = new Date(g.startedAt).getTime();
    const ge = new Date(g.endedAt).getTime();
    if (isNaN(gs) || isNaN(ge)) continue;

    const clipStart = Math.max(cursor, gs);
    const clipEnd = Math.min(effEnd, ge);
    if (clipEnd <= clipStart) continue;

    // Covered run immediately preceding this gap.
    push(out, 'covered', cursor, clipStart);
    push(
      out,
      g.source === 'collector_session_boundary' ? 'collector_session_boundary' : 'controller_stream',
      clipStart,
      clipEnd,
      i
    );
    cursor = clipEnd;
  }

  push(out, 'covered', cursor, effEnd);

  if (futureMs > 0) {
    push(out, 'future', effEnd, reqEnd);
  }

  return out.filter((s) => s.ms > 0);
}

/** Total duration represented by the segments, used for proportional widths. */
export function segmentsSpan(segments: CoverageSegment[]): number {
  if (segments.length === 0) return 0;
  const start = Math.min(...segments.map((s) => s.start));
  const end = Math.max(...segments.map((s) => s.end));
  return end - start;
}

/**
 * A window around a gap so the investigator can see evidence immediately
 * before and after the interruption. Padding scales with the gap but is
 * clamped so a multi-day gap does not produce an unreadable window.
 */
export function windowAroundGap(gap: MergedGap): { from: string; to: string } {
  const start = new Date(gap.startedAt).getTime();
  const end = new Date(gap.endedAt).getTime();
  const MIN_PAD = 15 * 60_000;
  const MAX_PAD = 2 * 60 * 60_000;
  const pad = Math.min(Math.max(end - start, MIN_PAD), MAX_PAD);
  return {
    from: new Date(start - pad).toISOString(),
    to: new Date(end + pad).toISOString()
  };
}
