import React, { useEffect, useState } from 'react';
import {
  useConnectionDetail,
  useConnectionTraffic
} from '../../api/queries';
import { QueryApiClient } from '../../api/client';
import { ConnectionRef } from '../../state/AuditContext';
import { ConnectionRecord } from '../../api/types';
import {
  describeAccountingClass,
  describeAttributionClass,
  describeObservationEndReason,
  describeConnectionState,
  describePrecision,
  finalEgress,
  intermediateHops,
  qualityIssues,
  topPolicyGroup,
  basenameOf
} from '../../lib/semantics';
import { Badge, Tip } from '../../components/ui/primitives';
import { ErrorState, SkeletonLine } from '../../components/ui/states';
import { toProductError } from '../../lib/apiError';
import { formatBytes, formatBytesExact, formatCount, formatDuration } from '../../utils/format';
import { formatLocalTime, formatLocalDateTimeCompact } from '../../utils/time';
import { IconClose, IconChevronLeft, IconChevronRight, IconPulse } from '../../components/ui/icons';

/**
 * The Inspector explains one connection's complete cause-and-effect chain.
 *
 * It is a contextual surface inside History, not a destination: closing it
 * must return the investigator to the exact filter / page / window state.
 */

interface Props {
  client: QueryApiClient | null;
  ref: ConnectionRef;
  onClose: () => void;
  onStep: (delta: number) => void;
  hasPrev: boolean;
  hasNext: boolean;
  position: string;
}

/* ---------- Collapsible section ---------- */
const ISection: React.FC<{
  title: string;
  badge?: React.ReactNode;
  children: React.ReactNode;
  defaultOpen?: boolean;
}> = ({ title, badge, children, defaultOpen = true }) => {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="isec">
      <button className="isec-head" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        <span className={`isec-caret ${open ? 'is-open' : ''}`}>
          <IconChevronRight size={11} />
        </span>
        <span className="isec-title">{title}</span>
        {badge}
      </button>
      {open && <div className="isec-body">{children}</div>}
    </div>
  );
};

/* ---------- Causal path ---------- */
const CausalStep: React.FC<{
  label: string;
  value: React.ReactNode;
  sub?: React.ReactNode;
  muted?: boolean;
  filled?: boolean;
  last?: boolean;
}> = ({ label, value, sub, muted, filled }) => (
  <div className="causal-step">
    <div className="causal-rail">
      <span className={`causal-node ${filled ? 'causal-node--filled' : ''}`} />
      <span className="causal-line" />
    </div>
    <div className="causal-content">
      <div className="causal-label">{label}</div>
      <div className={`causal-value ${muted ? 'causal-value--muted' : ''}`}>{value}</div>
      {sub && <div className="causal-sub">{sub}</div>}
    </div>
  </div>
);

const CausalPath: React.FC<{ conn: ConnectionRecord }> = ({ conn }) => {
  const m = conn.metadata;
  const egress = finalEgress(conn);
  const group = topPolicyGroup(conn);
  const hops = intermediateHops(conn);

  const dest = m.host ?? m.sniffHost ?? m.destinationIP ?? m.remoteDestination ?? null;
  const destSub = [
    m.destinationIP && m.host ? m.destinationIP : null,
    m.destinationPort ? `port ${m.destinationPort}` : null,
    m.network ? m.network.toUpperCase() : null,
    m.dnsMode ? `dns: ${m.dnsMode}` : null
  ]
    .filter(Boolean)
    .join(' · ');

  return (
    <div className="causal">
      <CausalStep
        label="Process"
        filled={!!m.process}
        muted={!m.process}
        value={m.process ?? 'No process attribution'}
        sub={m.processPath ?? undefined}
      />
      <CausalStep
        label="Destination"
        filled={!!dest}
        muted={!dest}
        value={dest ?? 'No destination recorded'}
        sub={destSub || undefined}
      />
      <CausalStep
        label="Rule"
        filled={!!conn.rule}
        muted={!conn.rule}
        value={conn.rule || 'Match (final fallback)'}
        sub={conn.rulePayload || undefined}
      />
      <CausalStep
        label="Top policy group"
        filled={!!group}
        muted={!group}
        value={group ?? 'Not reported'}
      />
      <CausalStep
        label="Intermediate chain"
        filled={hops.length > 0}
        muted={hops.length === 0}
        value={hops.length > 0 ? hops.join(' → ') : 'Direct — no intermediate hop'}
        sub={conn.providerChains?.length ? `provider: ${conn.providerChains.join(' → ')}` : undefined}
      />
      <CausalStep
        label="Final physical egress"
        filled={!!egress}
        muted={!egress}
        value={egress ?? (conn.route === 'DIRECT' ? 'Direct (no proxy egress)' : 'Not reported')}
        sub={
          conn.chains && conn.chains.length > 0
            ? `chains: ${conn.chains.join(' → ')}`
            : undefined
        }
      />
    </div>
  );
};

