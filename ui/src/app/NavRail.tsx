import React from 'react';
import type { SurfaceId } from '../state/AppFilters';

/**
 * The navigation is ordered by the questions a user actually asks, not by endpoint:
 *
 *   总览    how much went through the proxy, and can I trust the number
 *   归因    who / where did it go
 *   规则    why was it routed that way  (the surface that leads to a config change)
 *   连接    the underlying evidence, one record at a time
 *   监控    where we were blind
 *   系统    what this installation is and what state it is in
 *
 * "规则" sits above "连接" because the product's end goal is rule optimisation;
 * the connection ledger is the evidence you fall back to when a rule row looks wrong.
 */

interface NavItem {
  id: SurfaceId;
  label: string;
  hint: string;
  icon: React.ReactNode;
}

const strokeProps = {
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.6,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
};

const NAV_ITEMS: NavItem[] = [
  {
    id: 'overview',
    label: '总览',
    hint: '代理流量规模与数据完整性',
    icon: (
      <svg viewBox="0 0 20 20" {...strokeProps}>
        <path d="M3 14.5V9m4.5 5.5V4m4.5 10.5V7.5M16.5 14.5v-3" />
      </svg>
    ),
  },
  {
    id: 'attribution',
    label: '归因',
    hint: '按进程 / 目标 / 节点 / 协议拆解',
    icon: (
      <svg viewBox="0 0 20 20" {...strokeProps}>
        <rect x="3" y="3.5" width="6" height="6" rx="1.2" />
        <rect x="11" y="3.5" width="6" height="3" rx="1" />
        <rect x="11" y="8.5" width="6" height="8" rx="1.2" />
        <rect x="3" y="11.5" width="6" height="5" rx="1.2" />
      </svg>
    ),
  },
  {
    id: 'rules',
    label: '规则',
    hint: '命中规则排行与分流缺口',
    icon: (
      <svg viewBox="0 0 20 20" {...strokeProps}>
        <path d="M4 5.5h12M4 10h8M4 14.5h5" />
        <circle cx="15" cy="10" r="1.6" />
        <circle cx="11.5" cy="14.5" r="1.4" />
      </svg>
    ),
  },
  {
    id: 'history',
    label: '连接',
    hint: '历史连接明细与单条审计下钻',
    icon: (
      <svg viewBox="0 0 20 20" {...strokeProps}>
        <path d="M3.5 5.5h13M3.5 10h13M3.5 14.5h13" />
        <circle cx="6.5" cy="5.5" r="1.3" />
        <circle cx="12" cy="10" r="1.3" />
        <circle cx="8" cy="14.5" r="1.3" />
      </svg>
    ),
  },
  {
    id: 'coverage',
    label: '监控',
    hint: '监控覆盖率与缺口时间轴',
    icon: (
      <svg viewBox="0 0 20 20" {...strokeProps}>
        <rect x="3" y="4" width="14" height="12" rx="1.4" />
        <path d="M7.5 4v12M12.5 4v12" strokeDasharray="2 2" />
      </svg>
    ),
  },
  {
    id: 'system',
    label: '系统',
    hint: '数据库、会话与核算状态',
    icon: (
      <svg viewBox="0 0 20 20" {...strokeProps}>
        <circle cx="10" cy="10" r="6.2" />
        <path d="M10 6.6v3.6l2.4 1.6" />
      </svg>
    ),
  },
];

export const NavRail: React.FC<{
  active: SurfaceId;
  onSelect: (s: SurfaceId) => void;
}> = ({ active, onSelect }) => (
  <nav className="pl-rail" aria-label="主导航">
    <div className="pl-rail__brand">
      <div className="pl-rail__mark" aria-hidden="true">
        <svg viewBox="0 0 24 24" {...strokeProps} strokeWidth={1.8}>
          <circle cx="12" cy="12" r="8.5" />
          <circle cx="12" cy="12" r="3.2" />
          <path d="M12 3.5v3M12 17.5v3M3.5 12h3M17.5 12h3" />
        </svg>
      </div>
      <div className="pl-rail__brand-text">
        <span className="pl-rail__brand-name">ProxyLens</span>
        <span className="pl-rail__brand-sub">代理流量审计</span>
      </div>
    </div>

    <ul className="pl-rail__list">
      {NAV_ITEMS.map((item) => (
        <li key={item.id}>
          <button
            type="button"
            className={`pl-rail__item ${active === item.id ? 'is-active' : ''}`}
            onClick={() => onSelect(item.id)}
            aria-current={active === item.id ? 'page' : undefined}
            title={item.hint}
          >
            <span className="pl-rail__icon">{item.icon}</span>
            <span className="pl-rail__label">{item.label}</span>
          </button>
        </li>
      ))}
    </ul>

    <div className="pl-rail__foot">
      <div className="pl-rail__note">
        只读旁路观测
        <br />
        不介入代理数据路径
      </div>
    </div>
  </nav>
);
