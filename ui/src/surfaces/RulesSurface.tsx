import React, { useMemo, useState } from 'react';
import { QueryApiClient } from '../api/client';
import { useTopRulesQuery } from '../api/queries';
import { TopRuleItem } from '../api/types';
import { useAppFilters } from '../state/AppFilters';
import { Panel } from '../components/Panel';
import { ByteValue, Chip, ProportionBar, RouteBadge } from '../components/DataViz';
import { LoadingBlock, QueryState } from '../components/StateBlock';
import { dominantTone, estimateShare, ruleAuditFlags } from '../domain/rules';
import { routeColor } from '../domain/route';
import { formatBytes, formatCount, formatPercent } from '../utils/format';

/**
 * Rules is the surface that leads somewhere.
 *
 * Every other surface explains what happened; this one explains WHY, because the rule is the
 * only part of the causal chain the user can actually change. It is therefore the only place
 * where ProxyLens offers an opinion — and even then, the opinion is a named, explained flag
 * ("兜底规则", "全量 UDP 代理") derived from the rule row itself, never a black-box score.
 *
 * A rule row cannot be drilled into the connection ledger: the v1 connections endpoint filters
 * on process / host / destinationIp / network only. Rather than fake a filtered view, the rows
 * are non-interactive and the limitation is stated.
 */

type SortKey = 'bytes' | 'connections';

