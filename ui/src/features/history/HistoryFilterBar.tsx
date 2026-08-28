import React, { useState } from 'react';
import { useAudit, FilterKey } from '../../state/AuditContext';
import { Chip } from '../../components/ui/primitives';
import { IconFilter } from '../../components/ui/icons';
import { formatLocalTime } from '../../utils/time';

/**
 * Explicit, dimension-aware filters.
 *
 * Deliberately not a universal search box: the Query API can only filter by
 * these four dimensions, so offering a free-text search would imply a
 * capability that does not exist.
 */

const DIMENSIONS: { key: FilterKey; label: string; placeholder: string; fixed?: string[] }[] = [
  { key: 'process', label: 'Process', placeholder: 'chrome.exe' },
  { key: 'host', label: 'Host', placeholder: 'github.com' },
  { key: 'destinationIp', label: 'Dest IP', placeholder: '93.184.216.34' },
  { key: 'network', label: 'Network', placeholder: 'tcp', fixed: ['tcp', 'udp'] }
];

const DIM_LABEL: Record<FilterKey, string> = {
  process: 'Process',
  host: 'Host',
  destinationIp: 'Dest IP',
  network: 'Network'
};

export const HistoryFilterBar: React.FC = () => {
  const { filters, setFilter, removeFilter, clearFilters, activeFilterCount, range, refresh } =
    useAudit();
  const [dim, setDim] = useState<FilterKey>('process');
  const [value, setValue] = useState('');

  const activeDim = DIMENSIONS.find((d) => d.key === dim)!;

  const submit = () => {
    if (!value.trim()) return;
    setFilter(dim, value);
    setValue('');
  };

  return (
    <div className="history-toolbar">
      <div className="filter-add">
        <span
          className="contextbar-label"
          style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--sp-2)' }}
        >
          <IconFilter size={11} />
          Filter
        </span>
        <select
          value={dim}
          onChange={(e) => {
            setDim(e.target.value as FilterKey);
            setValue('');
          }}
          aria-label="Filter dimension"
        >
          {DIMENSIONS.map((d) => (
            <option key={d.key} value={d.key}>
              {d.label}
            </option>
          ))}
        </select>

        {activeDim.fixed ? (
          <select
            className="filter-value-select"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            aria-label="Filter value"
          >
            <option value="">select…</option>
            {activeDim.fixed.map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </select>
        ) : (
          <input
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') submit();
            }}
            placeholder={activeDim.placeholder}
            aria-label="Filter value"
          />
        )}

        <button className="btn btn--sm" onClick={submit} disabled={!value.trim()}>
          Add
        </button>
      </div>

      <div className="chip-row">
        {(Object.keys(filters) as FilterKey[]).map((k) => (
          <Chip key={k} label={DIM_LABEL[k]} value={filters[k]!} onRemove={() => removeFilter(k)} />
        ))}
        {activeFilterCount > 1 && (
          <button className="btn btn--sm btn--ghost" onClick={clearFilters}>
            Clear all
          </button>
        )}
      </div>

      <div className="history-toolbar-spacer" />

      <div className="snapshot-note">
        <span>Snapshot</span>
        <span className="mono">{formatLocalTime(range.to)}</span>
        <button
          className="btn btn--sm btn--ghost"
          onClick={refresh}
          title="Advance the snapshot to now and return to page 1"
        >
          Refresh
        </button>
      </div>
    </div>
  );
};
