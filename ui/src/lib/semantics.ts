/**
 * ProxyLens semantic labels.
 *
 * Every value the backend can emit is mapped to a specific, explainable
 * human label here. The frozen rule "Unknown must be explainable" is enforced
 * at this layer: nothing falls through to a generic "Unknown" bucket, and a
 * value we do not recognise is surfaced as `Unrecognised value` together with
 * the raw string so it can be investigated rather than silently hidden.
 */

import { ConnectionRecord, QualityFlags } from '../api/types';

/* ==========================================================================
 * Route classification — four semantically independent classes
 * ========================================================================== */

export type RouteKey = 'PROXY' | 'DIRECT' | 'REJECT' | 'ALL';

export const ROUTES: RouteKey[] = ['PROXY', 'DIRECT', 'REJECT', 'ALL'];

export interface RouteMeta {
  key: RouteKey;
  label: string;
  short: string;
  /** What this class actually means for an auditor. */
  meaning: string;
}

export const ROUTE_META: Record<RouteKey, RouteMeta> = {
  PROXY: {
    key: 'PROXY',
    label: 'Proxy',
    short: 'PROXY',
    meaning: 'Forwarded through a proxy chain and an outbound node.'
  },
  DIRECT: {
    key: 'DIRECT',
    label: 'Direct',
    short: 'DIRECT',
    meaning: 'Sent without a proxy hop; still observed and accounted.'
  },
  REJECT: {
    key: 'REJECT',
    label: 'Reject',
    short: 'REJECT',
    meaning: 'Blocked by a rule before any egress traffic occurred.'
  },
  ALL: {
    key: 'ALL',
    label: 'All routes',
    short: 'ALL',
    meaning: 'Every routing class combined, counted independently.'
  }
};

export function routeKey(value: string | undefined | null): RouteKey {
  const v = (value || '').toUpperCase();
  if (v === 'PROXY' || v === 'DIRECT' || v === 'REJECT' || v === 'ALL') return v;
  return 'ALL';
}

/** CSS class suffix used by the badge component. */
export function routeTone(value: string | undefined | null): string {
  return routeKey(value).toLowerCase();
}

/* ==========================================================================
 * Accounting class — how a connection's bytes were attributed
 * (vocabulary of `accountingClass` on accounted traffic / summary)
 * ========================================================================== */

export type Tone = 'ok' | 'warn' | 'danger' | 'neutral' | 'info';

export interface SemanticLabel {
  label: string;
  tone: Tone;
  /** Why this class exists — shown on hover / in the inspector. */
  explanation: string;
}

const ACCOUNTING_CLASS: Record<string, SemanticLabel> = {
  unique: {
    label: 'Unique application traffic',
    tone: 'ok',
    explanation:
      'Bytes accounted directly to this connection with no relay duplicate offset applied.'
  },
  confirmed_relay_duplicate: {
    label: 'Confirmed relay duplicate',
    tone: 'warn',
    explanation:
      'This connection is the relay side of a confirmed pair. Its bytes are already counted at the application side, so they are excluded from totals to prevent double counting.'
  },
  ambiguous_relay: {
    label: 'Ambiguous relay',
    tone: 'warn',
    explanation:
      'Relay characteristics were detected but the pair could not be confirmed, either because candidates were insufficient or because traffic features conflicted. Bytes are retained, not deducted.'
  },
  missing_attribution: {
    label: 'Missing attribution',
    tone: 'danger',
    explanation:
      'Traffic was observed but could not be attributed to a specific process. The destination and rule may still be known.'
  },
  // Aliases seen in earlier contract drafts — kept so a legacy value is still
  // explained rather than rendered raw.
  known_application: {
    label: 'Known application',
    tone: 'ok',
    explanation: 'Attributed to an identified local process.'
  },
  direct_application: {
    label: 'Direct application',
    tone: 'ok',
    explanation: 'Attributed to an identified process egressing directly.'
  },
  system_proxy_inbound: {
    label: 'System proxy inbound',
    tone: 'info',
    explanation: 'Accepted by the local inbound listener rather than TUN.'
  },
  ambiguous_relay_candidate: {
    label: 'Ambiguous relay candidate',
    tone: 'warn',
    explanation: 'A possible relay pair that could not be confirmed.'
  }
};

