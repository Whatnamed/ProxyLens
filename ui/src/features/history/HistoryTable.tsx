import React from 'react';
import { ConnectionRecord } from '../../api/types';
import { ConnectionRef } from '../../state/AuditContext';
import {
  basenameOf,
  displayHost,
  displayProcess,
  evidenceFlags,
  finalEgress,
  routeTone
} from '../../lib/semantics';
import { Badge, Tip } from '../../components/ui/primitives';
import { formatBytes } from '../../utils/format';
import { formatLocalDateTimeContextual } from '../../utils/time';

/**
 * The authoritative connection list: flat, newest first, one row per
 * historical connection. Overview owns aggregation; this owns evidence.
 */

interface Props {
  items: ConnectionRecord[];
  isLoading: boolean;
  selected: ConnectionRef | null;
  onSelect: (conn: ConnectionRecord) => void;
  /** Reference time used to decide whether a row needs its date shown. */
  referenceTime: string;
}

function sameRef(a: ConnectionRef | null, c: ConnectionRecord) {
  return (
    !!a &&
    a.sessionId === c.sessionId &&
    a.epochId === c.epochId &&
    a.connectionId === c.connectionId
  );
}

const EvidenceCell: React.FC<{ conn: ConnectionRecord }> = ({ conn }) => {
  const flags = evidenceFlags(conn);
  if (flags.length === 0) {
    return <span className="dimmer" style={{ fontSize: 9 }}>—</span>;
  }
  const shown = flags.slice(0, 2);
  const rest = flags.slice(2);

  return (
    <span className="cell-evidence">
      {shown.map((f) => (
        <Tip
          key={f.key}
          align="right"
          content={f.title}
        >
          <span className={`evidence-flag evidence-flag--${f.tone}`} title={f.title}>
            {f.text}
          </span>
        </Tip>
      ))}
      {rest.length > 0 && (
        <Tip
          align="right"
          content={
            <>
              {rest.map((f) => (
                <div key={f.key} style={{ marginBottom: 4 }}>
                  <strong>{f.text}</strong> — {f.title}
                </div>
              ))}
            </>
          }
        >
          <span className="evidence-flag evidence-flag--neutral">+{rest.length}</span>
        </Tip>
      )}
    </span>
  );
};

export const HistoryTable: React.FC<Props> = ({
  items,
  isLoading,
  selected,
  onSelect,
  referenceTime
}) => {
  return (
    <div className="history-table-wrap">
      <div className="htable" style={{ opacity: isLoading ? 0.55 : 1, transition: 'opacity 140ms' }}>
        <div className="htable-head" role="row">
          <span>Time</span>
          <span>Process</span>
          <span>Destination</span>
          <span>Net</span>
          <span>Route</span>
          <span>Rule</span>
          <span>Egress</span>
          <span style={{ textAlign: 'right' }}>Traffic</span>
          <span style={{ textAlign: 'right' }}>Evidence</span>
        </div>

        {items.map((c) => {
          const proc = displayProcess(c);
          const host = displayHost(c);
          const egress = finalEgress(c);
          const time = formatLocalDateTimeContextual(c.firstObservedAt, referenceTime);
          const total = c.monitoredUploadTotal + c.monitoredDownloadTotal;

          return (
            <button
              key={`${c.sessionId}-${c.epochId}-${c.connectionId}`}
              className={`htable-row ${sameRef(selected, c) ? 'is-selected' : ''}`}
              onClick={() => onSelect(c)}
              role="row"
              title="Open Connection Inspector"
            >
              <span className="cell-time">
                {time.date && <span className="cell-time-date">{time.date}</span>}
                {time.time}
              </span>

              <span
                className={`cell-process ${proc.missing ? 'cell-process--missing' : ''}`}
                title={c.metadata.processPath ?? proc.name}
              >
                {proc.name}
              </span>

              <Tip
                content={
                  <>
                    <div className="tip-row">
                      <span className="tip-label">
                        {host.kind === 'host' ? 'Host' : host.kind === 'sniff' ? 'Sniffed' : 'IP'}
                      </span>
                      <span>{host.value}</span>
                    </div>
                    {c.metadata.destinationIP && (
                      <div className="tip-row">
                        <span className="tip-label">Destination IP</span>
                        <span>{c.metadata.destinationIP}</span>
                      </div>
                    )}
                    {c.metadata.destinationPort && (
                      <div className="tip-row">
                        <span className="tip-label">Port</span>
                        <span>{c.metadata.destinationPort}</span>
                      </div>
                    )}
                    {c.metadata.processPath && (
                      <div className="tip-row">
                        <span className="tip-label">Path</span>
                        <span>{basenameOf(c.metadata.processPath)}</span>
                      </div>
                    )}
                  </>
                }
              >
                <span className="cell-dest">
                  {host.value}
                  {c.metadata.destinationPort && (
                    <span className="cell-dest-port">:{c.metadata.destinationPort}</span>
                  )}
                </span>
              </Tip>

              <span className="cell-net">{c.metadata.network ?? '—'}</span>

              <span>
                <Badge tone={routeTone(c.route) as 'proxy' | 'direct' | 'reject' | 'all'}>
                  {c.route || '—'}
                </Badge>
              </span>

              <span className="cell-rule" title={c.rulePayload ? `${c.rule} (${c.rulePayload})` : c.rule}>
                {c.rule || <span style={{ color: 'var(--text-disabled)' }}>Match</span>}
                {c.rulePayload && <span className="cell-rule-payload"> {c.rulePayload}</span>}
              </span>

              <span className={`cell-egress ${egress ? '' : 'cell-egress--none'}`} title={egress ?? ''}>
                {egress ?? (c.route === 'DIRECT' ? 'Direct' : '—')}
              </span>

              <Tip
                align="right"
                content={
                  <>
                    <div className="tip-row">
                      <span className="tip-label">Total</span>
                      <span>{total.toLocaleString('en-US')} B</span>
                    </div>
                    <div className="tip-row">
                      <span className="tip-label">Upload</span>
                      <span>{c.monitoredUploadTotal.toLocaleString('en-US')} B</span>
                    </div>
                    <div className="tip-row">
                      <span className="tip-label">Download</span>
                      <span>{c.monitoredDownloadTotal.toLocaleString('en-US')} B</span>
                    </div>
                  </>
                }
              >
                <span className="cell-traffic">{formatBytes(total)}</span>
              </Tip>

              <EvidenceCell conn={c} />
            </button>
          );
        })}
      </div>
    </div>
  );
};
