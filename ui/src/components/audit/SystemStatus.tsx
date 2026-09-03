import React, { useState, useEffect, useRef } from 'react';
import { MetaResponse } from '../../api/types';
import { KeyValue, StatusIndicator, StatusKind } from '../ui/primitives';
import { IconClose } from '../ui/icons';
import { Locale, translate } from '../../i18n';
import { useLocale } from '../../state/AuditContext';
import { formatLocalDateTime } from '../../utils/time';

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

export const SystemDetailDialog: React.FC<{
  meta: MetaResponse | undefined;
  onClose: () => void;
}> = ({ meta, onClose }) => {
  const { locale, t } = useLocale();
  const status = deriveSystemStatus(meta, Date.now(), locale);
  const dialogRef = useRef<HTMLDivElement | null>(null);
  const closeBtnRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    closeBtnRef.current?.focus();
  }, []);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
        return;
      }
      if (e.key === 'Tab' && dialogRef.current) {
        const focusable = dialogRef.current.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
        );
        if (focusable.length === 0) return;
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault();
            last.focus();
          }
        } else {
          if (document.activeElement === last) {
            e.preventDefault();
            first.focus();
          }
        }
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const collector = meta?.latestCollectorSession;
  const accounting = meta?.latestAccountingRun;

  return (
    <div
      className="pl-dialog-backdrop"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      role="presentation"
    >
      <div
        ref={dialogRef}
        className="pl-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="pl-dialog-system-title"
      >
        <div className="pl-dialog__header">
          <h2 id="pl-dialog-system-title" className="pl-dialog__title">
            {t('status.systemTitle')}
          </h2>
          <button
            ref={closeBtnRef}
            className="pl-icon-btn"
            onClick={onClose}
            aria-label={t('inspector.close')}
            title={t('inspector.closeTitle')}
          >
            <IconClose />
          </button>
        </div>

        <div className="pl-dialog__body">
          <div className="pl-dialog__section">
            <div className="pl-dialog__section-title">{t('status.collectorRuntime')}</div>
            <KeyValue
              items={[
                {
                  key: t('inspector.state'),
                  value: <StatusIndicator kind={status.collector.kind} label={status.collector.label} title={status.collector.title} />,
                },
                { key: t('status.appVersion'), value: meta?.appVersion ?? '—', mono: true },
                { key: t('status.sessionId'), value: collector?.sessionId ?? '—', mono: true },
                {
                  key: t('status.lastHeartbeat'),
                  value: collector?.lastHeartbeatAt ? formatLocalDateTime(collector.lastHeartbeatAt, locale) : '—',
                  mono: true,
                },
                {
                  key: t('status.heartbeatInterval'),
                  value: collector?.heartbeatIntervalMs ? `${collector.heartbeatIntervalMs} ms` : '—',
                  mono: true,
                },
              ]}
            />
          </div>

          <div className="pl-dialog__section">
            <div className="pl-dialog__section-title">{t('status.accountingEngine')}</div>
            <KeyValue
              items={[
                {
                  key: t('inspector.state'),
                  value: <StatusIndicator kind={status.accounting.kind} label={status.accounting.label} title={status.accounting.title} />,
                },
                { key: t('status.runId'), value: accounting?.runId ?? '—', mono: true },
                { key: t('status.algorithm'), value: accounting?.algorithmVersion ?? '—', mono: true },
                {
                  key: t('status.freshness'),
                  value: meta?.freshness
                    ? meta.freshness.isFresh
                      ? t('overview.accountingFresh')
                      : t('status.accountingStale', { count: meta.freshness.lagEvents })
                    : '—',
                },
                {
                  key: t('status.lagEvents'),
                  value: meta?.freshness ? String(meta.freshness.lagEvents) : '—',
                  mono: true,
                },
                {
                  key: t('status.journalEvents'),
                  value: accounting ? String(accounting.sourceJournalEventCount) : '—',
                  mono: true,
                },
              ]}
            />
          </div>

          <div className="pl-dialog__section">
            <div className="pl-dialog__section-title">{t('status.storageSchema')}</div>
            <KeyValue
              items={[
                {
                  key: t('status.dbState'),
                  value: <StatusIndicator kind={status.db.kind} label={status.db.label} title={status.db.title} />,
                },
                { key: t('status.schemaVersion'), value: meta ? `v${meta.schemaVersion}` : '—', mono: true },
                { key: t('status.maxBinarySchema'), value: meta ? `v${meta.maxBinarySchemaVersion}` : '—', mono: true },
                { key: t('status.apiVersion'), value: meta?.apiVersion ?? '—', mono: true },
              ]}
            />
          </div>
        </div>
      </div>
    </div>
  );
};

export const SystemStatusFooter: React.FC<{ meta: MetaResponse | undefined }> = ({ meta }) => {
  const { locale, t } = useLocale();
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const status = deriveSystemStatus(meta, Date.now(), locale);

  const handleClose = () => {
    setOpen(false);
    requestAnimationFrame(() => {
      triggerRef.current?.focus();
    });
  };

  return (
    <>
      <div className="pl-sidebar__status">
        <button
          ref={triggerRef}
          type="button"
          className="pl-sidebar__status-btn"
          onClick={() => setOpen(true)}
          aria-haspopup="dialog"
          aria-expanded={open}
          title={t('status.clickForDetails')}
        >
          <StatusIndicator kind={status.collector.kind} label={status.collector.label} title={status.collector.title} />
          <StatusIndicator kind={status.accounting.kind} label={status.accounting.label} title={status.accounting.title} />
          <StatusIndicator kind={status.db.kind} label={status.db.label} title={status.db.title} />
        </button>
      </div>
      {open && <SystemDetailDialog meta={meta} onClose={handleClose} />}
    </>
  );
};
