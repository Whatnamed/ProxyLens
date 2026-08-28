/**
 * Human labels for the enumerated values the Go Query API returns.
 *
 * These are pure presentation names for server-side facts. They deliberately do not
 * reinterpret the values: an ambiguous relay stays "ambiguous", it is not rounded up to
 * "confirmed" or down to "ignored".
 */

/** Accounting classes (how the reconciler counted this connection's bytes). */
export const ACCOUNTING_CLASS_LABELS: Record<string, { label: string; detail: string }> = {
  unique: {
    label: '唯一计入',
    detail: '该连接的流量被完整计入，不存在中继重复。',
  },
  confirmed_relay_duplicate: {
    label: '已确认中继重复',
    detail:
      '已配对到对应的逻辑连接，确认为底层中继副本，其流量已从总量中扣除，避免重复计费。',
  },
  ambiguous_relay: {
    label: '歧义中继（保守保留）',
    detail:
      '疑似中继副本但配对证据不足。按保守对账策略不做扣减，流量仍然计入，以保证不低估真实消耗。',
  },
  missing_attribution: {
    label: '缺失归因（待核查）',
    detail: '连接被观测到，但缺少进程/域名等关键归因信息，流量保留在待核查口径中。',
  },
};

/** Attribution classes assigned by the collector at observation time. */
export const ATTRIBUTION_CLASS_LABELS: Record<string, { label: string; detail: string }> = {
  known_application: { label: '已知应用', detail: '可归因到具体进程的正常连接。' },
  unpaired_missing_attribution: {
    label: '未配对缺失归因',
    detail: '缺少进程或规则的底层连接，且未找到配对的逻辑连接，计入待核查流量。',
  },
  relay_candidate: { label: '中继候选', detail: '形态上疑似中继底层连接，等待对账判定。' },
  confirmed_relay_duplicate: {
    label: '已确认中继重复',
    detail: '已确认是中继副本，其流量已从总量中扣除。',
  },
};

/** Precision of an accounting figure. */
export const PRECISION_LABELS: Record<string, string> = {
  exact: '精确计数',
  interval_derived: '区间推算',
};

/** Why observation of a connection stopped. */
export const OBSERVATION_END_LABELS: Record<string, string> = {
  disappeared_from_snapshot: '从快照消失',
  epoch_boundary: 'Epoch 边界',
  collector_session_interrupted: '采集会话中断',
  clean_stop: '采集器正常停止',
  active: '仍在观测中',
};

/** Metadata completeness flags. Each is a distinct, explainable state (PRODUCT 3.4). */
export interface QualityFlagView {
  id: string;
  label: string;
  detail: string;
}

export function qualityFlagViews(conn: {
  metadata?: { process?: string; processPath?: string; host?: string; sniffHost?: string; destinationIP?: string };
  rule?: string;
  chains?: string[];
}): QualityFlagView[] {
  const meta = conn.metadata ?? {};
  const flags: QualityFlagView[] = [];

  if (!meta.process) {
    flags.push({
      id: 'missing_process',
      label: 'MissingProcess',
      detail: '内核未识别发起进程。TUN 模式下的底层转发流量常见此情况。',
    });
  }
  if (!meta.host && !meta.sniffHost) {
    flags.push({
      id: 'missing_host',
      label: 'MissingHost',
      detail: 'Host 与 SniffHost 均为空，缺少域名证据。',
    });
  }
  if (!meta.host && !meta.sniffHost && meta.destinationIP) {
    flags.push({
      id: 'ip_only',
      label: 'IPOnly',
      detail: '仅能依据目标 IP 识别，缺少域名信息，无法按域名优化规则。',
    });
  }
  if (!conn.rule) {
    flags.push({
      id: 'missing_rule',
      label: 'MissingRule',
      detail: '未记录命中规则，无法解释分流决策。',
    });
  }
  if (!conn.chains || conn.chains.length === 0) {
    flags.push({
      id: 'missing_chain',
      label: 'MissingChain',
      detail: '未记录代理链，无法还原策略组路径与出站节点。',
    });
  }

  return flags;
}

export function accountingClassView(cls: string | undefined) {
  return (
    ACCOUNTING_CLASS_LABELS[cls ?? ''] ?? {
      label: cls || '未分类',
      detail: '该核算类别在当前版本中尚无展示说明。',
    }
  );
}

export function attributionClassView(cls: string | undefined) {
  return (
    ATTRIBUTION_CLASS_LABELS[cls ?? ''] ?? {
      label: cls || '未分类',
      detail: '该归因类别在当前版本中尚无展示说明。',
    }
  );
}
