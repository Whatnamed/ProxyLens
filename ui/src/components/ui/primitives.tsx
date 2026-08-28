import React from 'react';
import { formatBytes, formatBytesExact } from '../../utils/format';
import { Tone } from '../../lib/semantics';

/* ==========================================================================
 * Panel
 * ========================================================================== */

export const Panel: React.FC<{
  title?: React.ReactNode;
  note?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  flush?: boolean;
  className?: string;
  style?: React.CSSProperties;
}> = ({ title, note, actions, children, flush, className, style }) => (
  <section className={`panel ${className ?? ''}`} style={style}>
    {(title || actions) && (
      <header className="panel-head">
        <h3 className="panel-title">
          {title}
          {note && <span className="panel-note">{note}</span>}
        </h3>
        {actions && <div style={{ display: 'flex', gap: 'var(--sp-2)', flex: 'none' }}>{actions}</div>}
      </header>
    )}
    <div className={flush ? 'panel-body panel-body--flush' : 'panel-body'}>{children}</div>
  </section>
);

/* ==========================================================================
 * Badge
 * ========================================================================== */

export const Badge: React.FC<{
  tone?: Tone | 'proxy' | 'direct' | 'reject' | 'all';
  mono?: boolean;
  children: React.ReactNode;
  title?: string;
  style?: React.CSSProperties;
}> = ({ tone = 'neutral', mono, children, title, style }) => (
  <span className={`badge badge--${tone} ${mono ? 'badge--mono' : ''}`} title={title} style={style}>
    {children}
  </span>
);

/* ==========================================================================
 * Tooltip
 * ========================================================================== */

export const Tip: React.FC<{
  content: React.ReactNode;
  align?: 'left' | 'right';
  children: React.ReactNode;
  className?: string;
}> = ({ content, align = 'left', children, className }) => (
  <span className={`tip ${className ?? ''}`} tabIndex={-1}>
    {children}
    <span className={`tip-body ${align === 'right' ? 'tip-body--right' : ''}`} role="tooltip">
      {content}
    </span>
  </span>
);

/* ==========================================================================
 * Segmented control
 * ========================================================================== */

export const Segmented: React.FC<{
  value: string;
  options: { value: string; label: string; color?: string }[];
  onChange: (value: string) => void;
  ariaLabel?: string;
}> = ({ value, options, onChange, ariaLabel }) => (
  <div className="segmented" role="tablist" aria-label={ariaLabel}>
    {options.map((o) => (
      <button
        key={o.value}
        role="tab"
        aria-selected={value === o.value}
        className={`segmented-item ${value === o.value ? 'is-active' : ''}`}
        onClick={() => onChange(o.value)}
      >
        {o.color && <span className="seg-dot" style={{ color: o.color }} />}
        {o.label}
      </button>
    ))}
  </div>
);

/* ==========================================================================
 * Byte value — IEC units, exact integer always reachable
 * ========================================================================== */

export const ByteValue: React.FC<{
  bytes: number | null | undefined;
  /** Marks the value as interval-derived / estimated, never as exact. */
  estimated?: boolean;
  /** Precision label from the backend, shown in the tooltip. */
  precision?: string | null;
  className?: string;
  title?: string;
}> = ({ bytes, estimated, precision, className, title }) => {
  const value = bytes ?? 0;
  return (
    <Tip
      content={
        <>
          <div className="tip-row">
            <span className="tip-label">Exact</span>
            <span>{formatBytesExact(value)}</span>
          </div>
          <div className="tip-row">
            <span className="tip-label">Precision</span>
            <span>{precision ?? (estimated ? 'estimated' : 'exact')}</span>
          </div>
        </>
      }
    >
      <span
        className={`mono ${estimated ? 'is-estimated' : ''} ${className ?? ''}`}
        title={title ?? formatBytesExact(value)}
      >
        {formatBytes(value)}
      </span>
    </Tip>
  );
};

/* ==========================================================================
 * Split bar — exact (solid) vs estimated (hatched)
 * ========================================================================== */

export const SplitBar: React.FC<{
  exact: number;
  estimated: number;
  max: number;
  color?: string;
}> = ({ exact, estimated, max, color }) => {
  const safeMax = max > 0 ? max : 1;
  const exactPct = Math.max(0, Math.min(100, (exact / safeMax) * 100));
  const estPct = Math.max(0, Math.min(100 - exactPct, (estimated / safeMax) * 100));
  return (
    <div className="bar-track">
      <div
        className="bar-fill"
        style={{ width: `${exactPct}%`, background: color ?? 'var(--accent)' }}
      />
      <div className="bar-fill bar-fill--est" style={{ width: `${estPct}%` }} />
    </div>
  );
};

/* ==========================================================================
 * Filter chip
 * ========================================================================== */

export const Chip: React.FC<{
  label: string;
  value: string;
  onRemove: () => void;
}> = ({ label, value, onRemove }) => (
  <span className="chip">
    <span className="chip-key">{label}</span>
    <span className="chip-value">{value}</span>
    <button className="chip-remove" onClick={onRemove} aria-label={`Remove ${label} filter`}>
      ×
    </button>
  </span>
);

/* ==========================================================================
 * Section heading inside the Inspector
 * ========================================================================== */

export const Field: React.FC<{ label: string; children: React.ReactNode }> = ({ label, children }) => (
  <div style={{ minWidth: 0 }}>
    <div className="label" style={{ marginBottom: 'var(--sp-1)' }}>
      {label}
    </div>
    <div style={{ minWidth: 0 }}>{children}</div>
  </div>
);
