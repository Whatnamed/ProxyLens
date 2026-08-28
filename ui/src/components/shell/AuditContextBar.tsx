import React, { useEffect, useRef, useState } from 'react';
import { useAudit, WindowKind } from '../../state/AuditContext';
import { describeWindow, formatLocalTime, formatLocalDateTime } from '../../utils/time';
import { ROUTES, ROUTE_META, routeKey } from '../../lib/semantics';
import { Segmented } from '../ui/primitives';
import { IconRefresh, IconChevronDown } from '../ui/icons';

/**
 * The shared audit context: Time Range and Route Focus.
 *
 * This bar is deliberately global. Moving between Overview, History and
 * Coverage must never silently reset the window under investigation.
 */

const QUICK_WINDOWS: { value: string; label: string }[] = [
  { value: 'today', label: 'Today' },
  { value: 'yesterday', label: 'Yesterday' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' }
];

const ROUTE_OPTIONS = ROUTES.map((r) => ({
  value: r,
  label: ROUTE_META[r].short,
  color: `var(--route-${r.toLowerCase()})`
}));

/** `datetime-local` works in local wall-clock time; the API needs UTC ISO. */
function toLocalInputValue(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fromLocalInputValue(value: string): string | null {
  if (!value) return null;
  const d = new Date(value);
  if (isNaN(d.getTime())) return null;
  return d.toISOString();
}

export const AuditContextBar: React.FC = () => {
  const { range, setWindow, refresh, route, setRoute } = useAudit();
  const [customOpen, setCustomOpen] = useState(false);
  const [fromVal, setFromVal] = useState('');
  const [toVal, setToVal] = useState('');
  const [rangeError, setRangeError] = useState<string | null>(null);
  const anchorRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!customOpen) return;
    setFromVal(toLocalInputValue(range.from));
    setToVal(toLocalInputValue(range.to));
    setRangeError(null);
    const onDown = (e: MouseEvent) => {
      if (!anchorRef.current?.contains(e.target as Node)) setCustomOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setCustomOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [customOpen, range.from, range.to]);

  const applyCustom = () => {
    const from = fromLocalInputValue(fromVal);
    const to = fromLocalInputValue(toVal);
    if (!from || !to) {
      setRangeError('Both start and end are required.');
      return;
    }
    if (new Date(from) >= new Date(to)) {
      setRangeError('Start must be before end. The window is half-open, [from, to).');
      return;
    }
    setWindow('custom', { from, to });
    setCustomOpen(false);
  };

  const isCustom = range.kind === 'custom';
  const windowValue = isCustom ? 'custom' : range.kind;

  return (
    <header className="contextbar">
      <div className="contextbar-group">
        <span className="contextbar-label">Window</span>
        <Segmented
          value={windowValue}
          options={QUICK_WINDOWS}
          onChange={(v) => setWindow(v as WindowKind)}
          ariaLabel="Time window"
        />
        <div className="popover-anchor" ref={anchorRef}>
          <button
            className={`btn btn--sm ${isCustom ? 'btn--primary' : ''}`}
            onClick={() => setCustomOpen((v) => !v)}
            aria-expanded={customOpen}
            title="Custom time range"
          >
            Custom
            <IconChevronDown size={11} />
          </button>
          {customOpen && (
            <div className="popover" role="dialog" aria-label="Custom time range">
              <div className="popover-title">Custom range</div>
              <div className="range-form">
                <div className="range-field">
                  <label htmlFor="range-from">From (local, inclusive)</label>
                  <input
                    id="range-from"
                    type="datetime-local"
                    value={fromVal}
                    onChange={(e) => setFromVal(e.target.value)}
                  />
                </div>
                <div className="range-field">
                  <label htmlFor="range-to">To (local, exclusive)</label>
                  <input
                    id="range-to"
                    type="datetime-local"
                    value={toVal}
                    onChange={(e) => setToVal(e.target.value)}
                  />
                </div>
                {rangeError && (
                  <div style={{ fontSize: 'var(--fs-11)', color: 'var(--danger)' }}>{rangeError}</div>
                )}
                <div className="range-actions">
                  <button className="btn btn--sm btn--ghost" onClick={() => setCustomOpen(false)}>
                    Cancel
                  </button>
                  <button className="btn btn--sm btn--primary" onClick={applyCustom}>
                    Apply
                  </button>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>

      <div className="contextbar-group" style={{ minWidth: 0 }}>
        <div className="window-readout">
          <span className="window-range" title={`${formatLocalDateTime(range.from)} → ${formatLocalDateTime(range.to)}`}>
            {describeWindow(range.from, range.to)}
          </span>
          <span className="window-asof">
            Snapshot as of {formatLocalTime(range.to)}
            {range.kind === 'yesterday' || range.kind === 'custom' ? ' · closed range' : ' · live'}
          </span>
        </div>
      </div>

      <div className="contextbar-group contextbar-group--grow">
        <span className="contextbar-label">Route focus</span>
        <Segmented
          value={route}
          options={ROUTE_OPTIONS}
          onChange={(v) => setRoute(routeKey(v))}
          ariaLabel="Route focus"
        />
        <div className="contextbar-divider" />
        <button className="btn btn--sm" onClick={refresh} title="Advance the snapshot to now and return to page 1">
          <IconRefresh size={12} />
          Refresh
        </button>
      </div>
    </header>
  );
};
