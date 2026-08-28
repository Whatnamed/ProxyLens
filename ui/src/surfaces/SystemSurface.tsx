import React from 'react';
import { QueryApiClient } from '../api/client';
import { useMetaQuery } from '../api/queries';
import { Panel, Field } from '../components/Panel';
import { Chip, TrustDot } from '../components/DataViz';
import { QueryState } from '../components/StateBlock';
import { freshnessState } from '../domain/integrity';
import { formatCount, formatDuration } from '../utils/format';
import { formatLocalDateTime } from '../utils/time';

/**
 * System state.
 *
 * Deliberately the last surface and deliberately plain: it exists so a user can answer
 * "what am I actually looking at" (which database, which schema, which accounting run)
 * without opening a log file. It is not a settings page — ProxyLens is read-only, so there
 * is nothing here to configure beyond the time window and route scope in the command bar.
 */

const DB_STATE_VIEW: Record<string, { label: string; tone: 'ok' | 'warn' | 'danger' | 'neutral' }> = {
  READY: { label: '就绪', tone: 'ok' },
  UNAVAILABLE: { label: '不可用', tone: 'danger' },
  UNINITIALIZED: { label: '未初始化', tone: 'warn' },
  INCOMPATIBLE: { label: '版本不兼容', tone: 'danger' },
};

const RUN_STATUS_VIEW: Record<string, { label: string; tone: 'ok' | 'warn' | 'danger' | 'neutral' }> = {
  completed: { label: '已完成', tone: 'ok' },
  running: { label: '进行中', tone: 'warn' },
  failed: { label: '失败', tone: 'danger' },
};

