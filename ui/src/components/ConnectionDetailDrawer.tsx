import React, { useEffect } from 'react';
import { QueryApiClient } from '../api/client';
import {
  useConnectionDetailQuery,
  useConnectionTrafficQuery,
} from '../api/queries';
import { ConnectionRecord } from '../api/types';
import { CausalChain } from './CausalChain';
import { ByteValue, Chip, RouteBadge } from './DataViz';
import { Field, FieldGroup, Panel } from './Panel';
import { LoadingBlock, StateBlock, describeError } from './StateBlock';
import { formatBytes, formatCount, formatDuration } from '../utils/format';
import { formatLocalDateTime, formatLocalTime } from '../utils/time';
import {
  OBSERVATION_END_LABELS,
  PRECISION_LABELS,
  accountingClassView,
  attributionClassView,
  qualityFlagViews,
} from '../domain/labels';

/**
 * The audit drill-down.
 *
 * PRODUCT.md 3.1 states that the system's first duty is to preserve and explain a single
 * connection, and that aggregation is a secondary capability built on top. This drawer is
 * therefore the terminal point of every number in the UI: each ranked row can be followed
 * down to the exact connection records that produced it.
 *
 * It shows the causal chain, what the reconciler decided to count (and why), the raw
 * accounting event series, and the underlying traffic frames — so a figure can always be
 * argued with, rather than taken on trust.
 */

interface Identity {
  sessionId: string;
  epochId: number;
  connectionId: string;
}

interface Props {
  client: QueryApiClient | null;
  identity: Identity | null;
  /** Fallback row data so the header can render before the detail request resolves. */
  preview?: ConnectionRecord | null;
  onClose: () => void;
}