export function describeAccountingClass(value: string | undefined | null): SemanticLabel {
  if (!value) {
    return {
      label: 'Not accounted',
      tone: 'neutral',
      explanation: 'No accounting record exists for this connection in the latest completed run.'
    };
  }
  return (
    ACCOUNTING_CLASS[value] ?? {
      label: 'Unrecognised accounting class',
      tone: 'neutral',
      explanation: `The backend returned an accounting class ProxyLens does not model: "${value}". Treat its totals with caution and check the accounting version.`
    }
  );
}

/* ==========================================================================
 * Attribution class — process attribution state on the connection record
 * (vocabulary of `latestAttributionClass` / `Types.AttributionClass`)
 * ========================================================================== */

const ATTRIBUTION_CLASS: Record<string, SemanticLabel> = {
  known_application: {
    label: 'Known application',
    tone: 'ok',
    explanation: 'The connection is attributed to an identified local process.'
  },
  unpaired_missing_attribution: {
    label: 'Missing process attribution',
    tone: 'danger',
    explanation:
      'Destination and routing are known, but no local process could be attributed to this connection.'
  },
  relay_candidate: {
    label: 'Relay candidate',
    tone: 'warn',
    explanation:
      'This connection may be the relay side of a pair; the pairing has not been confirmed.'
  },
  confirmed_relay_duplicate: {
    label: 'Confirmed relay duplicate',
    tone: 'warn',
    explanation:
      'Confirmed as the relay side of a paired connection. Excluded from application-side totals to avoid double counting.'
  }
};

export function describeAttributionClass(value: string | undefined | null): SemanticLabel {
  if (!value) {
    return {
      label: 'Attribution pending',
      tone: 'neutral',
      explanation: 'No attribution class has been recorded for this connection yet.'
    };
  }
  return (
    ATTRIBUTION_CLASS[value] ?? {
      label: 'Unrecognised attribution class',
      tone: 'neutral',
      explanation: `The backend returned an attribution class ProxyLens does not model: "${value}".`
    }
  );
}

/* ==========================================================================
 * Quality flags — metadata completeness
 * ========================================================================== */

export interface QualityIssue {
  key: string;
  label: string;
  tone: Tone;
  explanation: string;
}

/**
 * Each flag becomes its own named evidence condition. They are never merged
 * into one "Unknown" bucket.
 */
export function qualityIssues(flags: QualityFlags | undefined): QualityIssue[] {
  if (!flags) return [];
  const issues: QualityIssue[] = [];

  if (flags.missingProcess) {
    issues.push({
      key: 'missingProcess',
      label: 'Missing process',
      tone: 'danger',
      explanation:
        'No local process could be attributed. The destination and routing decision may still be known.'
    });
  }
  if (flags.missingProcessPath && !flags.missingProcess) {
    issues.push({
      key: 'missingProcessPath',
      label: 'Missing process path',
      tone: 'warn',
      explanation:
        'The process name is known but the executable path is not, so two different binaries with the same name cannot be distinguished.'
    });
  }
  if (flags.missingHost) {
    issues.push({
      key: 'missingHost',
      label: 'Missing host',
      tone: 'warn',
      explanation: 'No host or sniffed hostname is available for this connection.'
    });
  }
  if (flags.ipOnly) {
    issues.push({
      key: 'ipOnly',
      label: 'IP-only destination',
      tone: 'warn',
      explanation:
        'Neither host nor sniffed host could be resolved, so the destination is identifiable only by IP address.'
    });
  }
  if (flags.missingRule) {
    issues.push({
      key: 'missingRule',
      label: 'No rule match',
      tone: 'warn',
      explanation:
        'The connection did not match an explicit rule and fell through to the final fallback matcher.'
    });
  }
  if (flags.missingChain) {
    issues.push({
      key: 'missingChain',
      label: 'Missing proxy chain',
      tone: 'warn',
      explanation: 'No proxy chain was reported, so the intermediate hops are unknown.'
    });
  }
  return issues;
}

