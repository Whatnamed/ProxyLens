import React from 'react';
import { QuickWindowType } from '../../utils/time';
import { RouteFocus, useAuditContext, useLocale } from '../../state/AuditContext';
import { DateTimePicker, Segmented } from '../ui/primitives';
import { IconRefresh, IconSnapshot } from '../ui/icons';
import { formatLocalDateTime } from '../../utils/time';
import { parseLocalDateTimeInput } from '../../utils/localDateTime';

function toLocalInputValue(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export const TimeRangeControl: React.FC = () => {
  const { t } = useLocale();
  const { timeRange, setQuickWindow, setCustomRange, customEditorOpen, openCustomEditor } = useAuditContext();
  const [draftFrom, setDraftFrom] = React.useState('');
  const [draftTo, setDraftTo] = React.useState('');
  const windowOptions = [
    { value: 'today' as const, label: t('timeRange.today') },
    { value: 'yesterday' as const, label: t('timeRange.yesterday') },
    { value: '7d' as const, label: t('timeRange.sevenDays') },
    { value: '30d' as const, label: t('timeRange.thirtyDays') },
    { value: 'custom' as const, label: t('timeRange.custom') },
  ];

  const editorVisible = customEditorOpen || timeRange.kind === 'custom';
  const parsedFrom = parseLocalDateTimeInput(draftFrom);
  const parsedTo = parseLocalDateTimeInput(draftTo);
  const invalidRange = !!draftFrom && !!draftTo && (!parsedFrom || !parsedTo || parsedFrom.getTime() >= parsedTo.getTime());

  React.useEffect(() => {
    if (editorVisible) {
      setDraftFrom(toLocalInputValue(timeRange.customFrom));
      setDraftTo(toLocalInputValue(timeRange.customTo));
    }
    // Seed drafts only when the editor opens, not on every keystroke.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editorVisible]);

  return (
    <div className="pl-context-bar__group">
      <Segmented
        ariaLabel={t('timeRange.aria')}
        options={windowOptions}
        value={editorVisible ? 'custom' : timeRange.kind}
        onChange={(v) => {
          if (v === 'custom') {
            openCustomEditor();
          } else {
            setQuickWindow(v as QuickWindowType);
          }
        }}
      />
      {editorVisible && (
        <>
          <DateTimePicker
            value={draftFrom}
            onChange={setDraftFrom}
            label={t('timeRange.start')}
            defaultTime="00:00"
          />
          <span className="pl-muted pl-small">{t('common.to')}</span>
          <DateTimePicker
            value={draftTo}
            onChange={setDraftTo}
            label={t('timeRange.end')}
            defaultTime="23:59"
          />
          {invalidRange && <span className="pl-date-range__error" role="alert">{t('timeRange.invalid')}</span>}
          <button
            className="pl-btn pl-btn--compact"
            disabled={!draftFrom || !draftTo || invalidRange}
            onClick={() => setCustomRange(draftFrom, draftTo)}
          >
            {t('common.apply')}
          </button>
        </>
      )}
    </div>
  );
};

export const RouteControl: React.FC = () => {
  const { t } = useLocale();
  const { routeFocus, setRouteFocus } = useAuditContext();
  const routeOptions: { value: RouteFocus; label: string; dotColor?: string; title: string }[] = [
    { value: 'PROXY', label: t('route.proxy'), dotColor: 'var(--pl-route-proxy)', title: t('route.proxyTitle') },
    { value: 'DIRECT', label: t('route.direct'), dotColor: 'var(--pl-route-direct)', title: t('route.directTitle') },
    { value: 'REJECT', label: t('route.reject'), dotColor: 'var(--pl-route-reject)', title: t('route.rejectTitle') },
    { value: 'ALL', label: t('route.all'), title: t('route.allTitle') },
  ];
  return (
    <Segmented
      ariaLabel={t('route.aria')}
      options={routeOptions}
      value={routeFocus}
      onChange={setRouteFocus}
    />
  );
};

export const HistorySnapshotControl: React.FC = () => {
  const { locale, t } = useLocale();
  const { snapshot, refreshHistory } = useAuditContext();
  if (!snapshot) return null;
  return (
    <div className="pl-context-bar__right">
      <span className="pl-snapshot-chip" title={t('snapshot.title')}>
        <IconSnapshot />
        {t('snapshot.label', { time: formatLocalDateTime(snapshot.to, locale) })}
      </span>
      <button className="pl-btn pl-btn--quiet pl-btn--compact" onClick={refreshHistory} title={t('snapshot.refreshTitle')}>
        <IconRefresh />
        {t('common.refresh')}
      </button>
    </div>
  );
};
