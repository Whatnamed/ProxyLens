/**
 * The integrity layer.
 *
 * This module is the reason ProxyLens is not just another traffic counter.
 *
 * PRODUCT.md 3.4 / 3.5 and ARCHITECTURE.md 5 require the tool to be explicit about the
 * difference between "we measured this" and "we were blind". A dashboard that silently
 * adds up whatever it has and prints a total is lying by omission. Every number in the
 * UI is therefore accompanied by these signals, which answer:
 *
 *   - Were we actually watching for the whole window?   (monitoring coverage)
 *   - Did every observed connection get a full story?   (attribution completeness)
 *   - How much physical traffic did polling never see?  (sampling residual)
 *   - Is what I am looking at up to date?               (accounting freshness)
 *
 * None of these are computed from anything other than the values the Go Query API returns.
 * This is presentation of server-side truth, not a second opinion implemented in TypeScript.
 */

import type { AccountingFreshness, CoverageSummary, UsageSummary } from '../api/types';

export type TrustLevel = 'healthy' | 'partial' | 'degraded' | 'unavailable';

export interface IntegritySignal {
  id: 'coverage' | 'attribution' | 'residual' | 'freshness';
  label: string;
  /** Short human-readable value, e.g. "96.2%" or "4 events behind". */
  value: string;
  /** Longer explanation shown on hover / in the detail list. */
  detail: string;
  level: TrustLevel;
}

/** Guard against dividing by zero when a window has no traffic at all. */
function ratio(part: number, whole: number): number | null {
  if (whole <= 0) return null;
  return Math.max(0, Math.min(1, part / whole));
}

/**
 * Monitoring coverage.
 * `coverageRatio` is intentionally nullable: the API returns null when the requested
 * window lies outside the range the collector has ever known about. That is a different
 * statement from "0% covered", and the UI must not collapse the two.
 */
export function coverageState(coverage: CoverageSummary | undefined | null): {
  level: TrustLevel;
  ratio: number | null;
  outsideKnownScope: boolean;
  label: string;
} {
  if (!coverage) {
    return {
      level: 'unavailable',
      ratio: null,
      outsideKnownScope: false,
      label: '无覆盖数据',
    };
  }

  const outsideKnownScope = (coverage.outsideKnownScopeMs ?? 0) > 0;
  const raw = coverage.coverageRatio;

  if (raw === null || raw === undefined) {
    return {
      level: 'unavailable',
      ratio: null,
      outsideKnownScope,
      label: outsideKnownScope ? '超出监控范围' : '覆盖率无法计算',
    };
  }

  const ratioValue = Math.max(0, Math.min(1, raw));
  let level: TrustLevel = 'healthy';
  if (ratioValue < 0.5) level = 'degraded';
  else if (ratioValue < 0.95) level = 'partial';

  return {
    level,
    ratio: ratioValue,
    outsideKnownScope,
    label: `${(ratioValue * 100).toFixed(1)}% 已监控`,
  };
}

/**
 * Attribution completeness: how much of the physically observed traffic carries a full
 * causal story (process -> target -> rule -> chain). The remainder is `missingAttribution`,
 * which must be shown as "observed but unexplained", never folded into an unknown bucket.
 */
export function attributionState(summary: UsageSummary | null | undefined): {
  level: TrustLevel;
  ratio: number | null;
  missingBytes: number;
  label: string;
} {
  if (!summary) return { level: 'unavailable', ratio: null, missingBytes: 0, label: '—' };

  const rawObserved = (summary.rawObservedUpload ?? 0) + (summary.rawObservedDownload ?? 0);
  const missing = (summary.missingAttributionUpload ?? 0) + (summary.missingAttributionDownload ?? 0);

  if (rawObserved <= 0) {
    return { level: 'unavailable', ratio: null, missingBytes: 0, label: '窗口内无流量' };
  }

  const attributed = Math.max(0, rawObserved - missing);
  const r = ratio(attributed, rawObserved) ?? 0;

  let level: TrustLevel = 'healthy';
  if (r < 0.8) level = 'degraded';
  else if (missing > 0) level = 'partial';

  return {
    level,
    ratio: r,
    missingBytes: missing,
    label: `${(r * 100).toFixed(1)}% 已归因`,
  };
}

/**
 * Sampling residual: the physical traffic that snapshot polling could never attribute to a
 * connection, because short-lived connections start and end between two snapshots.
 * ARCHITECTURE.md 4.2 establishes this as a physical limit, not a bug. Showing it turns an
 * invisible error into a declared measurement uncertainty.
 */
