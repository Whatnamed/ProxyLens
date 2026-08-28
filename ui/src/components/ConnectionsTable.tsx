import React from 'react';
import { ConnectionRecord } from '../api/types';
import { RouteBadge, Chip } from './DataViz';
import { egressNode } from '../domain/chains';
import { formatBytes } from '../utils/format';
import { formatLocalShort } from '../utils/time';

/**
 * The connection ledger.
 *
 * This is the primary evidence surface, so its columns are arranged in the order of the
 * causal chain (谁 → 去哪 → 为什么 → 走哪里 → 多少) rather than by field importance.
 * A user scanning a row should be able to read the whole story left to right.
 */

export interface ConnectionsTableProps {
  items: ConnectionRecord[];
  onSelect: (conn: ConnectionRecord) => void;
  /** Highlights a dimension value that the ledger has been narrowed to. */
  highlight?: { process?: string; host?: string; destinationIp?: string; network?: string };
  emptyNote?: React.ReactNode;
}

function targetOf(conn: ConnectionRecord): { text: string; isIpOnly: boolean } {
  const m = conn.metadata ?? {};
  if (m.host) return { text: m.host, isIpOnly: false };
  if (m.sniffHost) return { text: m.sniffHost, isIpOnly: false };
  if (m.destinationIP) return { text: m.destinationIP, isIpOnly: true };
  return { text: '未识别目标', isIpOnly: true };
}

export const ConnectionsTable: React.FC<ConnectionsTableProps> = ({
  items,
  onSelect,
  highlight = {},
  emptyNote,
}) => {
  if (items.length === 0) {
    return (
      <div className="pl-empty-note">
        {emptyNote ?? '当前窗口与筛选条件下没有连接记录。'}
      </div>
    );
  }

  return (
    <div className="pl-table-scroll">
      <table className="pl-table pl-table--rows">
        <thead>
          <tr>
            <th>时间</th>
            <th>进程</th>
            <th>目标</th>
            <th>端口</th>
            <th>协议</th>
            <th>出站</th>
            <th>命中规则</th>
            <th>节点</th>
            <th className="is-num">流量</th>
          </tr>
        </thead>
        <tbody>
          {items.map((c) => {
            const target = targetOf(c);
            const node = egressNode(c.chains);
            const hlProcess = highlight.process && c.metadata?.process === highlight.process;
            const hlHost =
              highlight.host && (c.metadata?.host === highlight.host || c.metadata?.sniffHost === highlight.host);
            const hlIp = highlight.destinationIp && c.metadata?.destinationIP === highlight.destinationIp;
            const hlNet = highlight.network && c.metadata?.network === highlight.network;

            return (
              <tr
                key={`${c.sessionId}-${c.epochId}-${c.connectionId}`}
                onClick={() => onSelect(c)}
                tabIndex={0}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') onSelect(c);
                }}
                className="pl-table__row--clickable"
                title="打开该连接的完整审计证据"
              >
                <td className="is-mono is-dim">{formatLocalShort(c.firstObservedAt)}</td>
                <td className={`is-mono ${hlProcess ? 'is-hl' : ''}`}>
                  {c.metadata?.process || <span className="is-missing">未识别</span>}
                </td>
                <td className={`is-mono ${hlHost || hlIp ? 'is-hl' : ''}`}>
                  {target.text}
                  {target.isIpOnly && (
                    <Chip tone="warn" title="仅有 IP，缺少域名证据">
                      IP
                    </Chip>
                  )}
                </td>
                <td className="is-mono is-dim">{c.metadata?.destinationPort || '—'}</td>
                <td className={`is-mono ${hlNet ? 'is-hl' : ''}`}>
                  {(c.metadata?.network || '—').toUpperCase()}
                </td>
                <td>
                  <RouteBadge route={c.route} />
                </td>
                <td className="is-mono">
                  {c.rule ? (
                    `${c.rule}${c.rulePayload ? `,${c.rulePayload}` : ''}`
                  ) : (
                    <span className="is-missing">未记录</span>
                  )}
                </td>
                <td className="is-mono">{node || <span className="is-missing">—</span>}</td>
                <td className="is-num is-mono">
                  {formatBytes(c.monitoredUploadTotal + c.monitoredDownloadTotal)}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
};
