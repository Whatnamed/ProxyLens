import React, { useEffect, useRef, useState } from 'react';
import { useAppFilters, WindowKind } from '../state/AppFilters';
import { ROUTE_SCOPES, type RouteScope } from '../domain/route';
import { QUICK_WINDOW_LABELS, QuickWindowType, toLocalInputValue } from '../utils/time';
import { TrustDot } from '../components/DataViz';
import type { TrustLevel } from '../domain/integrity';

/**
 * The command bar holds the two filters that change the meaning of every number on screen:
 * the route scope and the time window. They live in the chrome rather than inside a surface
 * because they are global lenses — moving between surfaces must not silently drop them.
 *
 * Route scope defaults to PROXY (PRODUCT 3.2): the product exists to explain real proxy
 * traffic, so that is the default view and DIRECT is a deliberate switch, not a peer.
 */

const QUICK_WINDOWS: QuickWindowType[] = ['today', 'yesterday', '7d', '30d'];

export const TopBar: React.FC<{
  trust: { level: TrustLevel; headline: string; detail: string } | null;
  isFetching: boolean;
  connectionState: 'connecting' | 'ready' | 'failed';
}> = ({ trust, isFetching, connectionState }) => {
  const {
    windowKind,
    setWindowKind,
    windowLabel,
    customFrom,
    customTo,
    setCustomRange,
    customRangeValid,
    routeScope,
    setRouteScope,
    liveMode,
    setLiveMode,
    refresh,
  } = useAppFilters();

  const [open, setOpen] = useState(false);
  const popRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (popRef.current && !popRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  return (
    <header className="pl-topbar">
      <div className="pl-topbar__group">
        <div
          className="pl-seg"
          role="group"
          aria-label="出站方式范围"
          title="决定所有统计口径：仅代理 / 仅直连 / 仅拦截 / 全部"
        >
          {ROUTE_SCOPES.map((s) => (
            <button
              key={s.value}
              type="button"
              className={`pl-seg__btn ${routeScope === s.value ? 'is-active' : ''}`}
              onClick={() => setRouteScope(s.value as RouteScope)}
              title={s.hint}
            >
              {s.label}
            </button>
          ))}
        </div>
      </div>

      <div className="pl-topbar__spacer" />

      <div className="pl-topbar__group">
        {trust && (
          <div
            className={`pl-trust pl-trust--${trust.level}`}
            title={`${trust.headline}：${trust.detail}`}
          >
            <TrustDot level={trust.level} />
            <span className="pl-trust__text">{trust.headline}</span>
          </div>
        )}

        <div className="pl-window" ref={popRef}>
          <button
            type="button"
            className={`pl-window__btn ${open ? 'is-open' : ''} ${!customRangeValid ? 'is-invalid' : ''}`}
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open}
          >
            <span className="pl-window__icon" aria-hidden="true">
              <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.6">
                <rect x="3" y="4.5" width="14" height="12" rx="1.5" />
                <path d="M3 8h14M7 3v3M13 3v3" strokeLinecap="round" />
              </svg>
            </span>
            {windowLabel}
          </button>

          {open && (
            <div className="pl-window__pop">
              <div className="pl-window__pop-title">快捷窗口（按本地自然日）</div>
              <div className="pl-window__quick">
                {QUICK_WINDOWS.map((w) => (
                  <button
                    key={w}
                    type="button"
                    className={`pl-window__quick-btn ${windowKind === w ? 'is-active' : ''}`}
                    onClick={() => {
                      setWindowKind(w as WindowKind);
                      setOpen(false);
                    }}
                  >
                    {QUICK_WINDOW_LABELS[w]}
                  </button>
                ))}
                <button
                  type="button"
                  className={`pl-window__quick-btn ${windowKind === 'custom' ? 'is-active' : ''}`}
                  onClick={() => setWindowKind('custom')}
                >
                  自定义
                </button>
              </div>

              {windowKind === 'custom' && (
                <div className="pl-window__custom">
                  <label>
                    起
                    <input
                      type="datetime-local"
                      value={customFrom}
                      onChange={(e) => setCustomRange(e.target.value, customTo)}
                    />
                  </label>
                  <label>
                    止
                    <input
                      type="datetime-local"
                      value={customTo}
                      onChange={(e) => setCustomRange(customFrom, e.target.value)}
                    />
                  </label>
                  <div className="pl-window__hint">
                    {customRangeValid ? '已应用自定义区间' : '需要完整的起止时间，且起点必须早于终点'}
                  </div>
                  {(customFrom || customTo) === '' && (
                    <button
                      type="button"
                      className="pl-btn pl-btn--ghost pl-btn--xs"
                      onClick={() => {
                        const now = new Date();
                        const dayAgo = new Date(now.getTime() - 86_400_000);
                        setCustomRange(toLocalInputValue(dayAgo.toISOString()), toLocalInputValue(now.toISOString()));
                      }}
                    >
                      填充最近 24 小时
                    </button>
                  )}
                </div>
              )}
            </div>
          )}
        </div>

        <button
          type="button"
          className={`pl-icon-btn ${liveMode ? 'is-on' : ''}`}
          onClick={() => setLiveMode(!liveMode)}
          title={liveMode ? '自动刷新已开启（每 15 秒）' : '自动刷新已关闭'}
          aria-pressed={liveMode}
        >
          <span className="pl-icon-btn__dot" />
          自动
        </button>

        <button
          type="button"
          className={`pl-icon-btn ${isFetching ? 'is-busy' : ''}`}
          onClick={refresh}
          title="立即刷新"
        >
          <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round">
            <path d="M16 10a6 6 0 1 1-1.8-4.3" />
            <path d="M16 3.5V7h-3.5" />
          </svg>
          刷新
        </button>

        <div
          className={`pl-conn pl-conn--${connectionState}`}
          title={
            connectionState === 'ready'
              ? '本地查询服务已连接'
              : connectionState === 'connecting'
                ? '正在初始化本地查询服务'
                : '本地查询服务不可用'
          }
        >
          <span className="pl-conn__dot" />
        </div>
      </div>
    </header>
  );
};