export const RulesSurface: React.FC<{ client: QueryApiClient | null }> = ({ client }) => {
  const { timeWindow, routeScope, liveMode } = useAppFilters();
  const [sort, setSort] = useState<SortKey>('bytes');
  const [onlyFlagged, setOnlyFlagged] = useState(false);

  const rulesQuery = useTopRulesQuery(client, timeWindow, routeScope, 100, liveMode ? 15_000 : false);

  const items = rulesQuery.data?.items ?? [];

  const analysis = useMemo(() => {
    const total = items.reduce((s, it) => s + (it.totalBytes ?? 0), 0);
    const byFlag = {
      fallback: 0,
      broadUdp: 0,
      missing: 0,
    };
    items.forEach((it) => {
      ruleAuditFlags(it).forEach((f) => {
        if (f.id === 'match-fallback') byFlag.fallback += it.totalBytes ?? 0;
        if (f.id === 'broad-udp') byFlag.broadUdp += it.totalBytes ?? 0;
        if (f.id === 'missing-rule') byFlag.missing += it.totalBytes ?? 0;
      });
    });
    return { total, byFlag };
  }, [items]);

  const flaggedItems = useMemo(
    () => (onlyFlagged ? items.filter((it) => ruleAuditFlags(it).length > 0) : items),
    [items, onlyFlagged]
  );

  const sorted = useMemo(() => {
    const copy = [...flaggedItems];
    copy.sort((a, b) =>
      sort === 'bytes' ? (b.totalBytes ?? 0) - (a.totalBytes ?? 0) : (b.connectionCount ?? 0) - (a.connectionCount ?? 0)
    );
    return copy;
  }, [flaggedItems, sort]);

  const max = sorted.reduce((m, it) => Math.max(m, it.totalBytes ?? 0), 0);

  const fallbackShare = analysis.total > 0 ? analysis.byFlag.fallback / analysis.total : null;
  const udpShare = analysis.total > 0 ? analysis.byFlag.broadUdp / analysis.total : null;
  const missingShare = analysis.total > 0 ? analysis.byFlag.missing / analysis.total : null;

  return (
    <div className="pl-surface">
      <div className="pl-grid pl-grid--stats">
        <Panel
          title="兜底规则占比"
          hint="命中 MATCH / FINAL 的流量占比。数值偏高意味着大量流量没有专属规则"
        >
          {rulesQuery.isLoading ? (
            <LoadingBlock rows={1} />
          ) : (
            <>
              <div className="pl-stat">
                <ByteValue bytes={analysis.byFlag.fallback} tone="strong" />
              </div>
              <div className="pl-stat__sub">
                占本口径 {formatPercent(fallbackShare)}
                {fallbackShare !== null && fallbackShare > 0.25
                  ? ' — 偏高，建议为头部目标补写明确规则'
                  : ''}
              </div>
            </>
          )}
        </Panel>
        <Panel title="宽泛 UDP 规则" hint="NETWORK,udp 一类规则送入代理的流量">
          {rulesQuery.isLoading ? (
            <LoadingBlock rows={1} />
          ) : (
            <>
              <div className="pl-stat">
                <ByteValue bytes={analysis.byFlag.broadUdp} tone="strong" />
              </div>
              <div className="pl-stat__sub">
                占本口径 {formatPercent(udpShare)}
                {udpShare !== null && udpShare > 0.1
                  ? ' — 值得检查是否有背景 UDP 被误送代理'
                  : ''}
              </div>
            </>
          )}
        </Panel>
        <Panel title="无规则记录" hint="内核未回报命中规则的流量，无法解释其分流依据">
          {rulesQuery.isLoading ? (
            <LoadingBlock rows={1} />
          ) : (
            <>
              <div className="pl-stat">
                <ByteValue bytes={analysis.byFlag.missing} tone="strong" />
              </div>
              <div className="pl-stat__sub">
                占本口径 {formatPercent(missingShare)}
                {missingShare !== null && missingShare > 0.05 ? ' — 存在解释不了的流量' : ''}
              </div>
            </>
          )}
        </Panel>
        <Panel title="规则总数" hint="当前窗口内命中的不同 (rule, payload) 组合数">
          {rulesQuery.isLoading ? (
            <LoadingBlock rows={1} />
          ) : (
            <>
              <div className="pl-stat">{items.length}</div>
              <div className="pl-stat__sub">
                {onlyFlagged ? `其中 ${sorted.length} 条命中审计标记` : '按流量降序排列'}
              </div>
            </>
          )}
        </Panel>
      </div>

      <Panel
        title="规则命中排行"
        hint="按 (规则, 匹配值) 聚合；标记列说明为什么这条规则值得检查"
        actions={
          <div className="pl-panel__actions-row">
            <div className="pl-seg pl-seg--sm">
              <button
                type="button"
                className={`pl-seg__btn ${sort === 'bytes' ? 'is-active' : ''}`}
                onClick={() => setSort('bytes')}
              >
                按流量
              </button>
              <button
                type="button"
                className={`pl-seg__btn ${sort === 'connections' ? 'is-active' : ''}`}
                onClick={() => setSort('connections')}
              >
                按连接数
              </button>
            </div>
            <button
              type="button"
              className={`pl-toggle ${onlyFlagged ? 'is-on' : ''}`}
              onClick={() => setOnlyFlagged((v) => !v)}
              aria-pressed={onlyFlagged}
            >
              仅看有标记
            </button>
          </div>
        }
        flush
      >
        <QueryState
          isLoading={rulesQuery.isLoading}
          isError={rulesQuery.isError}
          error={rulesQuery.error}
          isEmpty={sorted.length === 0}
          emptyTitle={onlyFlagged ? '没有命中审计标记的规则' : '窗口内没有规则记录'}
          emptyDetail={
            onlyFlagged
              ? '当前窗口内所有规则的形态都在预期范围内。'
              : '当前窗口与出站范围内没有可用于排行的数据。'
          }
          loadingRows={10}
        >
          <div className="pl-table-scroll">
            <table className="pl-table pl-table--rules">
              <thead>
                <tr>
                  <th className="is-num">#</th>
                  <th>规则</th>
                  <th>匹配值</th>
                  <th>出站</th>
                  <th>标记</th>
                  <th className="is-num">流量</th>
                  <th className="is-num">连接</th>
                  <th className="pl-col-share">占比</th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((it: TopRuleItem, i) => {
                  const flags = ruleAuditFlags(it);
                  const tone = dominantTone(flags);
                  const est = estimateShare(it);
                  return (
                    <tr key={`${it.rule}-${it.rulePayload}-${it.route}-${i}`}>
                      <td className="is-num is-dim">{i + 1}</td>
                      <td className="is-mono">{it.rule || <span className="is-missing">未记录</span>}</td>
                      <td className="is-mono">{it.rulePayload || <span className="is-dim">—</span>}</td>
                      <td>
                        <RouteBadge route={it.route} />
                      </td>
                      <td>
                        <div className="pl-flag-cell">
                          {flags.length === 0 && <span className="is-dim">—</span>}
                          {flags.map((f) => (
                            <Chip key={f.id} tone={f.tone} title={f.reason}>
                              {f.label}
                            </Chip>
                          ))}
                          {est > 0 && (
                            <Chip tone="info" title={`其中 ${formatPercent(est)} 为区间推算值，非精确计数`}>
                              含推算 {formatPercent(est, 0)}
                            </Chip>
                          )}
                        </div>
                      </td>
                      <td className="is-num is-mono">
                        {formatBytes(it.totalBytes)}
                        <span className="pl-cell-sub">
                          ↑{formatBytes(it.uploadBytes)} ↓{formatBytes(it.downloadBytes)}
                        </span>
                      </td>
                      <td className="is-num is-mono">{formatCount(it.connectionCount)}</td>
                      <td className="pl-col-share">
                        <ProportionBar
                          ratio={max > 0 ? (it.totalBytes ?? 0) / max : 0}
                          color={tone ? `var(--pl-${tone === 'danger' ? 'danger' : tone === 'warn' ? 'warn' : 'accent'})` : routeColor(it.route)}
                          height={4}
                        />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </QueryState>
      </Panel>

      <Panel title="使用说明" hint="这一页的标记是怎么来的">
        <ul className="pl-note-list">
          <li>
            标记完全由当前窗口内 <code>(rule, rulePayload)</code> 行的取值推导，不依赖任何跨窗口历史或黑盒评分。
          </li>
          <li>
            ProxyLens 为只读工具，不会改写 Mihomo 配置。若此处提示分流缺口，需要你在自己的配置文件中手动调整规则。
          </li>
          <li>
            当前 Query API 的连接检索不支持按规则过滤，因此规则行不能直接下钻到连接明细；可在「连接」页按进程或目标检索相关记录。
          </li>
        </ul>
      </Panel>
    </div>
  );
};
