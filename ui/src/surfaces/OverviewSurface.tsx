import React, { useState } from 'react';
import { QueryApiClient } from '../api/client';
import {
  useConnectionsQuery,
  useCoverageQuery,
  useMetaQuery,
  useProtocolsQuery,
  useSummaryQuery,
  useTopFinalProxiesQuery,
  useTopHostsQuery,
  useTopProcessesQuery,
} from '../api/queries';
import { ConnectionRecord } from '../api/types';
import { useAppFilters } from '../state/AppFilters';
import { Panel } from '../components/Panel';
import { ByteValue, MetricStat, RouteCompositionBar, TrustDot } from '../components/DataViz';
import { LoadingBlock, QueryState, StateBlock, describeError } from '../components/StateBlock';
import { ConnectionsTable } from '../components/ConnectionsTable';
import { RankedTable, buildDimensionRows } from '../components/RankedTable';
import { ConnectionDetailDrawer } from '../components/ConnectionDetailDrawer';
import { integritySignals, overallTrust } from '../domain/integrity';
import { routeBytes, routeComposition } from '../domain/route';
import { formatBytes } from '../utils/format';
import { formatLocalShort } from '../utils/time';

/**
 * Overview answers three questions in this order, and deliberately in this order:
 *
 *   1. 可以相信这些数字吗   — the integrity strip sits above the totals, because a big
 *                            number printed without its coverage and attribution caveats
 *                            is exactly the "unknown black box" this product replaces.
 *   2. 代理流量有多大       — the hero figure is PROXY bytes. DIRECT is shown beside it as
 *                            context, never added into it (PRODUCT scenario C).
 *   3. 这些流量属于谁       — the attribution ledger, dimension-switchable, each row
 *                            drilling into the underlying connection records.
 */

type LedgerDimension = 'process' | 'host' | 'node' | 'network';

const LEDGER_OPTIONS: { id: LedgerDimension; label: string; hint: string }[] = [
  { id: 'process', label: '进程', hint: '哪个程序产生了这些流量' },
  { id: 'host', label: '目标', hint: '流量发往哪些域名' },
  { id: 'node', label: '节点', hint: '经由哪些出站节点' },
  { id: 'network', label: '协议', hint: 'TCP 与 UDP 的分布' },
];

