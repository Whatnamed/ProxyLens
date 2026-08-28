import React from 'react';
import { ByteValue, ProportionBar, RouteBadge } from './DataViz';
import { formatCount } from '../utils/format';
import { routeColor } from '../domain/route';

/**
 * The workhorse view of the whole product: a ranked list of "who consumed the proxy".
 *
 * Ranked rows with an inline share bar beat a separate chart here, because the thing the
 * user does next is read the *name* (a process, a host, a rule) and then act on it. A bar
 * chart would force a detour through a legend to recover the same information.
 *
 * Every row is clickable: selecting it narrows the connection ledger to the records that
 * produced the row, which keeps every aggregate figure traceable to its evidence.
 */

export interface RankedRow {
  id: string;
  primary: React.ReactNode;
  secondary?: React.ReactNode;
  /** Route pill; omitted for dimensions that are already scoped to one route. */
  route?: string | null;
  badge?: React.ReactNode;
  total: number;
  upload: number;
  download: number;
  connections?: number;
  /** 0..1 relative to the largest row in the current list. */
  share: number;
  dimmed?: boolean;
  onClick?: () => void;
  title?: string;
}

export const RankedTable: React.FC<{
  rows: RankedRow[];
  showConnections?: boolean;
  /** Renders the share bar using a per-row route colour instead of the accent. */
  colorByRoute?: boolean;
}> = ({ rows, showConnections = true, colorByRoute = false }) => {
  if (rows.length === 0) {
    return <div className="pl-empty-note">当前窗口与范围内没有可排行的记录。</div>;
  }

  return (
    <div className="pl-rank">
      {rows.map((row, i) => {
        const interactive = !!row.onClick;
        return (
          <div
            key={row.id}
            className={`pl-rank__row ${interactive ? 'is-interactive' : ''} ${row.dimmed ? 'is-dimmed' : ''}`}
            onClick={row.onClick}
            role={interactive ? 'button' : undefined}
            tabIndex={interactive ? 0 : undefined}
            onKeyDown={
              interactive
                ? (e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      row.onClick?.();
                    }
                  }
                : undefined
            }
            title={row.title}
          >
            <span className="pl-rank__idx">{String(i + 1).padStart(2, '0')}</span>

            <div className="pl-rank__ident">
              <div className="pl-rank__primary">
                {row.primary}
                {row.badge}
              </div>
              {row.secondary && <div className="pl-rank__secondary">{row.secondary}</div>}
              <ProportionBar
                ratio={row.share}
                color={colorByRoute ? routeColor(row.route) : undefined}
                height={3}
              />
            </div>

            {row.route !== undefined && (
              <div className="pl-rank__route">
                <RouteBadge route={row.route} />
              </div>
            )}

            <div className="pl-rank__bytes">
              <ByteValue bytes={row.total} tone="strong" />
              <span className="pl-rank__split">
                ↑ {formatCount(row.upload)} B ↓ {formatCount(row.download)} B
              </span>
            </div>

            {showConnections && (
              <div className="pl-rank__conns">
                <span className="pl-rank__conns-num">{formatCount(row.connections ?? 0)}</span>
                <span className="pl-rank__conns-label">连接</span>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
};

/**
 * Builds ranked rows from a top-dimension API response, normalising the share against the
 * largest row so the bars are always readable even when totals are tiny.
 */
export function buildDimensionRows<T extends {
  key: string;
  route?: string;
  totalBytes: number;
  uploadBytes: number;
  downloadBytes: number;
  connectionCount: number;
}>(items: T[] | undefined, onPick: (key: string) => void): RankedRow[] {
  const list = items ?? [];
  const max = list.reduce((m, it) => Math.max(m, it.totalBytes ?? 0), 0);
  return list.map((it) => ({
    id: `${it.key}-${it.route ?? ''}`,
    primary: <span className="is-mono">{it.key || '(空)'}</span>,
    route: it.route ?? null,
    total: it.totalBytes ?? 0,
    upload: it.uploadBytes ?? 0,
    download: it.downloadBytes ?? 0,
    connections: it.connectionCount ?? 0,
    share: max > 0 ? (it.totalBytes ?? 0) / max : 0,
    onClick: () => onPick(it.key),
    title: `下钻到「${it.key || '(空)'}」的连接记录`,
  }));
}
