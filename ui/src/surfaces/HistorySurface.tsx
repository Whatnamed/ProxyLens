import React, { useEffect, useMemo, useState } from 'react';
import { QueryApiClient } from '../api/client';
import { useConnectionsQuery } from '../api/queries';
import { ConnectionRecord } from '../api/types';
import { useAppFilters } from '../state/AppFilters';
import { Panel } from '../components/Panel';
import { Chip } from '../components/DataViz';
import { QueryState } from '../components/StateBlock';
import { ConnectionsTable } from '../components/ConnectionsTable';
import { ConnectionDetailDrawer } from '../components/ConnectionDetailDrawer';
import { formatLocalShort } from '../utils/time';

/**
 * The connection ledger.
 *
 * PRODUCT.md 3.1: "审计优先于统计" — the single connection is the primary artifact and the
 * aggregates are built on top of it. This surface is therefore not a secondary "logs" page;
 * it is where any number from the other surfaces gets verified.
 *
 * Filters map one-to-one onto what the Query API actually supports (process, host,
 * destinationIp, network). There is deliberately no free-text "search everything" box, because
 * the API cannot honour one, and a filter that silently ignores part of its input is worse
 * than four explicit fields.
 */

const PAGE_SIZE = 50;

interface DraftFilters {
  process: string;
  host: string;
  destinationIp: string;
  network: string;
}

const EMPTY: DraftFilters = { process: '', host: '', destinationIp: '', network: '' };

export const HistorySurface: React.FC<{ client: QueryApiClient | null }> = ({ client }) => {
  const { timeWindow, routeScope, drill, clearDrill, liveMode } = useAppFilters();

  // A drill picked elsewhere in the UI seeds this surface, after which the user can refine
  // it further with the local filter form.
  const [draft, setDraft] = useState<DraftFilters>(EMPTY);
  const [applied, setApplied] = useState<DraftFilters>(EMPTY);
  const [page, setPage] = useState(0);
  const [selected, setSelected] = useState<ConnectionRecord | null>(null);

  useEffect(() => {
    const next: DraftFilters = {
      process: drill.process ?? '',
      host: drill.host ?? '',
      destinationIp: drill.destinationIp ?? '',
      network: drill.network ?? '',
    };
    setDraft(next);
    setApplied(next);
    setPage(0);
  }, [drill.process, drill.host, drill.destinationIp, drill.network]);

  const offset = page * PAGE_SIZE;

  const connectionsQuery = useConnectionsQuery(
    client,
    timeWindow,
    routeScope,
    {
      process: applied.process || undefined,
      host: applied.host || undefined,
      destinationIp: applied.destinationIp || undefined,
      network: applied.network || undefined,
      limit: PAGE_SIZE,
      offset,
    },
    liveMode ? 15_000 : false
  );

  const items = connectionsQuery.data?.items ?? [];
  const hasMore = connectionsQuery.data?.hasMore ?? false;

  const activeCount = useMemo(
    () => Object.values(applied).filter((v) => v.trim() !== '').length,
    [applied]
  );

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setApplied(draft);
    setPage(0);
  };

  const reset = () => {
    setDraft(EMPTY);
    setApplied(EMPTY);
    setPage(0);
    clearDrill();
  };

  return (
    <div className="pl-surface">
      <Panel
        title="连接检索"
        hint="四个字段分别对应 Query API 支持的检索维度，留空表示不限"
        actions={
          activeCount > 0 ? (
            <button type="button" className="pl-btn pl-btn--ghost pl-btn--xs" onClick={reset}>
              清除全部筛选
            </button>
          ) : undefined
        }
      >
        <form className="pl-filter-form" onSubmit={submit}>
          <label className="pl-filter">
            <span>进程</span>
            <input
              type="text"
              value={draft.process}
              placeholder="chrome.exe"
              onChange={(e) => setDraft({ ...draft, process: e.target.value })}
            />
          </label>
          <label className="pl-filter">
            <span>目标域名</span>
            <input
              type="text"
              value={draft.host}
              placeholder="github.com"
              onChange={(e) => setDraft({ ...draft, host: e.target.value })}
            />
          </label>
          <label className="pl-filter">
            <span>目标 IP</span>
            <input
              type="text"
              value={draft.destinationIp}
              placeholder="140.82.112.3"
              onChange={(e) => setDraft({ ...draft, destinationIp: e.target.value })}
            />
          </label>
          <label className="pl-filter pl-filter--narrow">
            <span>协议</span>
            <input
              type="text"
              value={draft.network}
              placeholder="tcp / udp"
              onChange={(e) => setDraft({ ...draft, network: e.target.value })}
            />
          </label>
          <button type="submit" className="pl-btn pl-btn--primary">
            检索
          </button>
        </form>

        {activeCount > 0 && (
          <div className="pl-active-filters">
            {applied.process && <Chip tone="info" mono>进程 = {applied.process}</Chip>}
            {applied.host && <Chip tone="info" mono>域名 = {applied.host}</Chip>}
            {applied.destinationIp && <Chip tone="info" mono>IP = {applied.destinationIp}</Chip>}
            {applied.network && <Chip tone="info" mono>协议 = {applied.network}</Chip>}
          </div>
        )}
      </Panel>

      <Panel
        title="连接台账"
        hint="点击任意一行打开该连接的完整审计证据"
        flush
        actions={
          connectionsQuery.data && (
            <div className="pl-pager">
              <button
                type="button"
                className="pl-btn pl-btn--ghost pl-btn--xs"
                onClick={() => setPage((p) => Math.max(0, p - 1))}
                disabled={page === 0}
              >
                上一页
              </button>
              <span className="pl-pager__label">
                第 {page + 1} 页 · 偏移 {offset}
              </span>
              <button
                type="button"
                className="pl-btn pl-btn--ghost pl-btn--xs"
                onClick={() => setPage((p) => p + 1)}
                disabled={!hasMore}
              >
                下一页
              </button>
            </div>
          )
        }
      >
        <QueryState
          isLoading={connectionsQuery.isLoading}
          isError={connectionsQuery.isError}
          error={connectionsQuery.error}
          isEmpty={items.length === 0}
          emptyTitle="没有匹配的连接"
          emptyDetail={
            activeCount > 0
              ? '当前筛选条件下没有连接记录。可放宽筛选或扩大时间窗口。'
              : '当前时间窗口与出站范围内没有任何连接记录。'
          }
          loadingRows={12}
        >
          <ConnectionsTable items={items} onSelect={setSelected} highlight={applied} />
        </QueryState>

        {timeWindow && (
          <p className="pl-panel__note">
            窗口 {formatLocalShort(timeWindow.from)} → {formatLocalShort(timeWindow.to)}，
            本页 {items.length} 条{hasMore ? '（还有更多）' : '（已到末页）'}。
          </p>
        )}
      </Panel>

      <ConnectionDetailDrawer
        client={client}
        identity={
          selected
            ? {
                sessionId: selected.sessionId,
                epochId: selected.epochId,
                connectionId: selected.connectionId,
              }
            : null
        }
        preview={selected}
        onClose={() => setSelected(null)}
      />
    </div>
  );
};
