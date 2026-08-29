import React, { useState } from 'react';
import { QueryApiClient } from '../../api/client';
import { AccountedTrafficRecord, ConnectionRecord } from '../../api/types';
import { useAuditContext } from '../../state/AuditContext';
import { useConnectionDetailQuery, useConnectionTrafficQuery } from '../../api/queries';
import {
  CopyButton,
  EvidenceChip,
  ErrorState,
  KeyValue,
  RouteBadge,
  Section,
  SkeletonRows,
} from '../../components/ui/primitives';
import { IconClose } from '../../components/ui/icons';
import { formatBytes } from '../../utils/format';
import { formatLocalDateTime } from '../../utils/time';
import { qualityFlagLabels } from '../../utils/qualityFlags';
import { isNoAccountingRunError } from '../common/PageGate';

function formatTs(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

const accountingClassLabel = (cls?: string): { label: string; kind: 'neutral' | 'estimated' | 'ambiguous' | 'missing' } => {
  switch (cls) {
    case 'known_application':
      return { label: 'Known application', kind: 'neutral' };
    case 'direct_application':
      return { label: 'Direct application', kind: 'neutral' };
    case 'system_proxy_inbound':
      return { label: 'System proxy inbound', kind: 'neutral' };
    case 'ambiguous_relay_candidate':
      return { label: 'Ambiguous relay candidate', kind: 'ambiguous' };
    case 'unpaired_missing_attribution':
      return { label: 'Unpaired missing attribution', kind: 'missing' };
    default:
      return { label: cls || 'Unknown class', kind: 'neutral' };
  }
};

const CausalPath: React.FC<{ c: ConnectionRecord; topGroup?: string }> = ({ c, topGroup }) => {
  const chains = c.chains ?? [];
  const finalEgress = chains[0];
  const top = topGroup ?? (chains.length > 1 ? chains[chains.length - 1] : undefined);
  const intermediates = chains.length > 2 ? chains.slice(1, chains.length - 1) : [];
  const dest = c.metadata?.host || c.metadata?.sniffHost;
  const ip = c.metadata?.destinationIP;
  const port = c.metadata?.destinationPort;

  const steps: { key: string; value: React.ReactNode; mono?: boolean; accent?: boolean }[] = [
    {
      key: 'Process',
      value: c.metadata?.process ? (
        <>
          {c.metadata.process}
          {c.metadata.processPath && (
            <span className="pl-cell-sub pl-mono" title={c.metadata.processPath}>{c.metadata.processPath}</span>
          )}
        </>
      ) : (
        <EvidenceChip kind="missing" label="Missing process" title="Mihomo did not report a process for this connection" />
      ),
    },
    {
      key: 'Destination',
      value: dest ? (
        <>
          {dest}
          {ip && <span className="pl-cell-sub pl-mono">{ip}{port ? `:${port}` : ''}</span>}
        </>
      ) : ip ? (
        <>
          <EvidenceChip kind="missing" label="IP-only" title="No host or sniffed domain was available" />{' '}
          {ip}{port ? `:${port}` : ''}
        </>
      ) : (
        <EvidenceChip kind="missing" label="Missing destination" />
      ),
      mono: !dest,
    },
    {
      key: 'Rule',
      value: c.rule ? (
        <span className="pl-mono">
          {c.rule}
          {c.rulePayload ? <span className="pl-secondary"> · {c.rulePayload}</span> : null}
        </span>
      ) : (
        <EvidenceChip kind="missing" label="Missing rule" title="No rule recorded for this connection" />
      ),
    },
  ];

  if (top) steps.push({ key: 'Top policy', value: <span className="pl-mono">{top}</span> });
  if (intermediates.length > 0) {
    steps.push({ key: 'Proxy chain', value: <span className="pl-mono">{intermediates.join(' → ')}</span> });
  }
  steps.push({
    key: 'Egress',
    value: <span className="pl-mono">{finalEgress ?? (c.route === 'DIRECT' ? 'DIRECT' : '(unknown)')}</span>,
    accent: (c.route || '').toUpperCase() === 'PROXY',
  });

  return (
    <ol className="pl-causal">
      {steps.map((s) => (
        <li key={s.key} className={`pl-causal__step${s.accent ? ' pl-causal__step--accent' : ''}`}>
          <span className="pl-causal__key">{s.key}</span>
          <span className={`pl-causal__value${s.mono ? ' pl-causal__value--mono' : ''}`}>{s.value}</span>
        </li>
      ))}
    </ol>
  );
};

const EventTimeline: React.FC<{ events: AccountedTrafficRecord[] }> = ({ events }) => {
  if (events.length === 0) {
    return <div className="pl-muted pl-small">No accounting events recorded for this connection.</div>;
  }
  let prev: AccountedTrafficRecord | null = null;
  return (
    <div>
      {events.map((ev, idx) => {
        const changed =
          prev !== null &&
          (prev.process !== ev.process ||
            prev.host !== ev.host ||
            prev.rule !== ev.rule ||
            prev.rulePayload !== ev.rulePayload ||
            prev.finalProxy !== ev.finalProxy ||
            prev.route !== ev.route);
        prev = ev;
        return (
          <div key={`${ev.sourceEventId}-${idx}`} className={`pl-event-row${changed ? ' pl-event-row--changed' : ''}`}>
            <span className="pl-event-row__time">{formatTs(ev.observedAt)}</span>
            <span className="pl-event-row__body">
              <span>{ev.process ?? '-'}</span>
              <span>→ {ev.host ?? ev.destinationIp ?? '-'}</span>
              {ev.rule && <span>· {ev.rule}{ev.rulePayload ? ` ${ev.rulePayload}` : ''}</span>}
              {ev.finalProxy && <span>· {ev.finalProxy}</span>}
              <span>· ↑{formatBytes(ev.accountedUpload)} ↓{formatBytes(ev.accountedDownload)}</span>
              {ev.precision === 'interval_derived' && (
                <EvidenceChip kind="estimated" label="interval" title="Interval-derived (estimated) accounting precision" />
              )}
              {changed && (
                <EvidenceChip kind="ambiguous" label="evidence changed" title="Routing/attribution evidence evolved at this event (node switch, rule update or relay pairing)" />
              )}
            </span>
          </div>
        );
      })}
    </div>
  );
};

export const ConnectionInspector: React.FC<{ client: QueryApiClient | null }> = ({ client }) => {
  const { selected, setSelected } = useAuditContext();
  const [showRawFrames, setShowRawFrames] = useState(false);

  const detailQ = useConnectionDetailQuery(client, selected);
  const trafficQ = useConnectionTrafficQuery(client, selected, showRawFrames && !!selected);

  if (!selected) {
    return (
      <aside className="pl-inspector" aria-label="Connection inspector">
        <div className="pl-inspector__placeholder">
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
            <circle cx="10.5" cy="10.5" r="6" />
            <path d="M15 15l5.5 5.5" />
          </svg>
          <span>Select a connection to inspect its causal path, accounting evidence and lifecycle.</span>
        </div>
      </aside>
    );
  }

  const detail = detailQ.data;
  const c = detail?.connection;
  const summary = detail?.accountingSummary;
  const events = detail?.accountingEvents ?? [];
  const cls = accountingClassLabel(summary?.accountingClass ?? c?.latestAttributionClass);
  const hasInterval = events.some((e) => e.precision === 'interval_derived');
  const identity = `${selected.sessionId} · epoch ${selected.epochId} · ${selected.connectionId}`;

  return (
    <aside className="pl-inspector" aria-label="Connection inspector">
      <div className="pl-inspector__header">
        <div className="pl-inspector__title-row">
          <span className="pl-inspector__title">
            {c?.metadata?.process ?? 'Connection'}
            {c && (c.metadata?.host || c.metadata?.sniffHost || c.metadata?.destinationIP)
              ? ` → ${c.metadata.host || c.metadata.sniffHost || c.metadata.destinationIP}`
              : ''}
          </span>
          {c && <RouteBadge route={c.route} />}
          <button className="pl-icon-btn" aria-label="Close inspector" title="Close inspector (History context is preserved)" onClick={() => setSelected(null)}>
            <IconClose />
          </button>
        </div>
        <div className="pl-inspector__id-row">
          <span className="pl-inspector__id" title={identity}>{identity}</span>
          <CopyButton text={identity} title="Copy composite identity" />
        </div>
      </div>

      <div className="pl-inspector__body">
        {detailQ.isLoading ? (
          <SkeletonRows rows={10} />
        ) : detailQ.isError ? (
          isNoAccountingRunError(detailQ.error) ? (
            <Section title="Causal path">
              {c ? <CausalPath c={c} /> : null}
              <div className="pl-muted pl-small" style={{ marginTop: 8 }}>
                Accounting evidence is unavailable: no completed accounting run exists for this connection yet.
              </div>
            </Section>
          ) : (
            <ErrorState
              title="Connection detail unavailable"
              body={String((detailQ.error as Error)?.message ?? detailQ.error)}
            />
          )
        ) : c ? (
          <>
            <Section title="Causal path" sub="Why this traffic happened, in forward order">
              <CausalPath c={c} topGroup={summary?.latestTopPolicyGroup} />
            </Section>

            <Section title="Traffic accounting" sub={hasInterval ? 'Contains interval-derived (estimated) portions' : 'Exact per-event accounting'}>
              {summary ? (
                <>
                  <KeyValue
                    items={[
                      { key: 'Upload', value: `${formatBytes(summary.accountedUploadTotal)} (raw ${formatBytes(summary.rawUploadTotal)})`, mono: true },
                      { key: 'Download', value: `${formatBytes(summary.accountedDownloadTotal)} (raw ${formatBytes(summary.rawDownloadTotal)})`, mono: true },
                      { key: 'Total', value: formatBytes(summary.accountedUploadTotal + summary.accountedDownloadTotal), mono: true },
                      { key: 'Route', value: <RouteBadge route={summary.route} quiet /> },
                      { key: 'Class', value: <EvidenceChip kind={cls.kind} label={cls.label} /> },
                    ]}
                  />
                  {summary.accountedUploadTotal !== summary.rawUploadTotal || summary.accountedDownloadTotal !== summary.rawDownloadTotal ? (
                    <div className="pl-muted pl-small" style={{ marginTop: 6 }}>
                      Accounted values differ from raw counters after conservative relay reconciliation; ambiguous relay traffic is kept, never silently dropped.
                    </div>
                  ) : null}
                </>
              ) : (
                <KeyValue
                  items={[
                    { key: 'Upload', value: formatBytes(c.monitoredUploadTotal), mono: true },
                    { key: 'Download', value: formatBytes(c.monitoredDownloadTotal), mono: true },
                    { key: 'Total', value: formatBytes(c.monitoredUploadTotal + c.monitoredDownloadTotal), mono: true },
                  ]}
                />
              )}
              <div className="pl-muted pl-small" style={{ marginTop: 6 }} title="Unrounded integer bytes for reconciliation">
                Exact integers: ↑ {summary ? summary.accountedUploadTotal : c.monitoredUploadTotal} B · ↓ {summary ? summary.accountedDownloadTotal : c.monitoredDownloadTotal} B
              </div>
            </Section>

            <Section title="Evidence quality">
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                <EvidenceChip kind={cls.kind} label={cls.label} title={`Attribution class: ${summary?.accountingClass ?? c.latestAttributionClass ?? 'n/a'}`} />
                {c.preexistingAtStart && (
                  <EvidenceChip kind="estimated" label="Preexisting at session start" title="Already active when the collector session began; baseline counters excluded from increments" />
                )}
                {c.possibleUnobservedTail && (
                  <EvidenceChip kind="estimated" label="Possible unobserved tail" title="Final traffic after last observation may be unrecorded" />
                )}
                {qualityFlagLabels(c.qualityFlags).map((f) => (
                  <EvidenceChip key={f} kind="neutral" label={f} />
                ))}
                {!c.metadata?.process && <EvidenceChip kind="missing" label="Missing process" />}
                {!c.metadata?.host && !c.metadata?.sniffHost && <EvidenceChip kind="missing" label="Missing host" />}
                {!c.rule && <EvidenceChip kind="missing" label="Missing rule" />}
                {(!c.chains || c.chains.length === 0) && <EvidenceChip kind="missing" label="Missing chain" />}
              </div>
              {c.relayEvidence && Object.keys(c.relayEvidence).length > 0 && (
                <details className="pl-disclosure" style={{ marginTop: 8 }}>
                  <summary>Relay evidence detail</summary>
                  <pre className="pl-mono pl-small" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', color: 'var(--pl-text-secondary)' }}>
                    {JSON.stringify(c.relayEvidence, null, 2)}
                  </pre>
                </details>
              )}
            </Section>

            <Section title="Lifecycle">
              <KeyValue
                items={[
                  { key: 'First observed', value: formatLocalDateTime(c.firstObservedAt), mono: true },
                  { key: 'Last observed', value: formatLocalDateTime(c.lastObservedAt), mono: true },
                  { key: 'State', value: c.observationActive ? 'Active (still observed)' : `Ended (${c.state})` },
                  { key: 'End reason', value: c.observationEndReason ?? '—' },
                  { key: 'Session', value: c.sessionId, mono: true },
                  { key: 'Epoch', value: String(c.epochId), mono: true },
                  { key: 'Connection ID', value: c.connectionId, mono: true },
                ]}
              />
            </Section>

            <Section title="Accounting events" sub={`${events.length} event(s), oldest → newest · evidence evolution preserved`}>
              <EventTimeline events={events} />
            </Section>

            <Section title="Raw traffic frames" sub="Advanced · sampled delta evidence">
              <details className="pl-disclosure" open={showRawFrames} onToggle={(e) => setShowRawFrames((e.target as HTMLDetailsElement).open)}>
                <summary>{showRawFrames ? 'Hide raw frames' : 'Show raw frames'}</summary>
                {showRawFrames && (
                  <div style={{ marginTop: 8 }}>
                    {trafficQ.isLoading ? (
                      <SkeletonRows rows={4} />
                    ) : trafficQ.isError ? (
                      <div className="pl-muted pl-small">Raw traffic frames unavailable: {String((trafficQ.error as Error)?.message ?? trafficQ.error)}</div>
                    ) : (trafficQ.data?.traffic ?? []).length === 0 ? (
                      <div className="pl-muted pl-small">No raw traffic frames recorded for this connection.</div>
                    ) : (
                      <div>
                        <div className="pl-traffic-frame-row" style={{ color: 'var(--pl-text-muted)', borderBottom: '1px solid var(--pl-border)' }}>
                          <span>Time</span>
                          <span>Precision</span>
                          <span style={{ textAlign: 'right' }}>Δ↑ / Δ↓</span>
                          <span style={{ textAlign: 'right' }}>Counters ↑ / ↓</span>
                        </div>
                        {(trafficQ.data?.traffic ?? []).map((f) => (
                          <div key={f.eventId} className="pl-traffic-frame-row">
                            <span title={f.observedAt}>{formatTs(f.observedAt)}</span>
                            <span>{f.precision === 'exact' ? 'exact' : <EvidenceChip kind="estimated" label="interval" />}</span>
                            <span style={{ textAlign: 'right' }}>
                              {formatBytes(f.deltaUpload)} / {formatBytes(f.deltaDownload)}
                            </span>
                            <span style={{ textAlign: 'right' }}>
                              {formatBytes(f.observedUploadCounter)} / {formatBytes(f.observedDownloadCounter)}
                            </span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </details>
            </Section>
          </>
        ) : (
          <div className="pl-muted pl-small" style={{ padding: '12px 0' }}>
            The Query API returned no connection payload for this identity
            ({identity}). The connection may have been removed by a newer
            accounting run. Close the inspector to return to the history list.
          </div>
        )}
      </div>
    </aside>
  );
};