export const ConnectionDetailDrawer: React.FC<Props> = ({ client, identity, preview, onClose }) => {
  const detailQuery = useConnectionDetailQuery(client, identity);
  const trafficQuery = useConnectionTrafficQuery(client, identity);

  useEffect(() => {
    if (!identity) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [identity, onClose]);

  if (!identity) return null;

  const conn = detailQuery.data?.connection ?? preview ?? null;
  const summary = detailQuery.data?.accountingSummary;
  const events = detailQuery.data?.accountingEvents ?? [];

  const durationMs =
    conn && conn.firstObservedAt && conn.lastObservedAt
      ? new Date(conn.lastObservedAt).getTime() - new Date(conn.firstObservedAt).getTime()
      : null;

  return (
    <>
      <div className="pl-scrim" onClick={onClose} aria-hidden="true" />
      <aside className="pl-drawer" role="dialog" aria-modal="true" aria-label="连接审计详情">
        <header className="pl-drawer__head">
          <div>
            <div className="pl-drawer__eyebrow">连接审计</div>
            <h2 className="pl-drawer__title">
              {conn?.metadata?.host ||
                conn?.metadata?.sniffHost ||
                conn?.metadata?.destinationIP ||
                '未识别目标'}
            </h2>
            <div className="pl-drawer__sub">
              <RouteBadge route={conn?.route} />
              {conn && <Chip mono>{conn.metadata?.network?.toUpperCase() ?? '—'}</Chip>}
              {conn?.observationActive && <Chip tone="ok">仍在观测</Chip>}
            </div>
          </div>
          <button className="pl-btn pl-btn--ghost" onClick={onClose} aria-label="关闭">
            ✕
          </button>
        </header>

        <div className="pl-drawer__body">
          {detailQuery.isLoading && <LoadingBlock rows={6} label="正在载入连接审计证据…" />}
          {detailQuery.isError && (
            <StateBlock {...describeError(detailQuery.error)} />
          )}

          {conn && (
            <>
              <Panel title="因果链路" hint="按规则命中顺序呈现：谁 → 去哪 → 为什么 → 走哪里">
                <CausalChain conn={conn} />
              </Panel>

              <Panel title="流量核算" hint="原始观测与最终计入口径的对照">
                <dl className="pl-fields">
                  {summary ? (
                    <>
                      <Field label="原始观测" title="连接层面的原始字节增量，未经中继去重">
                        <ByteValue bytes={summary.rawUploadTotal + summary.rawDownloadTotal} />
                      </Field>
                      <Field label="最终计入" title="经保守中继对账后实际计入统计的字节">
                        <ByteValue
                          bytes={summary.accountedUploadTotal + summary.accountedDownloadTotal}
                          tone="strong"
                        />
                      </Field>
                      <Field label="上传 / 下载" title="最终计入口径的上下行拆分">
                        <span className="pl-pair">
                          <span className="pl-pair__up">↑ {formatBytes(summary.accountedUploadTotal)}</span>
                          <span className="pl-pair__down">
                            ↓ {formatBytes(summary.accountedDownloadTotal)}
                          </span>
                        </span>
                      </Field>
                    </>
                  ) : (
                    <>
                      <Field label="监控期累计">
                        <ByteValue bytes={conn.monitoredUploadTotal + conn.monitoredDownloadTotal} />
                      </Field>
                      <Field label="上传 / 下载">
                        <span className="pl-pair">
                          <span className="pl-pair__up">↑ {formatBytes(conn.monitoredUploadTotal)}</span>
                          <span className="pl-pair__down">
                            ↓ {formatBytes(conn.monitoredDownloadTotal)}
                          </span>
                        </span>
                      </Field>
                    </>
                  )}
                  <Field label="观测时长">
                    {durationMs !== null ? formatDuration(durationMs) : '—'}
                  </Field>
                </dl>

                {summary && (
                  <div className="pl-drawer__note">
                    <Chip
                      tone={
                        summary.accountingClass === 'confirmed_relay_duplicate'
                          ? 'info'
                          : summary.accountingClass === 'ambiguous_relay' ||
                              summary.accountingClass === 'missing_attribution'
                            ? 'warn'
                            : 'ok'
                      }
                    >
                      {accountingClassView(summary.accountingClass).label}
                    </Chip>
                    <span>{accountingClassView(summary.accountingClass).detail}</span>
                  </div>
                )}
              </Panel>

              <Panel title="身份与时间" hint="连接由 (session, epoch, connection) 三元组唯一确定">
                <dl className="pl-fields">
                  <Field label="Connection ID" title="Mihomo 连接 ID，单独并不唯一">
                    {conn.connectionId}
                  </Field>
                  <Field label="Session / Epoch">
                    {conn.sessionId} / {conn.epochId}
                  </Field>
                  <Field label="首次观测">{formatLocalDateTime(conn.firstObservedAt)}</Field>
                  <Field label="最后观测">{formatLocalDateTime(conn.lastObservedAt)}</Field>
                  <Field label="观测结束">
                    {conn.observationEndReason
                      ? OBSERVATION_END_LABELS[conn.observationEndReason] ?? conn.observationEndReason
                      : '—'}
                  </Field>
                  <Field label="冷启动既有连接" title="采集器启动时已存在的连接，其基线流量不计入当期">
                    {conn.preexistingAtStart ? '是（已作基线，不计入）' : '否'}
                  </Field>
                  {conn.possibleUnobservedTail && (
                    <Field label="可能存在未观测尾部">
                      <Chip tone="warn">是</Chip>
                    </Field>
                  )}
                </dl>
              </Panel>

              <Panel title="证据完整度" hint="缺失项按类型分别标注，绝不合并为一个「未知」">
                {qualityFlagViews({
                  metadata: conn.metadata,
                  rule: conn.rule,
                  chains: conn.chains,
                }).length === 0 ? (
                  <div className="pl-empty-note">该连接具备完整的进程、域名、规则与代理链证据。</div>
                ) : (
                  <ul className="pl-flag-list">
                    {qualityFlagViews({
                      metadata: conn.metadata,
                      rule: conn.rule,
                      chains: conn.chains,
                    }).map((f) => (
                      <li key={f.id}>
                        <Chip tone="warn" mono>
                          {f.label}
                        </Chip>
                        <span>{f.detail}</span>
                      </li>
                    ))}
                  </ul>
                )}
                <dl className="pl-fields">
                  <Field label="进程路径" mono>
                    <span className="pl-wrap">{conn.metadata?.processPath || '—'}</span>
                  </Field>
                  <Field label="Sniff Host">{conn.metadata?.sniffHost || '—'}</Field>
                  <Field label="目标">{conn.metadata?.destinationIP || '—'}</Field>
                  <Field label="端口">
                    {conn.metadata?.destinationPort
                      ? `${conn.metadata.destinationPort}（源 ${conn.metadata?.sourcePort ?? '—'}）`
                      : '—'}
                  </Field>
                  <Field label="归因类别" title={attributionClassView(conn.latestAttributionClass).detail}>
                    {attributionClassView(conn.latestAttributionClass).label}
                  </Field>
                  <Field label="DNS 模式">{conn.metadata?.dnsMode || '—'}</Field>
                </dl>
              </Panel>

              <Panel
                title="核算事件时序"
                hint={`最近一次核算轮次下的 ${formatCount(events.length)} 条计入事件`}
                flush
              >
                {events.length === 0 ? (
                  <div className="pl-empty-note">该连接在当前核算轮次中没有计入事件。</div>
                ) : (
                  <div className="pl-table-scroll">
                    <table className="pl-table">
                      <thead>
                        <tr>
                          <th>时间</th>
                          <th>精度</th>
                          <th className="is-num">Δ 上行</th>
                          <th className="is-num">Δ 下行</th>
                          <th>规则</th>
                        </tr>
                      </thead>
                      <tbody>
                        {events.map((ev) => (
                          <tr key={`${ev.runId}-${ev.sourceEventId}`}>
                            <td className="is-mono">{formatLocalTime(ev.observedAt)}</td>
                            <td>
                              <Chip
                                tone={ev.precision === 'exact' ? 'ok' : 'warn'}
                                title={
                                  ev.precision === 'exact'
                                    ? '直接来自计数器差值的精确值'
                                    : '由观测区间推算的分配值'
                                }
                              >
                                {PRECISION_LABELS[ev.precision] ?? ev.precision}
                              </Chip>
                            </td>
                            <td className="is-num is-mono">{formatBytes(ev.accountedUpload)}</td>
                            <td className="is-num is-mono">{formatBytes(ev.accountedDownload)}</td>
                            <td className="is-mono">{ev.rule ? `${ev.rule},${ev.rulePayload ?? ''}` : '—'}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </Panel>

              <Panel
                title="流量增量帧"
                hint="采集器实际观测到的原始字节增量序列"
                flush
                actions={
                  trafficQuery.isError ? (
                    <Chip tone="danger" title={String(trafficQuery.error?.message ?? '')}>
                      载入失败
                    </Chip>
                  ) : undefined
                }
              >
                {trafficQuery.isLoading && <LoadingBlock rows={3} />}
                {trafficQuery.isError && <StateBlock {...describeError(trafficQuery.error)} compact />}
                {trafficQuery.data && trafficQuery.data.traffic.length === 0 && (
                  <div className="pl-empty-note">没有记录到流量增量帧。</div>
                )}
                {trafficQuery.data && trafficQuery.data.traffic.length > 0 && (
                  <div className="pl-table-scroll">
                    <table className="pl-table">
                      <thead>
                        <tr>
                          <th>时间</th>
                          <th>精度</th>
                          <th className="is-num">Δ 上行</th>
                          <th className="is-num">Δ 下行</th>
                          <th className="is-num">累计</th>
                        </tr>
                      </thead>
                      <tbody>
                        {trafficQuery.data.traffic.map((f) => (
                          <tr key={f.eventId}>
                            <td className="is-mono">{formatLocalTime(f.observedAt)}</td>
                            <td>
                              <Chip tone={f.precision === 'exact' ? 'ok' : 'warn'}>
                                {PRECISION_LABELS[f.precision] ?? f.precision}
                              </Chip>
                            </td>
                            <td className="is-num is-mono">{formatBytes(f.deltaUpload)}</td>
                            <td className="is-num is-mono">{formatBytes(f.deltaDownload)}</td>
                            <td className="is-num is-mono">
                              {formatBytes(f.monitoredUploadTotal + f.monitoredDownloadTotal)}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </Panel>
            </>
          )}
        </div>
      </aside>
    </>
  );
};

/** Small helper used by surfaces that render a nested group of fields. */
export const DetailSection: React.FC<{ label: string; children: React.ReactNode }> = ({
  label,
  children,
}) => (
  <FieldGroup label={label}>
    <dl className="pl-fields">{children}</dl>
  </FieldGroup>
);
