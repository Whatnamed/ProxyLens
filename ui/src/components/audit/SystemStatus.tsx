import React from 'react';
import { MetaResponse } from '../../api/types';
import { StatusIndicator, StatusKind } from '../ui/primitives';
import { Locale, translate } from '../../i18n';
import { useLocale } from '../../state/AuditContext';

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

export function deriveSystemStatus(meta: MetaResponse | undefined, now = Date.now(), locale: Locale = 'en'): SystemStatusModel {
  const t = (key: string, vars?: Record<string, string | number>) => translate(locale, key, vars);
  if (!meta) {
    return {
      collector: { kind: 'offline', label: t('status.collectorUnknown'), title: t('status.metadataUnavailable') },
      accounting: { kind: 'offline', label: t('status.accountingUnknown'), title: t('status.metadataUnavailable') },
      db: { kind: 'offline', label: t('status.databaseUnknown'), title: t('status.metadataUnavailable') },
    };
  }

  let collector: SystemStatusModel['collector'];
  const session = meta.latestCollectorSession;
  if (!session) {
    collector = { kind: 'offline', label: t('status.collectorOffline'), title: t('status.noCollectorSession') };
  } else if (session.status === 'running') {
    const hb = session.lastHeartbeatAt ? new Date(session.lastHeartbeatAt).getTime() : NaN;
    if (isNaN(hb) || now - hb > heartbeatStaleThresholdMs(session.heartbeatIntervalMs)) {
      collector = {
        kind: 'stale',
        label: t('status.collectorStale'),
        title: t('status.collectorStaleTitle', { session: session.sessionId, heartbeat: session.lastHeartbeatAt ?? 'never' }),
      };
    } else {
      collector = { kind: 'fresh', label: t('status.collectorHealthy'), title: t('status.collectorHealthyTitle', { session: session.sessionId, heartbeat: session.lastHeartbeatAt ?? '—' }) };
    }
  } else {
    collector = {
      kind: 'offline',
      label: t('status.collectorOffline'),
      title: t('status.collectorEndedTitle', { status: session.status }),
    };
  }

  let accounting: SystemStatusModel['accounting'];
  if (!meta.latestAccountingRun) {
    accounting = { kind: 'neutral', label: t('status.noAccountingRun'), title: t('status.noAccountingRunTitle') };
  } else if (meta.freshness) {
    accounting = meta.freshness.isFresh
      ? { kind: 'fresh', label: t('overview.accountingFresh'), title: t('status.accountingFreshTitle', { run: meta.freshness.runId }) }
      : {
          kind: 'stale',
          label: t('status.accountingStale', { count: meta.freshness.lagEvents }),
          title: t('status.accountingStaleTitle', { run: meta.freshness.runId, count: meta.freshness.lagEvents }),
        };
  } else {
    accounting = {
      kind: meta.latestAccountingRun.status === 'completed' ? 'fresh' : 'stale',
      label: t('status.accountingStatus', { status: meta.latestAccountingRun.status }),
      title: t('status.accountingStatusTitle', { run: meta.latestAccountingRun.runId, status: meta.latestAccountingRun.status }),
    };
  }

  let db: SystemStatusModel['db'];
  switch (meta.dbState) {
    case 'READY':
      db = { kind: 'fresh', label: t('status.databaseReady'), title: t('status.schema', { version: meta.schemaVersion }) };
      break;
    case 'INCOMPATIBLE':
      db = {
        kind: 'offline',
        label: t('status.databaseIncompatible'),
        title: t('status.schemaTooNew', { version: meta.schemaVersion, max: meta.maxBinarySchemaVersion }),
      };
      break;
    default:
      db = { kind: 'offline', label: t('status.databaseState', { state: meta.dbState.toLowerCase() }), title: t('status.databaseStateTitle', { state: meta.dbState }) };
  }

  return { collector, accounting, db };
}

export const SystemStatusFooter: React.FC<{ meta: MetaResponse | undefined }> = ({ meta }) => {
  const { locale } = useLocale();
  const status = deriveSystemStatus(meta, Date.now(), locale);
  return (
    <div className="pl-sidebar__status">
      <StatusIndicator kind={status.collector.kind} label={status.collector.label} title={status.collector.title} />
      <StatusIndicator kind={status.accounting.kind} label={status.accounting.label} title={status.accounting.title} />
      <StatusIndicator kind={status.db.kind} label={status.db.label} title={status.db.title} />
    </div>
  );
};
