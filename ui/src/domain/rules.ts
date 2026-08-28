/**
 * Derived rule audit flags.
 *
 * PRODUCT.md 4.3 defines "Audit Intelligence" as a Phase 4 capability, and most of those
 * checks need cross-window history (first-seen processes, route flips) that the v1 Query API
 * does not expose. This module deliberately does NOT fake those.
 *
 * What it does implement is the subset that is strictly derivable from a single rule row
 * returned by `/api/v1/analytics/top/rules` today, using declared rule semantics:
 *
 *   - MATCH fallback volume     (PRODUCT 4.3.3) — traffic that reached the default policy
 *                               because no specific rule claimed it.
 *   - Broad UDP rules           (PRODUCT 4.3.4) — NETWORK,udp style rules that sweep all UDP,
 *                               including background chatter, into the proxy.
 *   - Missing rule evidence     (PRODUCT 3.4)   — rows with no rule at all.
 *
 * Every flag carries the reason it fired, so a row is never flagged by an unexplained score.
 * These are presentation-level derivations over real API values; no backend logic is duplicated.
 */

import type { TopRuleItem } from '../api/types';

export type FlagTone = 'warn' | 'danger' | 'info';

export interface RuleFlag {
  id: 'match-fallback' | 'broad-udp' | 'missing-rule' | 'broad-wildcard';
  label: string;
  tone: FlagTone;
  reason: string;
}

const norm = (v: string | undefined | null) => (v ?? '').trim().toLowerCase();

/**
 * Rows whose bytes were reconstructed from an observation interval rather than read as an
 * exact counter delta. This is the row-level honesty marker: it tells the user the figure is
 * an apportioned estimate, not a measured value.
 */
export function estimateShare(item: {
  totalBytes?: number;
  estimatedUploadBytes?: number;
  estimatedDownloadBytes?: number;
}): number {
  const total = item.totalBytes ?? 0;
  if (total <= 0) return 0;
  const estimated = (item.estimatedUploadBytes ?? 0) + (item.estimatedDownloadBytes ?? 0);
  return Math.max(0, Math.min(1, estimated / total));
}

export function ruleAuditFlags(item: TopRuleItem): RuleFlag[] {
  const flags: RuleFlag[] = [];
  const rule = norm(item.rule);
  const payload = (item.rulePayload ?? '').trim();

  if (rule === 'match' || rule === 'final') {
    flags.push({
      id: 'match-fallback',
      label: '兜底规则',
      tone: 'warn',
      reason:
        '这些流量没有命中任何具体规则，最终落到了默认策略上。此处流量大通常意味着分流规则存在缺口，而不是预期行为。',
    });
  }

  if (rule === 'network' && payload === 'udp') {
    flags.push({
      id: 'broad-udp',
      label: '全量 UDP 代理',
      tone: 'warn',
      reason:
        'NETWORK,udp 会把所有 UDP 连接送入代理，包括时间同步、软件遥测、游戏与语音等通常并不需要走节点的背景流量。',
    });
  }

  if (rule === '' && payload === '') {
    flags.push({
      id: 'missing-rule',
      label: '缺少规则记录',
      tone: 'danger',
      reason: '连接已被观测，但内核没有回报命中了哪条规则，因此无法从该记录解释分流决策。',
    });
  }

  if (payload === '*' || payload === '') {
    if (rule !== '' && rule !== 'match' && rule !== 'final') {
      flags.push({
        id: 'broad-wildcard',
        label: '宽泛匹配',
        tone: 'info',
        reason: `规则 ${item.rule} 在不受限的匹配值上生效，实际覆盖范围可能远大于名称给人的印象。`,
      });
    }
  }

  return flags;
}

/** Highest-severity tone across a row's flags, used to pick the row marker colour. */
export function dominantTone(flags: RuleFlag[]): FlagTone | null {
  if (flags.some((f) => f.tone === 'danger')) return 'danger';
  if (flags.some((f) => f.tone === 'warn')) return 'warn';
  if (flags.some((f) => f.tone === 'info')) return 'info';
  return null;
}