/* ==========================================================================
 * Observation lifecycle
 * ========================================================================== */

const END_REASONS: Record<string, string> = {
  disappeared_from_snapshot:
    'The connection stopped appearing in Mihomo snapshots and observation was closed.',
  epoch_boundary:
    'An epoch break occurred, so the previous observation interval was closed and a new epoch was started.',
  collector_session_closed:
    'The Collector session ended cleanly while the connection was still open.',
  collector_session_interrupted:
    'The Collector session was interrupted (crash or forced stop) while the connection was still open.'
};

export function describeObservationEndReason(reason: string | undefined | null): string | null {
  if (!reason) return null;
  return END_REASONS[reason] ?? `Observation ended: ${reason}`;
}

const CONNECTION_STATES: Record<string, SemanticLabel> = {
  active: { label: 'Active', tone: 'ok', explanation: 'Still present in the latest observation.' },
  disappeared_from_snapshot: {
    label: 'Ended',
    tone: 'neutral',
    explanation: 'No longer present in Mihomo snapshots; observation has been closed.'
  }
};

export function describeConnectionState(state: string | undefined | null): SemanticLabel {
  if (!state) return { label: 'Unknown state', tone: 'neutral', explanation: 'No state recorded.' };
  return (
    CONNECTION_STATES[state] ?? {
      label: 'Unrecognised state',
      tone: 'neutral',
      explanation: `Connection state reported by the backend: "${state}".`
    }
  );
}

/* ==========================================================================
 * Precision — exact vs interval-derived
 * ========================================================================== */

export function isEstimatedPrecision(precision: string | undefined | null): boolean {
  if (!precision) return false;
  return precision === 'interval_derived' || precision === 'estimated';
}

export function describePrecision(precision: string | undefined | null): SemanticLabel {
  if (!precision) {
    return {
      label: 'Unspecified precision',
      tone: 'neutral',
      explanation: 'The backend did not report how this value was derived.'
    };
  }
  if (precision === 'exact' || precision === 'exact_snapshot') {
    return {
      label: 'Exact',
      tone: 'ok',
      explanation: 'Measured directly from a discrete traffic event for this connection.'
    };
  }
  if (precision === 'interval_derived') {
    return {
      label: 'Interval-derived',
      tone: 'warn',
      explanation:
        'Allocated proportionally across the sampling interval rather than measured at a single point. Reliable in aggregate, not exact per connection.'
    };
  }
  if (precision === 'estimated') {
    return {
      label: 'Estimated',
      tone: 'warn',
      explanation:
        'Estimated from global counter deltas rather than measured per connection.'
    };
  }
  return {
    label: 'Unrecognised precision',
    tone: 'neutral',
    explanation: `Precision value reported by the backend: "${precision}".`
  };
}

/* ==========================================================================
 * Monitoring gaps
 * ========================================================================== */

export type GapSource = 'controller_stream' | 'collector_session_boundary' | 'unknown';

export interface GapSourceMeta {
  label: string;
  tone: Tone;
  explanation: string;
}

const GAP_SOURCES: Record<string, GapSourceMeta> = {
  controller_stream: {
    label: 'Controller stream',
    tone: 'warn',
    explanation:
      'The Mihomo External Controller stream was interrupted, reconnected, or the system was suspended. Connections during this interval were not observed.'
  },
  collector_session_boundary: {
    label: 'Collector offline',
    tone: 'danger',
    explanation:
      'The ProxyLens Collector was not running. No collection occurred during this interval at all.'
  }
};

