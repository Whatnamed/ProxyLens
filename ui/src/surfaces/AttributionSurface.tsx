import React, { useMemo, useState } from 'react';
import { QueryApiClient } from '../api/client';
import {
  useProtocolsQuery,
  useSummaryQuery,
  useTopFinalProxiesQuery,
  useTopHostsQuery,
  useTopProcessesQuery,
} from '../api/queries';
import { TopDimensionItem } from '../api/types';
import { useAppFilters } from '../state/AppFilters';
import { Panel } from '../components/Panel';
import { ByteValue } from '../components/DataViz';
import { LoadingBlock, QueryState } from '../components/StateBlock';
import { RankedTable, RankedRow } from '../components/RankedTable';
import { formatPercent } from '../utils/format';

/**
 * Attribution is the deep counterpart to the overview ledger: same question, but at full
 * depth and with a concentration reading.
 *
 * Concentration ("the top 5 processes account for 81% of proxy traffic") is the number that
 * actually changes user behaviour — it tells you whether your proxy usage is dominated by a
 * few things you can reason about, or spread across a long tail that no rule edit will fix.
 */

type Dimension = 'process' | 'host' | 'node' | 'network';

const DIMENSIONS: {
  id: Dimension;
  label: string;
  question: string;
  hint: string;
  /** Which connection filter this dimension can be drilled into, if any. */
  drillKey?: 'process' | 'host' | 'network';
}[] = [
  { id: 'process', label: '进程', question: '谁产生了这些流量', hint: '按发起进程聚合', drillKey: 'process' },
  { id: 'host', label: '目标', question: '流量去了哪里', hint: '按请求域名聚合', drillKey: 'host' },
  { id: 'node', label: '节点', question: '经由哪个节点出站', hint: '按最终出站节点聚合' },
  { id: 'network', label: '协议', question: 'TCP 还是 UDP', hint: '按传输层协议聚合', drillKey: 'network' },
];

const DEEP_LIMIT = 50;

