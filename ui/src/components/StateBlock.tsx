import React from 'react';
import { ApiClientError } from '../api/client';
import { mapApiErrorCode } from '../utils/error';

/**
 * ProxyLens has more failure modes than a usual web app, and several of them are
 * EXPECTED states rather than errors:
 *
 *   - NO_COMPLETED_ACCOUNTING_RUN : a fresh install that has not reconciled yet
 *   - DB_UNAVAILABLE              : no database configured for this session
 *   - API_UNAVAILABLE             : the query sidecar is not reachable
 *   - stale accounting            : numbers lag the event journal
 *
 * Collapsing all of these into one red "something went wrong" box would hide the
 * difference between "nothing has been collected" and "the tool is broken", which is
 * exactly the distinction this product is built around.
 */

export type StateTone = 'loading' | 'empty' | 'unavailable' | 'error' | 'stale';

export interface DescribedError {
  tone: 'unavailable' | 'error';
  title: string;
  detail: string;
  code?: string;
}

export function describeError(error: unknown): DescribedError {
  if (error instanceof ApiClientError) {
    switch (error.code) {
      case 'NO_COMPLETED_ACCOUNTING_RUN':
        return {
          tone: 'unavailable',
          title: '尚无核算结果',
          detail:
            '数据库里还没有任何一次成功完成的核算轮次。请确认后台采集器已运行并写入数据；核算完成后本页会自动填充。',
          code: error.code,
        };
      case 'DB_UNAVAILABLE':
        return {
          tone: 'unavailable',
          title: '数据库不可用',
          detail: '本地查询服务无法打开审计数据库。请检查数据库路径配置与文件访问权限。',
          code: error.code,
        };
      case 'UNAUTHORIZED':
        return {
          tone: 'error',
          title: '会话鉴权失败',
          detail: '本地查询服务拒绝了本次会话令牌。请重新启动 ProxyLens。',
          code: error.code,
        };
      case 'API_UNAVAILABLE':
        return {
          tone: 'unavailable',
          title: '查询服务未就绪',
          detail: '无法连接到本地 Go 查询服务。若刚启动，请稍候片刻后重试。',
          code: error.code,
        };
      case 'FORBIDDEN_ORIGIN':
        return {
          tone: 'error',
          title: '请求来源被拒绝',
          detail: '本地查询服务的 CORS 白名单不包含当前来源。',
          code: error.code,
        };
      default:
        return {
          tone: 'error',
          title: mapApiErrorCode(error.code),
          detail: error.message,
          code: error.code,
        };
    }
  }

  const message = error instanceof Error ? error.message : String(error);
  return { tone: 'error', title: '请求失败', detail: message };
}

export const StateBlock: React.FC<{
  tone: StateTone;
  title: string;
  detail?: React.ReactNode;
  code?: string;
  action?: React.ReactNode;
  compact?: boolean;
}> = ({ tone, title, detail, code, action, compact = false }) => (
  <div className={`pl-state pl-state--${tone} ${compact ? 'is-compact' : ''}`} role="status">
    <div className="pl-state__marker" aria-hidden="true" />
    <div className="pl-state__content">
      <div className="pl-state__title">{title}</div>
      {detail && <div className="pl-state__detail">{detail}</div>}
      <div className="pl-state__foot">
        {code && <code className="pl-state__code">{code}</code>}
        {action}
      </div>
    </div>
  </div>
);

/** Skeleton reserved-space loader: keeps layout stable instead of a spinner that jumps. */
export const LoadingBlock: React.FC<{ rows?: number; label?: string }> = ({ rows = 4, label }) => (
  <div className="pl-loading" role="status" aria-busy="true">
    {label && <div className="pl-loading__label">{label}</div>}
    {Array.from({ length: rows }).map((_, i) => (
      <div className="pl-loading__row" key={i} style={{ width: `${100 - i * 7}%` }} />
    ))}
  </div>
);

/**
 * Renders the correct state for one query result.
 * `emptyWhen` lets a caller declare "loaded, but nothing to show" without duplicating
 * the empty copy in every surface.
 */
export const QueryState: React.FC<{
  isLoading: boolean;
  isError: boolean;
  error?: unknown;
  isEmpty?: boolean;
  emptyTitle?: string;
  emptyDetail?: React.ReactNode;
  loadingRows?: number;
  children: React.ReactNode;
}> = ({ isLoading, isError, error, isEmpty, emptyTitle, emptyDetail, loadingRows, children }) => {
  if (isLoading) return <LoadingBlock rows={loadingRows} />;
  if (isError) {
    const d = describeError(error);
    return <StateBlock tone={d.tone} title={d.title} detail={d.detail} code={d.code} />;
  }
  if (isEmpty) {
    return <StateBlock tone="empty" title={emptyTitle ?? '窗口内无数据'} detail={emptyDetail} />;
  }
  return <>{children}</>;
};