export function describeGapSource(source: string | undefined | null): GapSourceMeta {
  if (!source) {
    return {
      label: 'Unspecified source',
      tone: 'neutral',
      explanation: 'The gap record does not state what caused the interruption.'
    };
  }
  return (
    GAP_SOURCES[source] ?? {
      label: 'Unrecognised source',
      tone: 'neutral',
      explanation: `Gap source reported by the backend: "${source}".`
    }
  );
}

/* ==========================================================================
 * Collector health
 * ========================================================================== */

export type HealthLevel = 'healthy' | 'stale' | 'offline' | 'unknown';

export interface CollectorHealth {
  level: HealthLevel;
  label: string;
  detail: string;
}

/**
 * Heartbeat age is judged against the session's own advertised interval with a
 * grace multiplier, so a slow heartbeat cadence is not mistaken for failure.
 */
export function deriveCollectorHealth(
  session:
    | {
        status?: string;
        lastHeartbeatAt?: string;
        heartbeatIntervalMs?: number;
        startedAt?: string;
        endedAt?: string;
      }
    | undefined
    | null,
  now = Date.now()
): CollectorHealth {
  if (!session) {
    return {
      level: 'unknown',
      label: 'No collector session',
      detail: 'ProxyLens has never recorded a Collector session in this database.'
    };
  }

  if (session.status === 'interrupted') {
    return {
      level: 'offline',
      label: 'Interrupted',
      detail: 'The last Collector session ended unexpectedly and was not closed cleanly.'
    };
  }
  if (session.status === 'closed_clean') {
    return {
      level: 'offline',
      label: 'Stopped',
      detail: 'The Collector session closed cleanly and is not currently running.'
    };
  }

  const hb = session.lastHeartbeatAt ? new Date(session.lastHeartbeatAt).getTime() : null;
  if (hb === null) {
    return {
      level: 'unknown',
      label: 'No heartbeat',
      detail: 'A Collector session exists but has not reported a heartbeat yet.'
    };
  }

  const interval = session.heartbeatIntervalMs && session.heartbeatIntervalMs > 0
    ? session.heartbeatIntervalMs
    : 30_000;
  const age = now - hb;
  // 3x the advertised interval is the point at which we stop believing the
  // session is live.
  const threshold = Math.max(interval * 3, 60_000);

  if (age <= threshold) {
    return {
      level: 'healthy',
      label: 'Running',
      detail: `Heartbeat received ${Math.round(age / 1000)}s ago (interval ${Math.round(interval / 1000)}s).`
    };
  }

  const staleWindow = Math.max(interval * 10, 300_000);
  if (age <= staleWindow) {
    return {
      level: 'stale',
      label: 'Stale',
      detail: `No heartbeat for ${Math.round(age / 60000)} min. The Collector may be suspended or unresponsive.`
    };
  }

  return {
    level: 'offline',
    label: 'Offline',
    detail: `No heartbeat for ${Math.round(age / 60000)} min. The Collector is treated as not running.`
  };
}

/* ==========================================================================
 * Derived display helpers
 * ========================================================================== */

/**
 * The physical outbound node.
 * Frozen hop-order rule: `chains[0]` is the final physical egress hop, and
 * `chains[last]` is the top-level policy group.
 */
export function finalEgress(conn: Pick<ConnectionRecord, 'chains' | 'route'>): string | null {
  const chains = conn.chains?.filter(Boolean) ?? [];
  if (chains.length === 0) return null;
  return chains[0];
}

export function topPolicyGroup(conn: Pick<ConnectionRecord, 'chains'>): string | null {
  const chains = conn.chains?.filter(Boolean) ?? [];
  if (chains.length < 2) return null;
  return chains[chains.length - 1];
}

/** Hops strictly between the policy group and the physical egress. */
export function intermediateHops(conn: Pick<ConnectionRecord, 'chains'>): string[] {
  const chains = conn.chains?.filter(Boolean) ?? [];
  if (chains.length <= 2) return [];
  return chains.slice(1, chains.length - 1);
}