export const OverviewSurface: React.FC<{ client: QueryApiClient | null }> = ({ client }) => {
  const { timeWindow, routeScope, drillToHistory, liveMode, refreshToken } = useAppFilters();
  const [dimension, setDimension] = useState<LedgerDimension>('process');
  const [selected, setSelected] = useState<ConnectionRecord | null>(null);

  const refetchInterval = liveMode ? 15_000 : false;

  const metaQuery = useMetaQuery(client, liveMode ? 15_000 : false);
  const summaryQuery = useSummaryQuery(client, timeWindow, routeScope, refetchInterval);
  const coverageQuery = useCoverageQuery(client, timeWindow);
  const processesQuery = useTopProcessesQuery(client, timeWindow, routeScope, 10);
  const hostsQuery = useTopHostsQuery(client, timeWindow, routeScope, 10);
  const nodesQuery = useTopFinalProxiesQuery(client, timeWindow, routeScope, 10);
  const protocolsQuery = useProtocolsQuery(client, timeWindow, routeScope, 10);
  const recentQuery = useConnectionsQuery(client, timeWindow, routeScope, { limit: 12 }, refetchInterval);

  const summary = summaryQuery.data;
  const freshness = summary?.freshness ?? metaQuery.data?.freshness;
  const trust = overallTrust(summary, freshness);
  const signals = integritySignals(summary, freshness);

  const proxy = routeBytes(summary, 'PROXY');
  const direct = routeBytes(summary, 'DIRECT');
  const reject = routeBytes(summary, 'REJECT');
  const composition = routeComposition(summary);

  const ledgerQuery =
    dimension === 'process'
      ? processesQuery
      : dimension === 'host'
        ? hostsQuery
        : dimension === 'node'
          ? nodesQuery
          : protocolsQuery;

  const ledgerDrill = (key: string) => {
    if (dimension === 'process') drillToHistory({ process: key });
    else if (dimension === 'host') drillToHistory({ host: key });
    else if (dimension === 'network') drillToHistory({ network: key });
    // The node dimension has no direct filter in the v1 connections endpoint,
    // so it falls through to the unfiltered ledger rather than pretending to filter.
    else drillToHistory({});
  };

  const ledgerRows = buildDimensionRows(ledgerQuery.data?.items, ledgerDrill);

  return (
    <div className="pl-surface" key={refreshToken}>
      {summaryQuery.isError ? (
        <StateBlock {...describeError(summaryQuery.error)} />
      ) : (
        <>
          {/* 1. Integrity first. */}
          <Panel
            title="数据完整性"
            hint="在看任何流量数字之前，先确认这些数字在多大程度上可信"
            emphasis
            actions={
              <div className={`pl-trust pl-trust--${trust.level}`}>
                <TrustDot level={trust.level} />
                <span className="pl-trust__text">{trust.headline}</span>
              </div>
            }
          >
            {summaryQuery.isLoading ? (
              <LoadingBlock rows={1} />
            ) : (
              <>
                <div className="pl-integrity">
                  {signals.map((s) => (
                    <div key={s.id} className={`pl-integrity__item pl-integrity__item--${s.level}`} title={s.detail}>
                      <TrustDot level={s.level} />
                      <div className="pl-integrity__label">{s.label}</div>
                      <div className="pl-integrity__value">{s.value}</div>
                    </div>
                  ))}
                </div>
                <p className="pl-integrity__detail">{trust.detail}</p>
              </>
            )}
          </Panel>

          {/* 2. Magnitude. */}
          <div className="pl-grid pl-grid--metrics">
            <Panel
              title="代理流量"
              hint="经代理节点出站的上下行总量，这是本产品的默认视角"
              emphasis
            >
              {summaryQuery.isLoading ? (
                <LoadingBlock rows={2} />
              ) : (
                <>
                  <MetricStat
                    label="PROXY 总流量"
                    value={formatBytes(proxy.total).replace(/ [A-Za-z]+$/, '')}
                    unit={formatBytes(proxy.total).split(' ').pop()}
                    accent="var(--pl-route-proxy)"
                    emphasis
                    title={proxy.total.toLocaleString() + ' bytes'}
                  />
                  <div className="pl-pair pl-pair--lg">
                    <span className="pl-pair__up">↑ {formatBytes(proxy.upload)}</span>
                    <span className="pl-pair__down">↓ {formatBytes(proxy.download)}</span>
                  </div>
                </>
              )}
            </Panel>

            <Panel title="出站构成" hint="代理 / 直连 / 拦截 / 未定 的字节占比">
              {summaryQuery.isLoading ? (
                <LoadingBlock rows={3} />
              ) : (
                <>
                  <RouteCompositionBar segments={composition} height={10} />
                  <dl className="pl-fields pl-fields--tight">
                    <div className="pl-field">
                      <dt className="pl-field__label">
                        <i className="pl-swatch" style={{ background: 'var(--pl-route-proxy)' }} />
                        代理
                      </dt>
                      <dd className="pl-field__value is-mono">
                        <ByteValue bytes={proxy.total} />
                      </dd>
                    </div>
                    <div className="pl-field">
                      <dt className="pl-field__label">
                        <i className="pl-swatch" style={{ background: 'var(--pl-route-direct)' }} />
                        直连
                      </dt>
                      <dd className="pl-field__value is-mono">
                        <ByteValue bytes={direct.total} tone="muted" />
                      </dd>
                    </div>
                    <div className="pl-field">
                      <dt className="pl-field__label">
                        <i className="pl-swatch" style={{ background: 'var(--pl-route-reject)' }} />
                        拦截
                      </dt>
                      <dd className="pl-field__value is-mono">
                        <ByteValue bytes={reject.total} tone="muted" />
                      </dd>
                    </div>
                  </dl>
                  <p className="pl-panel__note">
                    直连流量不计入代理节点消耗。此处保留它是为了验证分流是否符合预期，
                    而不是把它加进上面的代理总量。
                  </p>
                </>
              )}
            </Panel>

            <Panel title="观测与核算" hint="口径与时效">
              {summaryQuery.isLoading ? (
                <LoadingBlock rows={3} />
              ) : (
                <dl className="pl-fields pl-fields--tight">
                  <div className="pl-field">
                    <dt className="pl-field__label" title="连接层原始观测字节，未经中继去重">
                      原始观测
                    </dt>
                    <dd className="pl-field__value is-mono">
                      <ByteValue
                        bytes={(summary?.rawObservedUpload ?? 0) + (summary?.rawObservedDownload ?? 0)}
                      />
                    </dd>
                  </div>
                  <div className="pl-field">
                    <dt className="pl-field__label" title="扣除已确认中继重复后的唯一观测字节">
                      去重后
                    </dt>
                    <dd className="pl-field__value is-mono">
                      <ByteValue
                        bytes={(summary?.uniqueObservedUpload ?? 0) + (summary?.uniqueObservedDownload ?? 0)}
                      />
                    </dd>
                  </div>
                  <div className="pl-field">
                    <dt className="pl-field__label" title="缺少归因信息、保留待核查的字节">
                      待核查
                    </dt>
                    <dd className="pl-field__value is-mono">
                      <ByteValue
                        bytes={
                          (summary?.missingAttributionUpload ?? 0) + (summary?.missingAttributionDownload ?? 0)
                        }
                      />
                    </dd>
                  </div>
                  <div className="pl-field">
                    <dt className="pl-field__label" title="歧义中继：疑似重复但证据不足，按保守策略不扣减">
                      歧义中继
                    </dt>
                    <dd className="pl-field__value is-mono">
                      <ByteValue
                        bytes={(summary?.ambiguousRelayUpload ?? 0) + (summary?.ambiguousRelayDownload ?? 0)}
                      />
                    </dd>
                  </div>
                  <div className="pl-field">
                    <dt className="pl-field__label" title="快照轮询未能捕获的物理流量">
                      采样残差
                    </dt>
                    <dd className="pl-field__value is-mono">
                      <ByteValue
                        bytes={(summary?.samplingResidualUpload ?? 0) + (summary?.samplingResidualDownload ?? 0)}
                      />
                    </dd>
                  </div>
                  <div className="pl-field">
                    <dt className="pl-field__label">核算算法</dt>
                    <dd className="pl-field__value is-mono">{summary?.accountingVersion || '—'}</dd>
                  </div>
                </dl>
              )}
            </Panel>
          </div>

          {/* 3. Attribution ledger. */}
          <Panel
            title="消耗归因"
            hint="点击任意一行，可直接下钻到产生该行的连接记录"
            actions={
              <div className="pl-seg pl-seg--sm">
                {LEDGER_OPTIONS.map((o) => (
                  <button
                    key={o.id}
                    type="button"
                    className={`pl-seg__btn ${dimension === o.id ? 'is-active' : ''}`}
                    onClick={() => setDimension(o.id)}
                    title={o.hint}
                  >
                    {o.label}
                  </button>
                ))}
              </div>
            }
          >
            <QueryState
              isLoading={ledgerQuery.isLoading}
              isError={ledgerQuery.isError}
              error={ledgerQuery.error}
              isEmpty={ledgerRows.length === 0}
              emptyTitle="该维度下没有记录"
              emptyDetail="当前窗口与范围内没有可用于排行的数据。可尝试放宽时间窗口或切换出站方式。"
              loadingRows={6}
            >
              <RankedTable rows={ledgerRows} colorByRoute />
              {dimension === 'node' && (
                <p className="pl-panel__note">
                  当前 Query API 的连接检索不支持按出站节点过滤，因此节点行下钻会打开未过滤的连接台账。
                </p>
              )}
            </QueryState>
          </Panel>

          {/* 4. Evidence. */}
          <Panel
            title="最近连接"
            hint="最新的连接记录，点击任意一行查看完整审计证据"
            flush
            actions={
              recentQuery.data && (
                <span className="pl-panel__meta">
                  显示 {recentQuery.data.items.length} 条
                  {recentQuery.data.hasMore ? '+' : ''}
                </span>
              )
            }
          >
            <QueryState
              isLoading={recentQuery.isLoading}
              isError={recentQuery.isError}
              error={recentQuery.error}
              isEmpty={recentQuery.data?.items.length === 0}
              emptyDetail="当前窗口内没有连接记录。"
              loadingRows={6}
            >
              <ConnectionsTable items={recentQuery.data?.items ?? []} onSelect={setSelected} />
            </QueryState>
            {coverageQuery.data?.coverageRatio !== undefined && (
              <p className="pl-panel__note">
                窗口 {formatLocalShort(timeWindow?.from)} → {formatLocalShort(timeWindow?.to)}，监控覆盖{' '}
                {((coverageQuery.data.coverageRatio ?? 0) * 100).toFixed(1)}%。
              </p>
            )}
          </Panel>
        </>
      )}

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
