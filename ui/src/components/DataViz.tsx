import React from 'react';
import { normalizeRoute, routeColor, routeSemantics, routeTint } from '../domain/route';
import { splitBytes } from '../utils/format';

/** Route pill. Route is the single most important attribute on any row, so it earns a chip. */
export const RouteBadge: React.FC<{ route?: string | null; size?: 'sm' | 'md' }> = ({
  route,
  size = 'sm',
}) => {
  const key = normalizeRoute(route);
  const sem = routeSemantics(key);
  return (
    <span
      className={`pl-badge pl-badge--${size}`}
      style={{ color: routeColor(key), background: routeTint(key) }}
      title={sem.meaning}
    >
      {sem.label}
    </span>
  );
};

/** Neutral chip for non-route metadata (protocol, precision, accounting class...). */
export const Chip: React.FC<{
  children: React.ReactNode;
  tone?: 'neutral' | 'ok' | 'warn' | 'danger' | 'info';
  title?: string;
  mono?: boolean;
}> = ({ children, tone = 'neutral', title, mono = false }) => (
  <span className={`pl-chip pl-chip--${tone} ${mono ? 'is-mono' : ''}`} title={title}>
    {children}
  </span>
);

export const ByteValue: React.FC<{
  bytes: number;
  /** Secondary line, typically the upload/download split. */
  sub?: React.ReactNode;
  tone?: 'default' | 'muted' | 'strong';
}> = ({ bytes, sub, tone = 'default' }) => {
  const { value, unit } = splitBytes(bytes);
  return (
    <span className={`pl-bytes pl-bytes--${tone}`}>
      <span className="pl-bytes__value">{value}</span>
      <span className="pl-bytes__unit">{unit}</span>
      {sub && <span className="pl-bytes__sub">{sub}</span>}
    </span>
  );
};

/**
 * Inline proportion bar used inside ranked rows.
 * Chosen over a separate chart because the question is always "how big is this row
 * relative to the other rows", which a bar in the row answers with no extra scan cost.
 */
export const ProportionBar: React.FC<{
  ratio: number;
  color?: string;
  track?: string;
  height?: number;
}> = ({ ratio, color, track, height = 4 }) => {
  const pct = Math.max(0, Math.min(1, ratio)) * 100;
  return (
    <div className="pl-bar" style={{ height, background: track }}>
      <div className="pl-bar__fill" style={{ width: `${pct}%`, background: color }} />
    </div>
  );
};

/**
 * Route composition bar.
 *
 * This is the one aggregate visual that earns its place: it answers "did any DIRECT or
 * REJECT volume sneak into what I think of as my proxy consumption", which is PRODUCT.md
 * scenario C. A pie chart would answer the same thing with more ink and less precision.
 */
export const RouteCompositionBar: React.FC<{
  segments: { route: string; total: number }[];
  height?: number;
}> = ({ segments, height = 8 }) => {
  const sum = segments.reduce((a, s) => a + s.total, 0);
  if (sum <= 0) return <div className="pl-bar" style={{ height }} />;
  return (
    <div className="pl-bar pl-bar--stack" style={{ height }}>
      {segments.map((s) => (
        <div
          key={s.route}
          className="pl-bar__seg"
          style={{ width: `${(s.total / sum) * 100}%`, background: routeColor(s.route) }}
          title={`${routeSemantics(s.route).label}: ${splitBytes(s.total).value} ${splitBytes(s.total).unit}`}
        />
      ))}
    </div>
  );
};

export const MetricStat: React.FC<{
  label: string;
  value: React.ReactNode;
  unit?: string;
  sub?: React.ReactNode;
  accent?: string;
  emphasis?: boolean;
  title?: string;
}> = ({ label, value, unit, sub, accent, emphasis = false, title }) => (
  <div className={`pl-metric ${emphasis ? 'is-emphasis' : ''}`} title={title}>
    <div className="pl-metric__label">{label}</div>
    <div className="pl-metric__value" style={accent ? { color: accent } : undefined}>
      {value}
      {unit && <span className="pl-metric__unit">{unit}</span>}
    </div>
    {sub && <div className="pl-metric__sub">{sub}</div>}
  </div>
);

/** Coloured dot for trust levels; pairs with the trust strip copy. */
export const TrustDot: React.FC<{ level: 'healthy' | 'partial' | 'degraded' | 'unavailable' }> = ({
  level,
}) => <span className={`pl-dot pl-dot--${level}`} aria-hidden="true" />;