export const AttributionSurface: React.FC<{ client: QueryApiClient | null }> = ({ client }) => {
  const { timeWindow, routeScope, drillToHistory, liveMode } = useAppFilters();
  const [dimension, setDimension] = useState<Dimension>('process');

  const summaryQuery = useSummaryQuery(client, timeWindow, routeScope, liveMode ? 15_000 : false);
  const processesQuery = useTopProcessesQuery(client, timeWindow, routeScope, DEEP_LIMIT);
  const hostsQuery = useTopHostsQuery(client, timeWindow, routeScope, DEEP_LIMIT);
  const nodesQuery = useTopFinalProxiesQuery(client, timeWindow, routeScope, DEEP_LIMIT);
  const protocolsQuery = useProtocolsQuery(client, timeWindow, routeScope, DEEP_LIMIT);

  const active = DIMENSIONS.find((d) => d.id === dimension)!;

  const query =
    dimension === 'process'
      ? processesQuery
      : dimension === 'host'
        ? hostsQuery
        : dimension === 'node'
          ? nodesQuery
          : protocolsQuery;

  const items = query.data?.items ?? [];

  const stats = useMemo(() => {
    const total = items.reduce((sum, it) => sum + (it.totalBytes ?? 0), 0);
    const top5 = items.slice(0, 5).reduce((sum, it) => sum + (it.totalBytes ?? 0), 0);
    const conns = items.reduce((sum, it) => sum + (it.connectionCount ?? 0), 0);
    return {
      total,
      top5Share: total > 0 ? top5 / total : null,
      conns,
      distinct: items.length,
    };
  }, [items]);

  const max = items.reduce((m, it) => Math.max(m, it.totalBytes ?? 0), 0);

  // How much of the route's own total this dimension accounts for. A dimension that only
  // explains 60% of the bytes is telling you the rest has no value for this dimension at
  // all — which is itself an audit signal, not a rounding error.
  const routeTotal = useMemo(() => {
    const s = summaryQuery.data;
    if (!s) return 0;
    switch (routeScope) {
      case 'DIRECT':
        return (s.directUpload ?? 0) + (s.directDownload ?? 0);
      case 'REJECT':
        return (s.rejectUpload ?? 0) + (s.rejectDownload ?? 0);
      case 'ALL':
        return (s.rawObservedUpload ?? 0) + (s.rawObservedDownload ?? 0);
      default:
        return (s.proxyUpload ?? 0) + (s.proxyDownload ?? 0);
    }
  }, [summaryQuery.data, routeScope]);

  const dimensionCoverage = routeTotal > 0 ? stats.total / routeTotal : null;

  const rows: RankedRow[] = items.map((it: TopDimensionItem) => ({
    id: `${it.key}-${it.route ?? ''}`,
    primary: <span className="is-mono">{it.key || '(空)'}</span>,
    route: it.route ?? null,
    total: it.totalBytes ?? 0,
    upload: it.uploadBytes ?? 0,
    download: it.downloadBytes ?? 0,
    connections: it.connectionCount ?? 0,
    share: max > 0 ? (it.totalBytes ?? 0) / max : 0,
    onClick: active.drillKey ? () => drillToHistory({ [active.drillKey!]: it.key }) : undefined,
    title: active.drillKey
      ? `下钻到「${it.key || '(空)'}」的连接记录`
      : '当前 Query API 不支持按该维度检索连接',
  }));

  return (
    <div className="pl-surface">
      <div className="pl-seg pl-seg--tabs" role="tablist" aria-label="归因维度">
        {DIMENSIONS.map((d) => (
          <button
            key={d.id}
            type="button"
            role="tab"
            aria-selected={dimension === d.id}
            className={`pl-seg__btn pl-seg__btn--tab ${dimension === d.id ? 'is-active' : ''}`}
            onClick={() => setDimension(d.id)}
          >
            <span className="pl-seg__btn-label">{d.label}</span>
            <span className="pl-seg__btn-sub">{d.question}</span>
          </button>
        ))}
      </div>

      <div className="pl-grid pl-grid--stats">
        <Panel title={`${active.label}总数`} hint="当前窗口内该维度出现的不同取值数量">
          {query.isLoading ? (
            <LoadingBlock rows={1} />
          ) : (
            <>
              <div className="pl-stat">{stats.distinct}</div>
              <div className="pl-stat__sub">{active.hint}</div>
            </>
          )}
        </Panel>
        <Panel title="窗口内总量" hint={`该维度全部取值的字节合计（${routeScope === 'ALL' ? '全部出站方式' : routeScope}）`}>
          {query.isLoading ? (
            <LoadingBlock rows={1} />
          ) : (
            <>
              <div className="pl-stat">
                <ByteValue bytes={stats.total} tone="strong" />
              </div>
              <div className="pl-stat__sub">共 {stats.conns.toLocaleString()} 条连接</div>
            </>
          )}
        </Panel>
        <Panel title="头部集中度" hint="前 5 项占该维度总量的比例">
          {query.isLoading ? (
            <LoadingBlock rows={1} />
          ) : (
            <>
              <div className="pl-stat">{formatPercent(stats.top5Share)}</div>
              <div className="pl-stat__sub">
                {stats.top5Share !== null && stats.top5Share > 0.8
                  ? '高度集中，少数目标即可解释大部分流量'
                  : stats.top5Share !== null && stats.top5Share > 0.5
                    ? '中度集中，仍有一条明显的长尾'
                    : '较为分散，需按长尾逐项排查'}
              </div>
            </>
          )}
        </Panel>
        <Panel
          title="口径对照"
          hint="同窗口同出站方式的总量，用于判断该维度合计是否解释了全部流量"
        >
          {summaryQuery.isLoading ? (
            <LoadingBlock rows={1} />
          ) : (
            <>
              <div className="pl-stat">
                <ByteValue bytes={routeTotal} />
              </div>
              <div className="pl-stat__sub">
                {dimensionCoverage === null
                  ? '窗口内无可对照的总量'
                  : `本维度合计解释了其中 ${formatPercent(dimensionCoverage)}，差额来自未归因或缺失该维度值的流量`}
              </div>
            </>
          )}
        </Panel>
      </div>

      <Panel
        title={`${active.label}排行`}
        hint={
          active.drillKey
            ? '点击任意一行可下钻到产生该行的连接记录'
            : '当前 Query API 不支持按该维度检索连接，因此行不可下钻'
        }
      >
        <QueryState
          isLoading={query.isLoading}
          isError={query.isError}
          error={query.error}
          isEmpty={rows.length === 0}
          emptyTitle={`没有 ${active.label} 维度的记录`}
          emptyDetail="当前窗口与出站范围内没有可用于排行的数据。"
          loadingRows={10}
        >
          <RankedTable rows={rows} colorByRoute />
          {items.length >= DEEP_LIMIT && (
            <p className="pl-panel__note">
              仅显示前 {DEEP_LIMIT} 项。更长的长尾可在「连接」台账中按具体值检索。
            </p>
          )}
        </QueryState>
      </Panel>
    </div>
  );
};