/* ---------- Main ---------- */
export const ConnectionInspector: React.FC<Props> = ({
  client,
  ref,
  onClose,
  onStep,
  hasPrev,
  hasNext,
  position
}) => {
  const detail = useConnectionDetail(client, ref);
  const [framesOpen, setFramesOpen] = useState(false);
  const frames = useConnectionTraffic(client, ref, framesOpen);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Only when focus is not inside a text input.
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'SELECT' || t.tagName === 'TEXTAREA')) return;
      if (e.key === 'Escape') onClose();
      if (e.key === 'ArrowDown') onStep(1);
      if (e.key === 'ArrowUp') onStep(-1);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose, onStep]);

  const conn = detail.data?.connection;
  const events = detail.data?.accountingEvents ?? [];
  const summary = detail.data?.accountingSummary;

  // Exact vs interval-derived, derived from the event stream itself.
  const precisionSplit = events.reduce<{ exact: number; derived: number }>(
    (acc, e) => {
      const bytes = e.accountedUpload + e.accountedDownload;
      if (e.precision === 'exact' || e.precision === 'exact_snapshot') acc.exact += bytes;
      else acc.derived += bytes;
      return acc;
    },
    { exact: 0, derived: 0 }
  );

  return (
    <aside className="inspector" aria-label="Connection Inspector">
      <div className="inspector-head">
        <div className="inspector-title">
          <div className="inspector-eyebrow">Connection Inspector · {position}</div>
          <div className="inspector-id" title={`${ref.sessionId} / epoch ${ref.epochId} / ${ref.connectionId}`}>
            {ref.connectionId}
          </div>
        </div>
        <div className="inspector-actions">
          <button
            className="btn btn--icon btn--sm"
            onClick={() => onStep(-1)}
            disabled={!hasPrev}
            title="Previous connection (Up)"
            aria-label="Previous connection"
          >
            <IconChevronLeft size={12} />
          </button>
          <button
            className="btn btn--icon btn--sm"
            onClick={() => onStep(1)}
            disabled={!hasNext}
            title="Next connection (Down)"
            aria-label="Next connection"
          >
            <IconChevronRight size={12} />
          </button>
          <button
            className="btn btn--icon btn--sm"
            onClick={onClose}
            title="Close inspector (Esc)"
            aria-label="Close inspector"
          >
            <IconClose size={12} />
          </button>
        </div>
      </div>

      <div className="inspector-body">
        {detail.isLoading && !detail.data ? (
          <>
            <SkeletonLine height={12} width="40%" />
            <SkeletonLine height={120} />
            <SkeletonLine height={80} />
            <SkeletonLine height={100} />
          </>
        ) : detail.isError ? (
          <ErrorState error={toProductError(detail.error)} onRetry={() => detail.refetch()} compact />
        ) : !conn ? null : (
          <>
            {/* ---- A. Causal path ---- */}
            <ISection title="Causal path" badge={<Badge tone={conn.route === 'PROXY' ? 'proxy' : conn.route === 'DIRECT' ? 'direct' : 'reject'}>{conn.route}</Badge>}>
              <CausalPath conn={conn} />
            </ISection>

            {/* ---- B. Traffic accounting ---- */}
            <ISection title="Traffic accounting">
              <div className="figures">
                <div className="figure">
                  <div className="figure-label">Upload</div>
                  <div className="figure-value">{formatBytes(conn.monitoredUploadTotal)}</div>
                </div>
                <div className="figure">
                  <div className="figure-label">Download</div>
                  <div className="figure-value">{formatBytes(conn.monitoredDownloadTotal)}</div>
                </div>
                <div className="figure">
                  <div className="figure-label">Total</div>
                  <div className="figure-value">
                    {formatBytes(conn.monitoredUploadTotal + conn.monitoredDownloadTotal)}
                  </div>
                </div>
              </div>

              <dl className="kv" style={{ marginTop: 'var(--sp-4)' }}>
                <dt>Raw observed</dt>
                <dd className="mono">
                  ↑ {formatBytes(summary?.rawUploadTotal ?? 0)} ↓ {formatBytes(summary?.rawDownloadTotal ?? 0)}
                  <Tip
                    content="Values as observed on this connection before relay reconciliation removed duplicate relay bytes."
                  >
                    <span style={{ marginLeft: 6, color: 'var(--text-tertiary)', cursor: 'help' }}>?</span>
                  </Tip>
                </dd>
                <dt>Accounted</dt>
                <dd className="mono">
                  ↑ {formatBytes(summary?.accountedUploadTotal ?? 0)} ↓{' '}
                  {formatBytes(summary?.accountedDownloadTotal ?? 0)}
                  <Tip content="Values after relay reconciliation; these are the bytes that count toward totals.">
                    <span style={{ marginLeft: 6, color: 'var(--text-tertiary)', cursor: 'help' }}>?</span>
                  </Tip>
                </dd>
                <dt>Precision</dt>
                <dd className="mono">
                  <span>{describePrecision('exact').label} </span>
                  <span>{formatBytes(precisionSplit.exact)}</span>
                  {precisionSplit.derived > 0 && (
                    <>
                      <span className="dimmer"> · </span>
                      <span className="is-estimated" title="Interval-derived, not a point measurement">
                        {formatBytes(precisionSplit.derived)} derived
                      </span>
                    </>
                  )}
                </dd>
                {summary?.runId && (
                  <>
                    <dt>Run</dt>
                    <dd className="mono truncate" title={summary.runId}>
                      {summary.runId}
                    </dd>
                  </>
                )}
              </dl>
            </ISection>

            {/* ---- C. Evidence quality ---- */}
            <ISection title="Evidence quality">
              <dl className="kv">
                <dt>Attribution</dt>
                <dd>
                  {(() => {
                    const a = describeAttributionClass(conn.latestAttributionClass ?? summary?.accountingClass);
                    return (
                      <Tip content={a.explanation}>
                        <Badge tone={a.tone} mono>
                          {a.label}
                        </Badge>
                      </Tip>
                    );
                  })()}
                </dd>
                <dt>Accounting class</dt>
                <dd>
                  {(() => {
                    const a = describeAccountingClass(summary?.accountingClass ?? conn.latestAttributionClass);
                    return (
                      <Tip content={a.explanation}>
                        <Badge tone={a.tone} mono>
                          {a.label}
                        </Badge>
                      </Tip>
                    );
                  })()}
                </dd>
              </dl>

              {(() => {
                const issues = qualityIssues(conn.qualityFlags);
                if (issues.length === 0) {
                  return (
                    <div style={{ marginTop: 'var(--sp-4)', fontSize: 'var(--fs-11)', color: 'var(--text-tertiary)' }}>
                      All metadata fields are present for this connection.
                    </div>
                  );
                }
                return (
                  <div style={{ marginTop: 'var(--sp-4)', display: 'flex', flexDirection: 'column', gap: 'var(--sp-2)' }}>
                    {issues.map((i) => (
                      <Tip key={i.key} content={i.explanation}>
                        <span style={{ display: 'block' }}>
                          <Badge tone={i.tone}>{i.label}</Badge>
                        </span>
                      </Tip>
                    ))}
                  </div>
                );
              })()}

              {(conn.possibleUnobservedTail || conn.preexistingAtStart) && (
                <div style={{ marginTop: 'var(--sp-4)', display: 'flex', flexDirection: 'column', gap: 'var(--sp-2)' }}>
                  {conn.possibleUnobservedTail && (
                    <Tip content="The connection may have transferred more after the last observation. Its totals are a lower bound.">
                      <span>
                        <Badge tone="neutral">Possible unobserved tail</Badge>
                      </span>
                    </Tip>
                  )}
                  {conn.preexistingAtStart && (
                    <Tip content="This connection was already open when the Collector began observing, so any earlier traffic was never seen.">
                      <span>
                        <Badge tone="neutral">Pre-existing at start</Badge>
                      </span>
                    </Tip>
                  )}
                </div>
              )}
            </ISection>

            {/* ---- D. Lifecycle ---- */}
            <ISection title="Observation lifecycle" defaultOpen={false}>
              <dl className="kv">
                <dt>First seen</dt>
                <dd className="mono">{formatLocalDateTimeCompact(conn.firstObservedAt)}</dd>
                <dt>Last seen</dt>
                <dd className="mono">{formatLocalDateTimeCompact(conn.lastObservedAt)}</dd>
                <dt>Duration</dt>
                <dd className="mono">
                  {formatDuration(
                    new Date(conn.lastObservedAt).getTime() - new Date(conn.firstObservedAt).getTime()
                  )}
                </dd>
                <dt>State</dt>
                <dd>
                  {(() => {
                    const s = describeConnectionState(conn.state);
                    return (
                      <Tip content={s.explanation}>
                        <Badge tone={s.tone}>{s.label}</Badge>
                      </Tip>
                    );
                  })()}
                </dd>
                {conn.observationEndReason && (
                  <>
                    <dt>End reason</dt>
                    <dd style={{ fontSize: 'var(--fs-11)', color: 'var(--text-secondary)' }}>
                      {describeObservationEndReason(conn.observationEndReason)}
                    </dd>
                  </>
                )}
                <dt>Session</dt>
                <dd className="mono truncate" title={conn.sessionId}>
                  {conn.sessionId}
                </dd>
                <dt>Epoch</dt>
                <dd className="mono">{conn.epochId}</dd>
                {conn.mihomoStart && (
                  <>
                    <dt>Mihomo start</dt>
                    <dd className="mono">{formatLocalDateTimeCompact(conn.mihomoStart)}</dd>
                  </>
                )}
                {conn.startClassification && (
                  <>
                    <dt>Start class</dt>
                    <dd className="mono">{conn.startClassification}</dd>
                  </>
                )}
              </dl>
            </ISection>

            {/* ---- E. Accounting event timeline ---- */}
            <ISection
              title="Accounting event timeline"
              badge={<Badge tone="neutral" mono>{formatCount(events.length)}</Badge>}
              defaultOpen={false}
            >
              {events.length === 0 ? (
                <div style={{ fontSize: 'var(--fs-11)', color: 'var(--text-tertiary)' }}>
                  No accounting events recorded for this connection in the latest completed run.
                </div>
              ) : (
                <div className="timeline">
                  {events.map((e, i) => {
                    const prev = i > 0 ? events[i - 1] : null;
                    const changed = (a?: string, b?: string) =>
                      prev !== null && (a ?? '') !== (b ?? '') ? true : false;
                    const changes: string[] = [];
                    if (changed(e.process, prev?.process)) changes.push(`process → ${e.process || '∅'}`);
                    if (changed(e.host, prev?.host)) changes.push(`host → ${e.host || '∅'}`);
                    if (changed(e.rule, prev?.rule)) changes.push(`rule → ${e.rule || '∅'}`);
                    if (changed(e.route, prev?.route)) changes.push(`route → ${e.route}`);
                    if (changed(e.topPolicyGroup, prev?.topPolicyGroup))
                      changes.push(`group → ${e.topPolicyGroup || '∅'}`);
                    if (changed(e.finalProxy, prev?.finalProxy))
                      changes.push(`egress → ${e.finalProxy || '∅'}`);
                    if (changed(e.accountingClass, prev?.accountingClass))
                      changes.push(`class → ${e.accountingClass}`);

                    return (
                      <div className="tl-item" key={`${e.sourceEventId}-${i}`}>
                        <div className="tl-time">{formatLocalTime(e.observedAt)}</div>
                        <div className="tl-body">
                          <div className="tl-headline">
                            <span className="tl-delta">
                              ↑{formatBytes(e.accountedUpload)} ↓{formatBytes(e.accountedDownload)}
                            </span>
                            <Badge tone={e.route === 'PROXY' ? 'proxy' : e.route === 'DIRECT' ? 'direct' : 'reject'}>
                              {e.route}
                            </Badge>
                            <Badge tone={e.precision === 'exact' ? 'ok' : 'warn'} mono>
                              {e.precision}
                            </Badge>
                          </div>
                          <div className="tl-detail">
                            <span>raw ↑{formatBytesExact(e.rawUpload)}</span>
                            <span>raw ↓{formatBytesExact(e.rawDownload)}</span>
                            {e.topPolicyGroup && <span>group {e.topPolicyGroup}</span>}
                            {e.finalProxy && <span>egress {e.finalProxy}</span>}
                          </div>
                          {changes.length > 0 && (
                            <div className="tl-detail">
                              {changes.map((c) => (
                                <span key={c} className="tl-change">
                                  {c}
                                </span>
                              ))}
                            </div>
                          )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </ISection>

            {/* ---- F. Raw traffic frames ---- */}
            <ISection
              title="Raw traffic frames"
              badge={
                frames.isFetching ? (
                  <Badge tone="neutral">loading</Badge>
                ) : frames.data ? (
                  <Badge tone="neutral" mono>{formatCount(frames.data.traffic.length)}</Badge>
                ) : null
              }
              defaultOpen={false}
            >
              {!framesOpen ? (
                <button className="btn btn--sm" onClick={() => setFramesOpen(true)}>
                  <IconPulse size={12} />
                  Load raw sampling frames
                </button>
              ) : frames.isError ? (
                <ErrorState error={toProductError(frames.error)} onRetry={() => frames.refetch()} compact />
              ) : frames.isLoading ? (
                <SkeletonLine height={90} />
              ) : (frames.data?.traffic.length ?? 0) === 0 ? (
                <div style={{ fontSize: 'var(--fs-11)', color: 'var(--text-tertiary)' }}>
                  No raw traffic frames recorded for this connection.
                </div>
              ) : (
                <div className="frames">
                  <table>
                    <thead>
                      <tr>
                        <th>Observed</th>
                        <th>Δ Up</th>
                        <th>Δ Down</th>
                        <th>Up counter</th>
                        <th>Down counter</th>
                        <th>Precision</th>
                      </tr>
                    </thead>
                    <tbody>
                      {frames.data!.traffic.map((f) => (
                        <tr key={f.eventId}>
                          <td>{formatLocalTime(f.observedAt)}</td>
                          <td>{formatBytesExact(f.deltaUpload)}</td>
                          <td>{formatBytesExact(f.deltaDownload)}</td>
                          <td>{formatBytesExact(f.observedUploadCounter)}</td>
                          <td>{formatBytesExact(f.observedDownloadCounter)}</td>
                          <td className={f.precision !== 'exact' ? 'is-estimated' : ''}>{f.precision}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </ISection>

            {/* ---- Identity footer ---- */}
            <div style={{ paddingBottom: 'var(--sp-3)' }}>
              <div className="label" style={{ marginBottom: 'var(--sp-2)' }}>
                Composite identity
              </div>
              <div className="mono" style={{ fontSize: 'var(--fs-10)', color: 'var(--text-tertiary)', wordBreak: 'break-all' }}>
                {ref.sessionId} / {ref.epochId} / {ref.connectionId}
              </div>
              {conn.metadata.processPath && (
                <div className="mono" style={{ fontSize: 'var(--fs-10)', color: 'var(--text-tertiary)', wordBreak: 'break-all', marginTop: 4 }}>
                  {basenameOf(conn.metadata.processPath)}
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </aside>
  );
};
