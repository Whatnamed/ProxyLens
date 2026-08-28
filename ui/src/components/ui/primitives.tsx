import React from 'react';

/* ---------- Route badge ---------- */

export const RouteBadge: React.FC<{ route: string; quiet?: boolean }> = ({ route, quiet }) => {
  const r = (route || '').toUpperCase();
  if (quiet || r === 'ALL') {
    return <span className="pl-route-badge pl-route-badge--all">{r || 'UNKNOWN'}</span>;
  }
  const cls =
    r === 'PROXY' ? 'pl-route-badge--proxy' : r === 'DIRECT' ? 'pl-route-badge--direct' : r === 'REJECT' ? 'pl-route-badge--reject' : 'pl-route-badge--all';
  return <span className={`pl-route-badge ${cls}`}>{r}</span>;
};

/* ---------- Status indicator ---------- */

export type StatusKind = 'fresh' | 'stale' | 'gap' | 'offline' | 'neutral';

export const StatusIndicator: React.FC<{ kind: StatusKind; label: string; title?: string }> = ({
  kind,
  label,
  title,
}) => (
  <span className={`pl-status pl-status--${kind}`} title={title}>
    <span className="pl-status__dot" />
    {label}
  </span>
);

/* ---------- Evidence chip ---------- */

export type EvidenceKind = 'estimated' | 'ambiguous' | 'missing' | 'neutral';

export const EvidenceChip: React.FC<{ kind: EvidenceKind; label: string; title?: string }> = ({
  kind,
  label,
  title,
}) => (
  <span className={`pl-evidence-chip pl-evidence-chip--${kind}`} title={title}>
    {label}
  </span>
);

/* ---------- Segmented control ---------- */

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
  dotColor?: string;
  title?: string;
}

export function Segmented<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
}: {
  options: SegmentedOption<T>[];
  value: T;
  onChange: (v: T) => void;
  ariaLabel?: string;
}) {
  return (
    <div className="pl-segmented" role="tablist" aria-label={ariaLabel}>
      {options.map((o) => (
        <button
          key={o.value}
          role="tab"
          aria-selected={o.value === value}
          title={o.title}
          className={`pl-segmented__item${o.value === value ? ' pl-segmented__item--active' : ''}`}
          onClick={() => onChange(o.value)}
        >
          {o.dotColor && <span className="pl-segmented__dot" style={{ background: o.dotColor }} />}
          {o.label}
        </button>
      ))}
    </div>
  );
}

/* ---------- States ---------- */

export const EmptyState: React.FC<{ title: string; body?: React.ReactNode; actions?: React.ReactNode }> = ({
  title,
  body,
  actions,
}) => (
  <div className="pl-state">
    <div className="pl-state__title">{title}</div>
    {body && <div className="pl-state__body">{body}</div>}
    {actions}
  </div>
);

export const ErrorState: React.FC<{
  title: string;
  body?: React.ReactNode;
  code?: string;
  actions?: React.ReactNode;
}> = ({ title, body, code, actions }) => (
  <div className="pl-state pl-state--error">
    <div className="pl-state__title">{title}</div>
    {body && <div className="pl-state__body">{body}</div>}
    {code && <span className="pl-state__code">{code}</span>}
    {actions}
  </div>
);

export const Skeleton: React.FC<{ width?: number | string; height?: number; style?: React.CSSProperties }> = ({
  width = '100%',
  height = 14,
  style,
}) => <div className="pl-skeleton" style={{ width, height, ...style }} />;

export const SkeletonRows: React.FC<{ rows?: number }> = ({ rows = 6 }) => (
  <div style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: '12px 8px' }}>
    {Array.from({ length: rows }).map((_, i) => (
      <Skeleton key={i} height={16} width={`${88 - (i % 3) * 9}%`} />
    ))}
  </div>
);

/* ---------- Key/value ---------- */

export const KeyValue: React.FC<{
  items: { key: string; value: React.ReactNode; mono?: boolean }[];
}> = ({ items }) => (
  <div className="pl-kv">
    {items.map((it) => (
      <React.Fragment key={it.key}>
        <span className="pl-kv__key">{it.key}</span>
        <span className={`pl-kv__value${it.mono ? ' pl-kv__value--mono' : ''}`}>{it.value ?? '-'}</span>
      </React.Fragment>
    ))}
  </div>
);

/* ---------- Copy icon button ---------- */

export const CopyButton: React.FC<{ text: string; title?: string }> = ({ text, title }) => {
  const [copied, setCopied] = React.useState(false);
  return (
    <button
      className="pl-icon-btn"
      title={copied ? 'Copied' : title ?? 'Copy'}
      aria-label={title ?? 'Copy'}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text);
          setCopied(true);
          setTimeout(() => setCopied(false), 1200);
        } catch {}
      }}
    >
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
        <rect x="9" y="9" width="11" height="11" rx="1.5" />
        <path d="M5 15V5a1 1 0 0 1 1-1h9" />
      </svg>
    </button>
  );
};

/* ---------- Section ---------- */

export const Section: React.FC<{
  title: string;
  sub?: React.ReactNode;
  right?: React.ReactNode;
  children: React.ReactNode;
}> = ({ title, sub, right, children }) => (
  <section className="pl-section">
    <div style={{ display: 'flex', alignItems: 'baseline', gap: 'var(--pl-space-3)' }}>
      <h2 className="pl-section__title">{title}</h2>
      {right && <div style={{ marginLeft: 'auto', display: 'flex', gap: 'var(--pl-space-2)' }}>{right}</div>}
    </div>
    {sub && <div className="pl-section__sub">{sub}</div>}
    {children}
  </section>
);
