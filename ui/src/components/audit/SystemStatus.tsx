import React from 'react';
import { MetaResponse } from '../../api/types';
import { StatusIndicator, StatusKind } from '../ui/primitives';

export interface SystemStatusModel {
  collector: { kind: StatusKind; label: string; title: string };
  accounting: { kind: StatusKind; label: string; title: string };
  db: { kind: StatusKind; label: string; title: string };
}

const DEFAULT_HEARTBEAT_INTERVAL_MS = 5_000;
const MIN_HEARTBEAT_GRACE_MS = 15_000;

// Must match the backend Coverage grace rule: max(3 × heartbeatIntervalMs, 15000ms).
export function heartbeatStaleThresholdMs(heartbeatIntervalMs?: number): number {
  const interval = typeof heartbeatIntervalMs === 'number' && heartbeatIntervalMs > 0
    ? heartbeatIntervalMs
    : DEFAULT_HEARTBEAT_INTERVAL_MS;
  return Math.max(3 * interval, MIN_HEARTBEAT_GRACE_MS);
}

export function deriveSystemStatus(meta: MetaResponse | undefined, now = Date.now()): SystemStatusModel {
  if (!meta) {
    return {
      collector: { kind: 'offline', label: 'Collector unknown', title: 'Query API metadata unavailable' },
      accounting: { kind: 'offline', label: 'Accounting unknown', title: 'Query API metadata unavailable' },
      db: { kind: 'offline', label: 'Database unknown', title: 'Query API metadata unavailable' },
    };
  }

  let collector: SystemStatusModel['collector'];
  const session = meta.latestCollectorSession;
  if (!session) {
    collector = { kind: 'offline', label: 'Collector offline', title: 'No collector session recorded in this database' };
  } else if (session.status === 'running') {
    const hb = session.lastHeartbeatAt ? new Date(session.lastHeartbeatAt).getTime() : NaN;
    if (isNaN(hb) || now - hb > heartbeatStaleThresholdMs(session.heartbeatIntervalMs)) {
      collector = {
        kind: 'stale',
        label: 'Collector stale',
        title: `Session ${session.sessionId} is marked running but its last heartbeat is old (${session.lastHeartbeatAt ?? 'never'})`,
      };
    } else {
      collector = { kind: 'fresh', label: 'Collector healthy', title: `Session ${session.sessionId} running; heartbeat ${session.lastHeartbeatAt}` };
    }
  } else {
    collector = {
      kind: 'offline',
      label: 'Collector offline',
      title: `Latest session ended with status ${session.status}`,
    };
  }

  let accounting: SystemStatusModel['accounting'];
  if (!meta.latestAccountingRun) {
    accounting = { kind: 'neutral', label: 'No accounting run', title: 'No completed accounting run exists yet' };
  } else if (meta.freshness) {
    accounting = meta.freshness.isFresh
      ? { kind: 'fresh', label: 'Accounting fresh', title: `Run ${meta.freshness.runId} covers all journal events` }
      : {
          kind: 'stale',
          label: `Accounting stale (${meta.freshness.lagEvents} behind)`,
          title: `Run ${meta.freshness.runId} is ${meta.freshness.lagEvents} journal events behind; data shown is as-of the last completed run`,
        };
  } else {
    accounting = {
      kind: meta.latestAccountingRun.status === 'completed' ? 'fresh' : 'stale',
      label: `Accounting ${meta.latestAccountingRun.status}`,
      title: `Run ${meta.latestAccountingRun.runId} status: ${meta.latestAccountingRun.status}`,
    };
  }

  let db: SystemStatusModel['db'];
  switch (meta.dbState) {
    case 'READY':
      db = { kind: 'fresh', label: 'Database ready', title: `Schema v${meta.schemaVersion}` };
      break;
    case 'INCOMPATIBLE':
      db = {
        kind: 'offline',
        label: 'Database incompatible',
        title: `Schema v${meta.schemaVersion} exceeds supported v${meta.maxBinarySchemaVersion}`,
      };
      break;
    default:
      db = { kind: 'offline', label: `Database ${meta.dbState.toLowerCase()}`, title: `dbState=${meta.dbState}` };
  }

  return { collector, accounting, db };
}

export const SystemStatusFooter: React.FC<{ meta: MetaResponse | undefined }> = ({ meta }) => {
  const status = deriveSystemStatus(meta);
  return (
    <div className="pl-sidebar__status">
      <StatusIndicator kind={status.collector.kind} label={status.collector.label} title={status.collector.title} />
      <StatusIndicator kind={status.accounting.kind} label={status.accounting.label} title={status.accounting.title} />
      <StatusIndicator kind={status.db.kind} label={status.db.label} title={status.db.title} />
    </div>
  );
};
