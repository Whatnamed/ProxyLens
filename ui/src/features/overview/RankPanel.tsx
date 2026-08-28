import React from 'react';
import { Panel, SplitBar, Tip } from '../../components/ui/primitives';
import { EmptyState, SkeletonRows } from '../../components/ui/states';
import { IconArrowRight, IconInfo } from '../../components/ui/icons';
import { formatCount, formatBytes } from '../../utils/format';

export interface RankRow {
  id: string;
  /** Primary label (process name, host, rule type, node name). */
  label: string;
  /** Secondary label (rule payload, sniff-host hint). */
  sub?: string;
  isEmptyKey?: boolean;
  uploadBytes: number;
  downloadBytes: number;
  totalBytes: number;
  connectionCount: number;
  exactBytes: number;
  estimatedBytes: number;
}

interface Props {
  title: string;
  /** The audit question this dimension answers: Who / Why / Where / Through what. */
  question: string;
  rows: RankRow[];
  isLoading: boolean;
  /**
   * The History dimension this can drill into, or null when the Query API
   * cannot filter by it.
   */
  drillFilter: 'process' | 'host' | null;
  unsupportedNote?: string;
  onDrill: (row: RankRow) => void;
}

export const RankPanel: React.FC<Props> = ({
  title,
  question,
  rows,
  isLoading,
  drillFilter,
  unsupportedNote,
  onDrill
}) => {
  const max = rows.reduce((m, r) => Math.max(m, r.totalBytes), 0);

  return (
    <Panel
      title={title}
      note={question}
      flush
      actions={
        unsupportedNote ? (
          <Tip content={unsupportedNote} align="right">
            <span
              className="badge badge--neutral"
              style={{ cursor: 'help' }}
              title={unsupportedNote}
            >
              <IconInfo size={10} />
              No filter
            </span>
          </Tip>
        ) : null
      }
    >
      {isLoading ? (
        <div style={{ padding: 'var(--sp-5)' }}>
          <SkeletonRows rows={6} />
        </div>
      ) : rows.length === 0 ? (
        <EmptyState
          glyph="empty"
          title="Nothing recorded"
          desc={`No ${title.toLowerCase()} produced traffic in this window with the current route focus.`}
        />
      ) : (
        <div className="rank-list">
          {rows.map((row, i) => (
            <button
              key={row.id}
              className="rank-row"
              onClick={() => onDrill(row)}
              title={
                drillFilter
                  ? `Open History filtered by ${drillFilter} = ${row.label}`
                  : 'Open History with the current scope (no dimension filter available)'
              }
            >
              <div className="rank-row-main">
                <span className="rank-index">{i + 1}</span>

                {row.sub ? (
                  <span className="rank-rule">
                    <span className="rank-rule-type truncate">{row.label}</span>
                    <span className="rank-rule-payload">{row.sub}</span>
                  </span>
                ) : (
                  <span className={`rank-name ${row.isEmptyKey ? 'rank-name--empty' : ''}`}>
                    {row.label}
                  </span>
                )}

                <Tip
                  align="right"
                  content={
                    <>
                      <div className="tip-row">
                        <span className="tip-label">Total</span>
                        <span>{row.totalBytes.toLocaleString('en-US')} B</span>
                      </div>
                      <div className="tip-row">
                        <span className="tip-label">Upload</span>
                        <span>{row.uploadBytes.toLocaleString('en-US')} B</span>
                      </div>
                      <div className="tip-row">
                        <span className="tip-label">Download</span>
                        <span>{row.downloadBytes.toLocaleString('en-US')} B</span>
                      </div>
                      <div className="tip-row">
                        <span className="tip-label">Exact</span>
                        <span>{row.exactBytes.toLocaleString('en-US')} B</span>
                      </div>
                      <div className="tip-row">
                        <span className="tip-label">Estimated</span>
                        <span>{row.estimatedBytes.toLocaleString('en-US')} B</span>
                      </div>
                      <div className="tip-row">
                        <span className="tip-label">Connections</span>
                        <span>{formatCount(row.connectionCount)}</span>
                      </div>
                    </>
                  }
                >
                  <span className="rank-total">{formatBytes(row.totalBytes)}</span>
                </Tip>

                <span className="rank-count">{formatCount(row.connectionCount)}</span>
                <span className="rank-drill">
                  <IconArrowRight size={12} />
                </span>
              </div>

              <div className="rank-bar">
                <SplitBar exact={row.exactBytes} estimated={row.estimatedBytes} max={max} />
              </div>
            </button>
          ))}
        </div>
      )}
    </Panel>
  );
};

/** Compact legend explaining that hatched segments are interval-derived. */
export const SplitLegend: React.FC = () => (
  <div style={{ display: 'flex', gap: 'var(--sp-5)', alignItems: 'center' }}>
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--sp-2)' }}>
      <span style={{ width: 14, height: 4, background: 'var(--accent)', borderRadius: 1 }} />
      <span className="dimmer" style={{ fontSize: 'var(--fs-10)' }}>
        Exact
      </span>
    </span>
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--sp-2)' }}>
      <span
        style={{
          width: 14,
          height: 4,
          borderRadius: 1,
          backgroundImage: 'repeating-linear-gradient(135deg, var(--warn) 0 2px, transparent 2px 4px)',
          backgroundColor: 'rgba(217,160,60,0.22)'
        }}
      />
      <span className="dimmer" style={{ fontSize: 'var(--fs-10)' }}>
        Estimated
      </span>
    </span>
  </div>
);
