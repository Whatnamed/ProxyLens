import React, { useEffect, useRef, useState } from 'react';
import { useMeta } from '../../api/queries';
import { QueryApiClient } from '../../api/client';
import { deriveCollectorHealth, HealthLevel } from '../../lib/semantics';
import { formatLocalDateTimeCompact, formatRelative } from '../../utils/time';
import { formatCount } from '../../utils/format';
import { Badge } from '../ui/primitives';
import { IconPulse, IconClose } from '../ui/icons';
import { toProductError } from '../../lib/apiError';

/**
 * System Status is a secondary persistent surface, not a destination.
 *
 * Frozen rule: normal status stays quiet. `isFresh = false` is Accounting
 * lag, not a system failure, and is never rendered with failure styling.
 */

const LEVEL_TONE: Record<HealthLevel, 'ok' | 'warn' | 'danger' | 'neutral'> = {
  healthy: 'ok',
  stale: 'warn',
  offline: 'danger',
  unknown: 'neutral'
};

interface Props {
  client: QueryApiClient | null;
}

export const SystemStatus: React.FC<Props> = ({ client }) => {
  const [open, setOpen] = useState(false);
  const anchorRef = useRef<HTMLDivElement>(null);
  const metaQuery = useMeta(client);
  const meta = metaQuery.data;

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!anchorRef.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const health = deriveCollectorHealth(meta?.latestCollectorSession);
  const freshness = meta?.freshness;
  const schemaMismatch =
    meta !== undefined && meta.schemaVersion !== meta.maxBinarySchemaVersion;

  // Only materially impactful conditions escalate the rail indicator.
  const problem: 'none' | 'warn' | 'danger' =
    metaQuery.isError || meta?.dbState === 'INCOMPATIBLE' || meta?.dbState === 'UNAVAILABLE'
      ? 'danger'
      : health.level === 'offline'
        ? 'danger'
        : health.level === 'stale' || schemaMismatch
          ? 'warn'
          : 'none';

  const dotClass =
    problem === 'danger'
      ? 'status-dot status-dot--danger'
      : problem === 'warn'
        ? 'status-dot status-dot--warn'
        : meta
          ? 'status-dot status-dot--ok'
          : 'status-dot status-dot--unknown';

  const summaryText = () => {
    if (metaQuery.isError) return 'Status unavailable';
    if (!meta) return 'Connecting…';
    if (meta.dbState === 'INCOMPATIBLE') return 'Database incompatible';
    if (meta.dbState === 'UNAVAILABLE') return 'Database unavailable';
    if (health.level === 'offline') return `Collector ${health.label.toLowerCase()}`;
    if (health.level === 'stale') return 'Collector stale';
    if (freshness && !freshness.isFresh) return `Accounting lag ${formatCount(freshness.lagEvents)}`;
    return 'All systems nominal';
  };

  return (
    <div className="popover-anchor" ref={anchorRef}>
      <button
        className={`status-trigger ${problem !== 'none' ? 'has-problem' : ''}`}
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-haspopup="dialog"
        title="System status"
      >
        <span className={dotClass} />
        <span className="status-trigger-text">{summaryText()}</span>
        <IconPulse size={13} />
      </button>

      {open && (
        <div className="status-popover" role="dialog" aria-label="System status">
          <div className="status-popover-head">
            <span>System Status</span>
            <button className="btn btn--ghost btn--icon btn--sm" onClick={() => setOpen(false)} aria-label="Close">
              <IconClose size={12} />
            </button>
          </div>

          {metaQuery.isError ? (
            <div className="status-group">
              <div className="status-group-title">Status unavailable</div>
              <div style={{ fontSize: 'var(--fs-11)', color: 'var(--text-secondary)' }}>
                {toProductError(metaQuery.error).detail}
              </div>
            </div>
          ) : !meta ? (
            <div className="status-group">
              <div className="status-group-title">Connecting</div>
              <div style={{ fontSize: 'var(--fs-11)', color: 'var(--text-tertiary)' }}>
                Reading local service state…
              </div>
            </div>
          ) : (
            <>
              <div className="status-group">
                <div className="status-group-title">Collector</div>
                <dl className="kv">
                  <dt>State</dt>
                  <dd>
                    <Badge tone={LEVEL_TONE[health.level]}>{health.label}</Badge>
                  </dd>
                  <dt>Session</dt>
                  <dd className="mono truncate" title={meta.latestCollectorSession?.sessionId}>
                    {meta.latestCollectorSession?.sessionId ?? '—'}
                  </dd>
                  <dt>Started</dt>
                  <dd className="mono">
                    {meta.latestCollectorSession
                      ? formatLocalDateTimeCompact(meta.latestCollectorSession.startedAt)
                      : '—'}
                  </dd>
                  <dt>Heartbeat</dt>
                  <dd className="mono">
                    {meta.latestCollectorSession?.lastHeartbeatAt
                      ? formatRelative(meta.latestCollectorSession.lastHeartbeatAt)
                      : '—'}
                  </dd>
                </dl>
                <div style={{ fontSize: 'var(--fs-10)', color: 'var(--text-tertiary)', marginTop: 'var(--sp-3)' }}>
                  {health.detail}
                </div>
              </div>

              <div className="status-group">
                <div className="status-group-title">Accounting</div>
                <dl className="kv">
                  <dt>Run</dt>
                  <dd className="mono truncate" title={meta.latestAccountingRun?.runId}>
                    {meta.latestAccountingRun?.runId ?? '—'}
                  </dd>
                  <dt>Algorithm</dt>
                  <dd className="mono">{meta.latestAccountingRun?.algorithmVersion ?? '—'}</dd>
                  <dt>Status</dt>
                  <dd>
                    <Badge
                      tone={
                        meta.latestAccountingRun?.status === 'completed'
                          ? 'ok'
                          : meta.latestAccountingRun?.status === 'failed'
                            ? 'danger'
                            : 'warn'
                      }
                    >
                      {meta.latestAccountingRun?.status ?? 'none'}
                    </Badge>
                  </dd>
                  <dt>Freshness</dt>
                  <dd>
                    {freshness ? (
                      <Badge tone={freshness.isFresh ? 'ok' : 'neutral'}>
                        {freshness.isFresh ? 'Fresh' : 'Catching up'}
                      </Badge>
                    ) : (
                      '—'
                    )}
                  </dd>
                  <dt>Lag events</dt>
                  <dd className="mono">{formatCount(freshness?.lagEvents ?? 0)}</dd>
                </dl>
                {freshness && !freshness.isFresh && (
                  <div
                    style={{
                      fontSize: 'var(--fs-10)',
                      color: 'var(--text-tertiary)',
                      marginTop: 'var(--sp-3)'
                    }}
                  >
                    Figures are as of {formatLocalDateTimeCompact(freshness.completedAt)}. New
                    observations are still being accounted; this is not a failure.
                  </div>
                )}
              </div>

              <div className="status-group">
                <div className="status-group-title">Database & API</div>
                <dl className="kv">
                  <dt>DB state</dt>
                  <dd>
                    <Badge
                      tone={
                        meta.dbState === 'READY'
                          ? 'ok'
                          : meta.dbState === 'INCOMPATIBLE' || meta.dbState === 'UNAVAILABLE'
                            ? 'danger'
                            : 'warn'
                      }
                    >
                      {meta.dbState}
                    </Badge>
                  </dd>
                  <dt>Schema</dt>
                  <dd className="mono">
                    v{meta.schemaVersion}
                    <span className="dimmer"> / max v{meta.maxBinarySchemaVersion}</span>
                    {schemaMismatch && (
                      <span style={{ color: 'var(--warn)' }} title="Reads are refused on schema mismatch">
                        {' '}
                        mismatch
                      </span>
                    )}
                  </dd>
                  <dt>API</dt>
                  <dd className="mono">{meta.apiVersion}</dd>
                  <dt>App</dt>
                  <dd className="mono">{meta.appVersion}</dd>
                </dl>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
};