export function displayHost(conn: Pick<ConnectionRecord, 'metadata'>): {
  value: string;
  isIpOnly: boolean;
  kind: 'host' | 'sniff' | 'ip';
} {
  const m = conn.metadata ?? {};
  if (m.host) return { value: m.host, isIpOnly: false, kind: 'host' };
  if (m.sniffHost) return { value: m.sniffHost, isIpOnly: false, kind: 'sniff' };
  if (m.remoteDestination) return { value: m.remoteDestination, isIpOnly: true, kind: 'ip' };
  if (m.destinationIP) return { value: m.destinationIP, isIpOnly: true, kind: 'ip' };
  return { value: 'No destination recorded', isIpOnly: true, kind: 'ip' };
}

export function displayProcess(conn: Pick<ConnectionRecord, 'metadata'>): {
  name: string;
  missing: boolean;
} {
  const p = conn.metadata?.process;
  if (!p) return { name: 'No process attribution', missing: true };
  return { name: p, missing: false };
}

/** Basename of a long executable path, for dense table display. */
export function basenameOf(path: string | undefined | null): string {
  if (!path) return '';
  const normalized = path.replace(/\\/g, '/');
  const idx = normalized.lastIndexOf('/');
  return idx >= 0 ? normalized.slice(idx + 1) : normalized;
}

/* ==========================================================================
 * Evidence flags — the compact per-row evidence-quality signal
 * ========================================================================== */

export interface EvidenceFlag {
  key: string;
  text: string;
  tone: Tone;
  title: string;
}

/**
 * Only meaningful quality conditions are surfaced, and each one keeps its own
 * identity. The UI never collapses them into a single "unknown" marker.
 */
export function evidenceFlags(conn: ConnectionRecord): EvidenceFlag[] {
  const flags: EvidenceFlag[] = [];
  const q = conn.qualityFlags;

  if (q?.missingProcess) {
    flags.push({
      key: 'missingProcess',
      text: 'no proc',
      tone: 'danger',
      title: 'Missing process attribution: destination and routing are known, but no local process could be attributed.'
    });
  }
  if (q?.ipOnly) {
    flags.push({
      key: 'ipOnly',
      text: 'ip only',
      tone: 'warn',
      title: 'IP-only destination: neither host nor sniffed host could be resolved.'
    });
  } else if (q?.missingHost) {
    flags.push({
      key: 'missingHost',
      text: 'no host',
      tone: 'warn',
      title: 'Missing host: no hostname is recorded for this connection.'
    });
  }

  const cls = conn.latestAttributionClass;
  if (cls === 'confirmed_relay_duplicate') {
    flags.push({
      key: 'relayDup',
      text: 'relay',
      tone: 'warn',
      title: 'Confirmed relay duplicate: already counted at the application side, so it is excluded from application totals.'
    });
  } else if (cls === 'relay_candidate') {
    flags.push({
      key: 'relayCandidate',
      text: 'relay?',
      tone: 'warn',
      title: 'Relay candidate: may be the relay side of a pair, but the pairing is unconfirmed.'
    });
  }

  if (q?.missingRule) {
    flags.push({
      key: 'missingRule',
      text: 'no rule',
      tone: 'warn',
      title: 'No rule match: fell through to the final fallback matcher.'
    });
  }
  if (q?.missingChain) {
    flags.push({
      key: 'missingChain',
      text: 'no chain',
      tone: 'warn',
      title: 'Missing proxy chain: intermediate hops are unknown.'
    });
  }
  if (conn.possibleUnobservedTail) {
    flags.push({
      key: 'tail',
      text: 'tail',
      tone: 'neutral',
      title:
        'Possible unobserved tail: the connection may have transferred more after the last observation.'
    });
  }
  if (conn.preexistingAtStart) {
    flags.push({
      key: 'preexisting',
      text: 'pre-existing',
      tone: 'neutral',
      title:
        'Pre-existing at session start: this connection was already open when the Collector began observing, so earlier traffic was never seen.'
    });
  }

  return flags;
}
