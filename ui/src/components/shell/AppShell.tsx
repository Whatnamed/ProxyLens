import React, { useEffect, useState } from 'react';
import { QueryApiClient } from '../../api/client';
import { useAuditContext, ViewName } from '../../state/AuditContext';
import { useMetaQuery } from '../../api/queries';
import { SystemStatusFooter } from '../audit/SystemStatus';
import { OverviewPage } from '../../features/overview/OverviewPage';
import { HistoryPage } from '../../features/history/HistoryPage';
import { CoveragePage } from '../../features/coverage/CoveragePage';
import { DiagnosticsView } from '../../diagnostics/DiagnosticsView';
import { IconCoverage, IconHistory, IconLens, IconMoon, IconOverview, IconSun } from '../ui/icons';

const NAV_ITEMS: { view: ViewName; label: string; icon: React.ReactNode }[] = [
  { view: 'overview', label: 'Overview', icon: <IconOverview /> },
  { view: 'history', label: 'History', icon: <IconHistory /> },
  { view: 'coverage', label: 'Coverage', icon: <IconCoverage /> },
];

function useHashDiagnostics(): boolean {
  const [active, setActive] = useState(() => window.location.hash === '#/diagnostics');
  useEffect(() => {
    const onChange = () => setActive(window.location.hash === '#/diagnostics');
    window.addEventListener('hashchange', onChange);
    return () => window.removeEventListener('hashchange', onChange);
  }, []);
  return active;
}

export const AppShell: React.FC<{
  client: QueryApiClient | null;
  isTauri: boolean;
  sessionError: string | null;
}> = ({ client, isTauri, sessionError }) => {
  const { view, setView, theme, toggleTheme } = useAuditContext();
  const metaQuery = useMetaQuery(client);
  const showDiagnostics = useHashDiagnostics();

  if (showDiagnostics) {
    return (
      <div style={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
        <div style={{ padding: '8px 16px', borderBottom: '1px solid var(--pl-border-muted)', display: 'flex', alignItems: 'center', gap: 12 }}>
          <span className="pl-eyebrow">Development diagnostics</span>
          <a href="#/" style={{ fontSize: 12 }}>Return to product UI</a>
        </div>
        <div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
          <DiagnosticsView client={client} isTauri={isTauri} sessionError={sessionError} />
        </div>
      </div>
    );
  }

  return (
    <div className="pl-app">
      <aside className="pl-sidebar">
        <div className="pl-sidebar__brand">
          <span className="pl-sidebar__mark"><IconLens /></span>
          <span className="pl-sidebar__name">ProxyLens</span>
          <span className="pl-sidebar__version">v1 audit</span>
        </div>
        <nav className="pl-nav" aria-label="Primary">
          {NAV_ITEMS.map((item) => (
            <button
              key={item.view}
              className={`pl-nav__item${view === item.view ? ' pl-nav__item--active' : ''}`}
              aria-current={view === item.view ? 'page' : undefined}
              onClick={() => setView(item.view)}
            >
              {item.icon}
              {item.label}
            </button>
          ))}
        </nav>
        <div className="pl-sidebar__footer">
          <SystemStatusFooter meta={metaQuery.data} />
          <button className="pl-sidebar__theme-toggle" onClick={toggleTheme} title="Switch Light/Dark theme">
            {theme === 'light' ? <IconMoon /> : <IconSun />}
            {theme === 'light' ? 'Dark theme' : 'Light theme'}
          </button>
        </div>
      </aside>
      <main className="pl-workspace">
        {view === 'overview' && <OverviewPage client={client} sessionError={sessionError} meta={metaQuery.data} />}
        {view === 'history' && <HistoryPage client={client} sessionError={sessionError} meta={metaQuery.data} />}
        {view === 'coverage' && <CoveragePage client={client} sessionError={sessionError} meta={metaQuery.data} />}
      </main>
    </div>
  );
};
