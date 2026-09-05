import React, { useEffect, useRef, useState } from 'react';
import { QueryApiClient } from '../../api/client';
import { useAuditContext, useLocale, ViewName } from '../../state/AuditContext';
import { useMetaQuery } from '../../api/queries';
import { SystemStatusFooter } from '../audit/SystemStatus';
import { OverviewPage } from '../../features/overview/OverviewPage';
import { HistoryPage } from '../../features/history/HistoryPage';
import { CoveragePage } from '../../features/coverage/CoveragePage';
import { DiagnosticsView } from '../../diagnostics/DiagnosticsView';
import { IconCoverage, IconHistory, IconLens, IconMoon, IconOverview, IconSun } from '../ui/icons';
import { SettingsDialog, SettingsTriggerIcon as SettingsDialogIcon } from '../settings/SettingsDialog';

const NAV_ITEMS: { view: ViewName; labelKey: string; icon: React.ReactNode }[] = [
  { view: 'overview', labelKey: 'nav.overview', icon: <IconOverview /> },
  { view: 'history', labelKey: 'nav.history', icon: <IconHistory /> },
  { view: 'coverage', labelKey: 'nav.coverage', icon: <IconCoverage /> },
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
  const { view, setView, theme, toggleTheme, locale, setLocale } = useAuditContext();
  const { t } = useLocale();
  const metaQuery = useMetaQuery(client);
  const showDiagnostics = useHashDiagnostics();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const settingsTriggerRef = useRef<HTMLButtonElement | null>(null);

  const closeSettings = () => {
    setSettingsOpen(false);
    window.requestAnimationFrame(() => settingsTriggerRef.current?.focus());
  };

  if (showDiagnostics) {
    return (
      <div style={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
        <div style={{ padding: '8px 16px', borderBottom: '1px solid var(--pl-border-muted)', display: 'flex', alignItems: 'center', gap: 12 }}>
          <span className="pl-eyebrow">{t('diagnostics.title')}</span>
          <a href="#/" style={{ fontSize: 12 }}>{t('diagnostics.return')}</a>
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
          <span className="pl-sidebar__version">{t('brand.version')}</span>
        </div>
        <nav className="pl-nav" aria-label={t('nav.primary')}>
          {NAV_ITEMS.map((item) => (
            <button
              key={item.view}
              className={`pl-nav__item${view === item.view ? ' pl-nav__item--active' : ''}`}
              aria-current={view === item.view ? 'page' : undefined}
              onClick={() => setView(item.view)}
            >
              {item.icon}
              {t(item.labelKey)}
            </button>
          ))}
        </nav>
        <div className="pl-sidebar__footer">
          <SystemStatusFooter meta={metaQuery.data} />
          <div className="pl-sidebar__utility">
            <button
              ref={settingsTriggerRef}
              type="button"
              className="pl-sidebar__theme-toggle"
              onClick={() => setSettingsOpen(true)}
              disabled={!isTauri}
              title={isTauri ? t('settings.open') : t('settings.desktopOnly')}
              aria-label={isTauri ? t('settings.open') : t('settings.desktopOnly')}
              aria-haspopup="dialog"
              aria-expanded={settingsOpen}
            >
              <SettingsDialogIcon />
              {t('settings.open')}
            </button>
            <button
              className="pl-sidebar__theme-toggle"
              onClick={toggleTheme}
              title={theme === 'light' ? t('theme.switchToDark') : t('theme.switchToLight')}
              aria-label={theme === 'light' ? t('theme.switchToDark') : t('theme.switchToLight')}
            >
              {theme === 'light' ? <IconMoon /> : <IconSun />}
              {theme === 'light' ? t('theme.dark') : t('theme.light')}
            </button>
            <div className="pl-sidebar__locale-row">
              <span className="pl-sidebar__locale-label">{t('locale.label')}</span>
              <div className="pl-locale-control" role="group" aria-label={t('locale.switch')}>
                <button
                  type="button"
                  className={`pl-locale-control__item${locale === 'en' ? ' pl-locale-control__item--active' : ''}`}
                  aria-pressed={locale === 'en'}
                  title={t('locale.english')}
                  onClick={() => setLocale('en')}
                >
                  {t('locale.shortEnglish')}
                </button>
                <button
                  type="button"
                  className={`pl-locale-control__item${locale === 'zh-CN' ? ' pl-locale-control__item--active' : ''}`}
                  aria-pressed={locale === 'zh-CN'}
                  title={t('locale.chinese')}
                  onClick={() => setLocale('zh-CN')}
                >
                  {t('locale.shortChinese')}
                </button>
              </div>
            </div>
          </div>
        </div>
      </aside>
      {settingsOpen && isTauri && <SettingsDialog onClose={closeSettings} />}
      <main className="pl-workspace">
        {view === 'overview' && <OverviewPage client={client} sessionError={sessionError} meta={metaQuery.data} />}
        {view === 'history' && <HistoryPage client={client} sessionError={sessionError} meta={metaQuery.data} />}
        {view === 'coverage' && <CoveragePage client={client} sessionError={sessionError} meta={metaQuery.data} />}
      </main>
    </div>
  );
};
