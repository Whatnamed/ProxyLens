import React from 'react';
import { QueryApiClient } from '../api/client';
import { useCoverageQuery, useMetaQuery } from '../api/queries';
import { useAppFilters } from '../state/AppFilters';
import { Panel, Field } from '../components/Panel';
import { Chip, TrustDot } from '../components/DataViz';
import { CoverageTimeline, GapList } from '../components/CoverageTimeline';
import { LoadingBlock, QueryState } from '../components/StateBlock';
import { coverageState } from '../domain/integrity';
import { formatDuration } from '../utils/format';
import { formatLocalDateTime, formatLocalShort } from '../utils/time';

/**
 * Coverage.
 *
 * PRODUCT.md 3.5 and scenario D: the tool must be able to say where it was blind. This
 * surface exists so that "we do not know" is an inspectable object — a time range with a
 * reason — instead of a gap in a bar chart or a suspiciously round total.
 *
 * The distinction drawn throughout is:
 *   monitored          : the collector was observing
 *   gap                : the collector was NOT observing, for a recorded reason
 *   outside known scope: before the collector ever ran; not a gap, just unknown
 *   future             : the window extends past now; nothing has happened yet
 */

export const CoverageSurface: React.FC<{ client: QueryApiClient | null }> = ({ client }) => {
  const { timeWindow, liveMode } = useAppFilters();
  const coverageQuery = useCoverageQuery(client, timeWindow);
  const metaQuery = useMetaQuery(client, liveMode ? 15_000 : false);

  const coverage = coverageQuery.data;
  const state = coverageState(coverage);
  const session = metaQuery.data?.latestCollectorSession;

  return (
    <div className="pl-surface">
      <Panel
        title="监控覆盖"
        hint="所选窗口内采集器实际处于观测状态的时间占比"
        emphasis
        actions={
          <div className={`pl-trust pl-trust--${state.level}`}>
            <TrustDot level={state.level} />
            <span className="pl-trust__text">{state.label}</span>
          </div>
        }
      >
        <QueryState
          isLoading={coverageQuery.isLoading}
          isError={coverageQuery.isError}
          error={coverageQuery.error}
          isEmpty={!coverage}
          emptyTitle="没有覆盖数据"
          emptyDetail="无法为当前窗口计算监控覆盖率。"
          loadingRows={4}
        >
          {coverage && (
            <>
              <CoverageTimeline coverage={coverage} />
              <dl className="pl-fields pl-fields--grid">
                <Field label="已监控时长">{formatDuration(coverage.coveredDurationMs)}</Field>
                <Field label="缺口时长">{formatDuration(coverage.uncoveredDurationMs)}</Field>
                <Field label="Controller 中断" title="Controller 数据流中断造成的未监控时长">
                  {formatDuration(coverage.controllerGapDurationMs)}
                </Field>
                <Field label="采集器离线" title="采集进程未运行造成的未监控时长">
                  {formatDuration(coverage.collectorOfflineDurationMs)}
                </Field>
                <Field label="超出已知范围" title="窗口早于采集器最早的观测记录">
                  {formatDuration(coverage.outsideKnownScopeMs)}
                </Field>
                <Field label="尚未发生" title="窗口末端超出当前时间的部分">
                  {formatDuration(coverage.futureDurationMs)}
                </Field>
                <Field label="已知范围起点">
                  {coverage.knownScopeStart ? formatLocalDateTime(coverage.knownScopeStart) : '—'}
                </Field>
                <Field label="评估区间" title="实际参与覆盖率计算的区间">
                  {coverage.effectiveScopeStart && coverage.effectiveScopeEnd
                    ? `${formatLocalShort(coverage.effectiveScopeStart)} → ${formatLocalShort(
                        coverage.effectiveScopeEnd
                      )}`
                    : '—'}
                </Field>
              </dl>
              {coverage.outsideKnownScopeMs > 0 && (
                <p className="pl-panel__note">
                  窗口起点早于采集器的已知范围，超出部分按「没有观测记录」计，
                  不计入覆盖率分母，也不会被当作未知流量呈现。
                </p>
              )}
              {state.ratio !== null && (
                <p className="pl-panel__note">
                  覆盖率是<strong>时间覆盖</strong>，表示采集器在线观察的时间比例，
                  不等价于字节级覆盖达到同样比例。
                </p>
              )}
            </>
          )}
        </QueryState>
      </Panel>

      <Panel
        title="缺口明细"
        hint="每个缺口都带有原因；没有原因的缺口会被原样显示，不做猜测"
        flush
        actions={
          coverage && (
            <span className="pl-panel__meta">{coverage.mergedGaps?.length ?? 0} 个缺口区间</span>
          )
        }
      >
        {coverageQuery.isLoading ? (
          <LoadingBlock rows={4} />
        ) : (
          <GapList gaps={coverage?.mergedGaps ?? []} />
        )}
      </Panel>

      <Panel title="采集会话" hint="最近一次后台采集会话的生命周期状态" flush>
        <QueryState
          isLoading={metaQuery.isLoading}
          isError={metaQuery.isError}
          error={metaQuery.error}
          isEmpty={!session}
          emptyTitle="没有采集会话"
          emptyDetail="数据库中还没有任何采集会话记录。"
          loadingRows={4}
        >
          {session && (
            <dl className="pl-fields pl-fields--grid">
              <Field label="会话 ID">{session.sessionId}</Field>
              <Field label="状态">
                <Chip
                  tone={
                    session.status === 'running' ? 'ok' : session.status === 'closed_clean' ? 'neutral' : 'danger'
                  }
                >
                  {session.status === 'running'
                    ? '运行中'
                    : session.status === 'closed_clean'
                      ? '正常关闭'
                      : '异常中断'}
                </Chip>
              </Field>
              <Field label="启动时间">{formatLocalDateTime(session.startedAt)}</Field>
              <Field label="最后事件">{formatLocalDateTime(session.lastEventAt)}</Field>
              <Field label="最后心跳">{formatLocalDateTime(session.lastHeartbeatAt)}</Field>
              <Field label="心跳间隔">
                {session.heartbeatIntervalMs ? formatDuration(session.heartbeatIntervalMs) : '—'}
              </Field>
              <Field label="结束时间">{session.endedAt ? formatLocalDateTime(session.endedAt) : '—'}</Field>
              <Field label="采集器版本">{session.collectorVersion || '—'}</Field>
            </dl>
          )}
        </QueryState>
        <p className="pl-panel__note">
          ProxyLens 的采集器是独立后台进程；关闭本窗口不会影响它继续采集。
        </p>
      </Panel>

      <Panel title="读取口径" hint="本页数据的来源与边界">
        <ul className="pl-note-list">
          <li>
            全部数据来自本地 Go 查询服务的 <code>/api/v1/coverage</code> 与 <code>/api/v1/meta</code>，
            前端不做任何覆盖率推算，也不复用其他端点反推。
          </li>
          <li>
            缺口区间由 <code>monitoring_gaps</code> 与采集会话状态合并推导，重叠区间会被合并为一条。
          </li>
          <li>
            覆盖率只描述<strong>时间</strong>维度，不代表字节覆盖达到同样比例；
            具体流量规模请回到「总览」查看。
          </li>
        </ul>
      </Panel>
    </div>
  );
};
