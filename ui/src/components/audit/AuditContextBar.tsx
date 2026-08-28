import React from 'react';
import { QuickWindowType } from '../../utils/time';
import { RouteFocus, useAuditContext } from '../../state/AuditContext';
import { Segmented } from '../ui/primitives';
import { IconRefresh, IconSnapshot } from '../ui/icons';
import { formatLocalDateTime } from '../../utils/time';

const WINDOW_OPTIONS: { value: QuickWindowType | 'custom'; label: string }[] = [
  { value: 'today', label: 'Today' },
  { value: 'yesterday', label: 'Yesterday' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
  { value: 'custom', label: 'Custom' },
];

const ROUTE_OPTIONS: { value: RouteFocus; label: string; dotColor?: string; title: string }[] = [
  { value: 'PROXY', label: 'Proxy', dotColor: 'var(--pl-route-proxy)', title: 'Focus on traffic routed through proxy nodes' },
  { value: 'DIRECT', label: 'Direct', dotColor: 'var(--pl-route-direct)', title: 'Focus on direct (non-proxied) traffic' },
  { value: 'REJECT', label: 'Reject', dotColor: 'var(--pl-route-reject)', title: 'Focus on rejected traffic' },
  { value: 'ALL', label: 'All', title: 'All routing outcomes' },
];

function toLocalInputValue(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export const TimeRangeControl: React.FC = () => {
  const { timeRange, setQuickWindow, setCustomRange } = useAuditContext();
  const [draftFrom, setDraftFrom] = React.useState('');
  const [draftTo, setDraftTo] = React.useState('');

  return (
    <div className="pl-context-bar__group">
      <Segmented
        ariaLabel="Time range"
        options={WINDOW_OPTIONS.map((o) => ({ value: o.value, label: o.label }))}
        value={timeRange.kind}
        onChange={(v) => {
          if (v === 'custom') {
            setDraftFrom(toLocalInputValue(timeRange.customFrom));
            setDraftTo(toLocalInputValue(timeRange.customTo));
            if (timeRange.kind !== 'custom') {
              setCustomRange('', '');
            }
          } else {
            setQuickWindow(v as QuickWindowType);
          }
        }}
      />
      {timeRange.kind === 'custom' && (
        <>
          <input
            className="pl-input pl-input--mono"
            type="datetime-local"
            aria-label="Custom range start (local time)"
            value={draftFrom}
            onChange={(e) => setDraftFrom(e.target.value)}
          />
          <span className="pl-muted pl-small">to</span>
          <input
            className="pl-input pl-input--mono"
            type="datetime-local"
            aria-label="Custom range end (local time)"
            value={draftTo}
            onChange={(e) => setDraftTo(e.target.value)}
          />
          <button
            className="pl-btn pl-btn--compact"
            disabled={!draftFrom || !draftTo}
            onClick={() => setCustomRange(draftFrom, draftTo)}
          >
            Apply
          </button>
        </>
      )}
    </div>
  );
};

export const RouteControl: React.FC = () => {
  const { routeFocus, setRouteFocus } = useAuditContext();
  return (
    <Segmented
      ariaLabel="Route focus"
      options={ROUTE_OPTIONS}
      value={routeFocus}
      onChange={setRouteFocus}
    />
  );
};

export const HistorySnapshotControl: React.FC = () => {
  const { snapshot, refreshHistory } = useAuditContext();
  if (!snapshot) return null;
  return (
    <div className="pl-context-bar__right">
      <span className="pl-snapshot-chip" title="History snapshot boundary — pagination is evaluated against this frozen [from, to) window. Refresh advances the boundary and returns to page 1.">
        <IconSnapshot />
        Snapshot to {formatLocalDateTime(snapshot.to)}
      </span>
      <button className="pl-btn pl-btn--quiet pl-btn--compact" onClick={refreshHistory} title="Advance snapshot to now and return to page 1">
        <IconRefresh />
        Refresh
      </button>
    </div>
  );
};