export const SystemSurface: React.FC<{
  client: QueryApiClient | null;
  isTauri: boolean;
  sessionError: string | null;
}> = ({ client, isTauri, sessionError }) => {
  const metaQuery = useMetaQuery(client, 10_000);
  const meta = metaQuery.data;
  const freshness = meta?.freshness;
  const fresh = freshnessState(freshness);
  const run = meta?.latestAccountingRun;
  const session = meta?.latestCollectorSession;

  const dbView = DB_STATE_VIEW[meta?.dbState ?? ''] ?? {
    label: meta?.dbState ?? '未知',
    tone: 'neutral' as const,
  };
  const runView = RUN_STATUS_VIEW[run?.status ?? ''] ?? {
    label: run?.status ?? '未知',
    tone: 'neutral' as const,
  };

  return (
    <div className="pl-surface">
      {sessionError && (
        <Panel title="会话初始化失败" hint="本地查询服务未能建立会话">
          <div className="pl-state pl-state--error is-compact">
            <div className="pl-state__marker" aria-hidden="true" />
            <div className="pl-state__content">
              <div className="pl-state__title">无法初始化查询会话</div>
              <div className="pl-state__detail">{sessionError}</div>
            </div>
          </div>
        </Panel>
      )}

      <Panel title="运行环境" hint="ProxyLens 在本机上的运行形态">
        <dl className="pl-fields pl-fields--grid">
          <Field label="桌面外壳">
            {isTauri ? 'Tauri 原生窗口' : '浏览器 / 开发模式'}
          </Field>
          <Field label="数据访问" title="前端只能通过本地 HTTP 查询服务访问数据">
            Go 本地查询服务（只读）
          </Field>
          <Field label="应用版本">{meta?.appVersion ?? '—'}</Field>
          <Field label="API 版本">{meta?.apiVersion ?? '—'}</Field>
        </dl>
        <p className="pl-panel__note">
          React 不直接读取 SQLite；Rust 外壳只负责启动与停止查询服务，不实现任何核算逻辑。
        </p>
      </Panel>

      <Panel title="数据库" hint="审计库的位置状态与 schema 版本" flush>
        <QueryState
          isLoading={metaQuery.isLoading}
          isError={metaQuery.isError}
          error={metaQuery.error}
          isEmpty={!meta}
          emptyTitle="无法读取系统元数据"
          loadingRows={4}
        >
          <dl className="pl-fields pl-fields--grid">
            <Field label="数据库状态">
              <Chip tone={dbView.tone}>{dbView.label}</Chip>
            </Field>
            <Field label="Schema 版本" title="库中 schema 版本 / 当前二进制支持的最高版本">
              {meta?.schemaVersion ?? '—'} / {meta?.maxBinarySchemaVersion ?? '—'}
            </Field>
          </dl>
          {meta && meta.schemaVersion !== meta.maxBinarySchemaVersion && (
            <p className="pl-panel__note">
              数据库 schema 与当前程序支持的最高版本不一致，查询服务会拒绝打开数据库以避免语义错误。
            </p>
          )}
        </QueryState>
      </Panel>

      <Panel
        title="核算轮次"
        hint="当前展示的数据来自哪一次核算"
        flush
        actions={
          <div className={`pl-trust pl-trust--${fresh.level}`}>
            <TrustDot level={fresh.level} />
            <span className="pl-trust__text">{fresh.label}</span>
          </div>
        }
      >
        <QueryState
          isLoading={metaQuery.isLoading}
          isError={metaQuery.isError}
          error={metaQuery.error}
          isEmpty={!run}
          emptyTitle="没有核算轮次"
          emptyDetail="数据库里还不存在任何核算轮次记录。"
          loadingRows={5}
        >
          {run && (
            <dl className="pl-fields pl-fields--grid">
              <Field label="Run ID">{run.runId}</Field>
              <Field label="状态">
                <Chip tone={runView.tone}>{runView.label}</Chip>
              </Field>
              <Field label="算法版本">{run.algorithmVersion}</Field>
              <Field label="源事件数">{formatCount(run.sourceJournalEventCount)}</Field>
              <Field label="开始时间">{formatLocalDateTime(run.startedAt)}</Field>
              <Field label="完成时间">
                {run.completedAt ? formatLocalDateTime(run.completedAt) : '—'}
              </Field>
              {run.failedReason && <Field label="失败原因">{run.failedReason}</Field>}
            </dl>
          )}
        </QueryState>
      </Panel>

      <Panel title="时效性" hint="核算结果与原始事件日志之间的距离" flush>
        <QueryState
          isLoading={metaQuery.isLoading}
          isError={metaQuery.isError}
          error={metaQuery.error}
          isEmpty={!freshness}
          emptyTitle="无法读取时效信息"
          loadingRows={3}
        >
          {freshness && (
            <dl className="pl-fields pl-fields--grid">
              <Field label="滞后事件数" title="尚未进入核算的事件数量">
                {formatCount(freshness.lagEvents)}
              </Field>
              <Field label="是否同步">
                <Chip tone={freshness.isFresh ? 'ok' : 'warn'}>
                  {freshness.isFresh ? '已同步' : '存在滞后'}
                </Chip>
              </Field>
              <Field label="核算游标" title="本次核算覆盖到的最大 journal sequence">
                {formatCount(freshness.sourceJournalSequenceMax)}
              </Field>
              <Field label="日志游标" title="事件日志当前的最大 journal sequence">
                {formatCount(freshness.currentJournalSequenceMax)}
              </Field>
              <Field label="完成时间">
                {freshness.completedAt ? formatLocalDateTime(freshness.completedAt) : '—'}
              </Field>
            </dl>
          )}
        </QueryState>
      </Panel>

      <Panel title="采集进程" hint="后台常驻采集器的会话状态" flush>
        <QueryState
          isLoading={metaQuery.isLoading}
          isError={metaQuery.isError}
          error={metaQuery.error}
          isEmpty={!session}
          emptyTitle="没有采集会话记录"
          loadingRows={4}
        >
          {session && (
            <dl className="pl-fields pl-fields--grid">
              <Field label="会话 ID">{session.sessionId}</Field>
              <Field label="状态">
                <Chip
                  tone={
                    session.status === 'running'
                      ? 'ok'
                      : session.status === 'closed_clean'
                        ? 'neutral'
                        : 'danger'
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
              <Field label="心跳间隔">
                {session.heartbeatIntervalMs ? formatDuration(session.heartbeatIntervalMs) : '—'}
              </Field>
              <Field label="版本">{session.collectorVersion || '—'}</Field>
            </dl>
          )}
        </QueryState>
      </Panel>

      <Panel title="产品边界" hint="ProxyLens 不会做什么">
        <ul className="pl-note-list">
          <li>旁路只读：不修改 Mihomo 配置、规则、节点、TUN、系统代理或路由表。</li>
          <li>不阻断、不限速、不做 TLS 解密，不保存请求正文、Cookie、Token 等隐私内容。</li>
          <li>全部历史仅保存在本机，不做任何云端上传。</li>
          <li>采集器独立于本窗口运行；关闭 ProxyLens 窗口不会中断后台采集。</li>
        </ul>
      </Panel>
    </div>
  );
};
