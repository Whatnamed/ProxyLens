import React from 'react';
import { CoverageSummary, MergedGap } from '../api/types';
import { formatDuration } from '../utils/format';
import { formatLocalShort } from '../utils/time';

/**
 * Monitoring coverage timeline.
 *
 * PRODUCT.md scenario D requires that a period where the collector was not running is
 * shown as a monitoring gap, never disguised as "unknown traffic". A timeline is the only
 * representation that makes the location of the blindness legible: the user sees *when*
 * we were blind, not just that coverage was 83%.
 *
 * Gaps are drawn as hatched emptiness rather than a filled colour, so a gap can never be
 * mistaken for a measured quantity.
 */

/** Reasons are surfaced verbatim when unknown; an unexplained gap is a bug, so hiding it is worse. */
export function gapReasonLabel(reason: string): string {
  switch (reason) {
    case 'collector_unclean_shutdown_or_process_termination':
      return '采集器异常退出或进程被终止';
    case 'collector_stopped_or_offline_trailing':
      return '采集器已停止 / 离线（尾部未监控）';
    case 'collector_heartbeat_stale':
      return '采集器心跳超时';
    case 'controller_stream':
      return 'Controller 连接中断';
    case 'collector_session_boundary':
      return '采集会话边界';
    case '':
      return '未标注原因';
    default:
      return reason;
  }
}

export function gapSourceLabel(source: string): string {
  switch (source) {
    case 'controller_stream':
      return 'Controller 数据流';
    case 'collector_session':
    case 'collector_session_boundary':
      return '采集会话';
    case 'mixed':
      return '多来源合并';
    default:
      return source || '未知来源';
  }
}

interface Track {
  key: string;
  leftPct: number;
  widthPct: number;
  className: string;
  title: string;
}

function clamp01(v: number) {
  return Math.max(0, Math.min(1, v));
}

/**
 * Builds the visual segments. The track spans the effective scope the API actually
 * evaluated; regions outside the collector's known scope and regions in the future are
 * drawn as distinct textures because "we never watched here" and "this has not happened
 * yet" are different statements.
 */
function buildTracks(coverage: CoverageSummary): { tracks: Track[]; start: number; end: number } {
  const startStr = coverage.effectiveScopeStart ?? coverage.requestedStart;
  const endStr = coverage.effectiveScopeEnd ?? coverage.requestedEnd;

  const start = startStr ? new Date(startStr).getTime() : NaN;
  const end = endStr ? new Date(endStr).getTime() : NaN;
  if (!isFinite(start) || !isFinite(end) || end <= start) {
    return { tracks: [], start: 0, end: 0 };
  }

  const span = end - start;
  const pct = (t: number) => clamp01((t - start) / span) * 100;

  const tracks: Track[] = [];

  // Region before the collector's first known observation.
  const knownStart = coverage.knownScopeStart ? new Date(coverage.knownScopeStart).getTime() : NaN;
  if (isFinite(knownStart) && knownStart > start) {
    tracks.push({
      key: 'outside',
      leftPct: 0,
      widthPct: pct(knownStart),
      className: 'pl-tl__seg pl-tl__seg--outside',
      title: `超出监控范围：${formatDuration(knownStart - start)}（采集器在此之前没有观测记录）`,
    });
  }

  // Region after "now" inside a requested window that reaches into the future.
  const futureMs = coverage.futureDurationMs ?? 0;
  if (futureMs > 0) {
    tracks.push({
      key: 'future',
      leftPct: pct(end - futureMs),
      widthPct: (futureMs / span) * 100,
      className: 'pl-tl__seg pl-tl__seg--future',
      title: `尚未发生：${formatDuration(futureMs)}（窗口末端超出当前时间）`,
    });
  }

  // The measured gaps themselves.
  (coverage.mergedGaps ?? []).forEach((gap: MergedGap, i) => {
    const gs = new Date(gap.startedAt).getTime();
    const ge = new Date(gap.endedAt).getTime();
    if (!isFinite(gs) || !isFinite(ge) || ge <= gs) return;
    const left = pct(gs);
    const width = clamp01((ge - gs) / span) * 100;
    if (width <= 0) return;
    tracks.push({
      key: `gap-${i}`,
      leftPct: left,
      widthPct: width,
      className: 'pl-tl__seg pl-tl__seg--gap',
      title: `${formatLocalShort(gap.startedAt)} → ${formatLocalShort(gap.endedAt)}｜${gapReasonLabel(
        gap.reason
      )}｜持续 ${formatDuration(gap.durationMs)}`,
    });
  });

  return { tracks, start, end };
}

export const CoverageTimeline: React.FC<{ coverage: CoverageSummary }> = ({ coverage }) => {
  const { tracks, start, end } = buildTracks(coverage);
  if (!tracks.length && start === end) return null;

  const covered = coverage.coveredDurationMs ?? 0;
  const uncovered = coverage.uncoveredDurationMs ?? 0;

  return (
    <div className="pl-tl">
      <div className="pl-tl__track">
        <div className="pl-tl__base" />
        {tracks.map((t) => (
          <div
            key={t.key}
            className={t.className}
            style={{ left: `${t.leftPct}%`, width: `${t.widthPct}%` }}
            title={t.title}
          />
        ))}
      </div>
      <div className="pl-tl__axis">
        <span>{formatLocalShort(new Date(start).toISOString())}</span>
        <span className="pl-tl__axis-mid">
          已监控 {formatDuration(covered)} · 缺口 {formatDuration(uncovered)}
        </span>
        <span>{formatLocalShort(new Date(end).toISOString())}</span>
      </div>
      <div className="pl-tl__legend">
        <span className="pl-tl__key">
          <i className="pl-tl__swatch pl-tl__swatch--covered" />
          已监控
        </span>
        <span className="pl-tl__key">
          <i className="pl-tl__swatch pl-tl__swatch--gap" />
          监控缺口
        </span>
        <span className="pl-tl__key">
          <i className="pl-tl__swatch pl-tl__swatch--outside" />
          超出已知范围
        </span>
        <span className="pl-tl__key">
          <i className="pl-tl__swatch pl-tl__swatch--future" />
          尚未发生
        </span>
      </div>
    </div>
  );
};

export const GapList: React.FC<{ gaps: MergedGap[] }> = ({ gaps }) => {
  if (!gaps || gaps.length === 0) {
    return <div className="pl-empty-note">本窗口内没有监控缺口，采集全程在线。</div>;
  }
  return (
    <ul className="pl-gap-list">
      {gaps.map((gap, i) => (
        <li key={`${gap.startedAt}-${i}`} className="pl-gap-list__item">
          <span className="pl-gap-list__bar" aria-hidden="true" />
          <div className="pl-gap-list__when">
            <span className="pl-gap-list__range">
              {formatLocalShort(gap.startedAt)} → {formatLocalShort(gap.endedAt)}
            </span>
            <span className="pl-gap-list__dur">{formatDuration(gap.durationMs)}</span>
          </div>
          <div className="pl-gap-list__why">
            <span className="pl-gap-list__reason">{gapReasonLabel(gap.reason)}</span>
            <span className="pl-gap-list__source">{gapSourceLabel(gap.source)}</span>
          </div>
        </li>
      ))}
    </ul>
  );
};