export function residualState(summary: UsageSummary | null | undefined): {
  level: TrustLevel;
  ratio: number | null;
  residualBytes: number;
  label: string;
} {
  if (!summary) return { level: 'unavailable', ratio: null, residualBytes: 0, label: '—' };

  const residual = (summary.samplingResidualUpload ?? 0) + (summary.samplingResidualDownload ?? 0);
  const unique = (summary.uniqueObservedUpload ?? 0) + (summary.uniqueObservedDownload ?? 0);
  const denominator = unique + residual;

  if (denominator <= 0) {
    return { level: 'healthy', ratio: null, residualBytes: residual, label: '无残差' };
  }

  const r = residual / denominator;
  let level: TrustLevel = 'healthy';
  if (r > 0.15) level = 'degraded';
  else if (r > 0.02) level = 'partial';

  return {
    level,
    ratio: r,
    residualBytes: residual,
    label: `${(r * 100).toFixed(2)}% 未被采样`,
  };
}

/** Accounting freshness: how far behind the raw event journal the current numbers are. */
export function freshnessState(freshness: AccountingFreshness | undefined | null): {
  level: TrustLevel;
  isFresh: boolean;
  lagEvents: number;
  label: string;
} {
  if (!freshness) {
    return { level: 'unavailable', isFresh: false, lagEvents: 0, label: '未知' };
  }
  const lag = freshness.lagEvents ?? 0;
  return {
    level: freshness.isFresh ? 'healthy' : 'partial',
    isFresh: !!freshness.isFresh,
    lagEvents: lag,
    label: freshness.isFresh ? '已同步' : `落后 ${lag.toLocaleString()} 个事件`,
  };
}

/**
 * Roll the individual signals into one headline trust statement.
 * The window is only as trustworthy as its weakest signal.
 */
export function overallTrust(
  summary: UsageSummary | null | undefined,
  freshness: AccountingFreshness | undefined | null
): { level: TrustLevel; headline: string; detail: string } {
  const cov = coverageState(summary?.coverage);
  const attr = attributionState(summary);
  const res = residualState(summary);
  const fresh = freshnessState(freshness);

  const levels: TrustLevel[] = [cov.level, attr.level, res.level, fresh.level];
  const severity: Record<TrustLevel, number> = {
    degraded: 3,
    partial: 2,
    unavailable: 1,
    healthy: 0,
  };

  // Only signals that carry actual data should degrade the headline;
  // "unavailable" on an empty window is a legitimate quiet state, not a fault.
  const actionable: TrustLevel[] = levels.filter((l) => l === 'degraded' || l === 'partial');
  const worst = actionable.sort((a, b) => severity[b] - severity[a])[0] ?? 'healthy';

  const reasons: string[] = [];
  if (cov.level === 'degraded' || cov.level === 'partial') {
    reasons.push(
      cov.ratio === null
        ? '窗口部分区间超出采集器的已知监控范围'
        : `窗口内仅有 ${(cov.ratio * 100).toFixed(1)}% 的时间处于监控状态`
    );
  }
  if (attr.level === 'degraded' || attr.level === 'partial') {
    reasons.push('部分已观测流量缺少完整因果归因');
  }
  if (res.level === 'degraded' || res.level === 'partial') {
    reasons.push('快照轮询遗漏了部分物理流量');
  }
  if (fresh.level === 'partial') {
    reasons.push(`核算结果落后事件日志 ${fresh.lagEvents.toLocaleString()} 个事件`);
  }

  const headline =
    worst === 'healthy'
      ? '本窗口数据完整'
      : worst === 'partial'
        ? '数据可用但不完整'
        : '数据存在实质性缺失';

  return {
    level: worst,
    headline,
    detail: reasons.length > 0 ? reasons.join('；') : '覆盖、归因与时效均无异常。',
  };
}

/** Build the signal list rendered in the overview trust strip. */
export function integritySignals(
  summary: UsageSummary | null | undefined,
  freshness: AccountingFreshness | undefined | null
): IntegritySignal[] {
  const cov = coverageState(summary?.coverage);
  const attr = attributionState(summary);
  const res = residualState(summary);
  const fresh = freshnessState(freshness);

  return [
    {
      id: 'coverage',
      label: '监控覆盖',
      value: cov.label,
      detail:
        cov.ratio === null
          ? '所选窗口超出了采集器掌握的时间范围，因此无法给出覆盖率百分比。'
          : '所选窗口内采集器实际处于观测状态的时间占比。缺口时间不会被伪装成未知流量。',
      level: cov.level,
    },
    {
      id: 'attribution',
      label: '因果归因',
      value: attr.label,
      detail:
        attr.ratio === null
          ? '本窗口内没有观测到任何流量。'
          : '已观测字节中具备完整「进程 → 目标 → 规则 → 代理链」链路的比例。其余部分作为「已观测但未归因」保留，绝不被隐藏。',
      level: attr.level,
    },
    {
      id: 'residual',
      label: '采样盲区',
      value: res.label,
      detail:
        '在两次连接快照之间开始并结束的物理流量。这是快照轮询的固有上限，ProxyLens 选择显式呈现它，而不是悄悄并入总量。',
      level: res.level,
    },
    {
      id: 'freshness',
      label: '核算时效',
      value: fresh.label,
      detail:
        '原始事件日志与最近一次已完成核算之间的距离。非数值表示最新事件尚未反映在上方数据中。',
      level: fresh.level,
    },
  ];
}
