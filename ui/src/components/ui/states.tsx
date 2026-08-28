import React from 'react';
import { ProductError } from '../../lib/apiError';
import { IconEmpty, IconRefresh, IconSearchOff, IconWarning, IconInfo, IconDatabase } from './icons';

/* ==========================================================================
 * Skeletons
 * ========================================================================== */

export const SkeletonLine: React.FC<{ width?: string | number; height?: number }> = ({
  width = '100%',
  height = 10
}) => <div className="skeleton" style={{ width, height }} />;

export const SkeletonRows: React.FC<{ rows?: number }> = ({ rows = 8 }) => (
  <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--sp-4)' }}>
    {Array.from({ length: rows }).map((_, i) => (
      <div key={i} style={{ display: 'flex', gap: 'var(--sp-5)', alignItems: 'center' }}>
        <div className="skeleton" style={{ width: 76, height: 9, flex: 'none' }} />
        <div className="skeleton" style={{ flex: 1, height: 9 }} />
        <div className="skeleton" style={{ width: 70, height: 9, flex: 'none' }} />
      </div>
    ))}
  </div>
);

/* ==========================================================================
 * Empty / informational states
 * ========================================================================== */

export const EmptyState: React.FC<{
  title: string;
  desc: React.ReactNode;
  actions?: React.ReactNode;
  glyph?: 'empty' | 'search' | 'info';
}> = ({ title, desc, actions, glyph = 'empty' }) => (
  <div className="state">
    <div className="state-glyph">
      {glyph === 'search' ? <IconSearchOff size={16} /> : glyph === 'info' ? <IconInfo size={16} /> : <IconEmpty size={16} />}
    </div>
    <div className="state-title">{title}</div>
    <p className="state-desc">{desc}</p>
    {actions && <div className="state-actions">{actions}</div>}
  </div>
);

/* ==========================================================================
 * Error / blocked states
 * ========================================================================== */

/**
 * Renders a product state for a failed query.
 * `no_accounting_run` and `not_found` are legitimate states rather than
 * failures, so they are toned down and worded accordingly.
 */
export const ErrorState: React.FC<{
  error: ProductError;
  onRetry?: () => void;
  compact?: boolean;
}> = ({ error, onRetry, compact }) => {
  const isState = error.kind === 'no_accounting_run' || error.kind === 'not_found';
  const tone = isState ? 'state' : error.kind === 'db_incompatible' || error.kind === 'unauthorized' ? 'state--danger' : 'state--warn';

  return (
    <div className={`state ${tone}`} style={compact ? { minHeight: 120, padding: 'var(--sp-7)' } : undefined}>
      <div className="state-glyph">
        {error.kind === 'db_unavailable' || error.kind === 'db_incompatible' ? (
          <IconDatabase size={16} />
        ) : isState ? (
          <IconInfo size={16} />
        ) : (
          <IconWarning size={16} />
        )}
      </div>
      <div className="state-title">{error.title}</div>
      <p className="state-desc">{error.detail}</p>
      {error.kind === 'no_accounting_run' && (
        <p className="state-desc" style={{ fontSize: 'var(--fs-11)' }}>
          Raw observations are preserved regardless. Accounting runs complete on their own cadence;
          nothing is lost while this is pending.
        </p>
      )}
      <div className="state-actions">
        <span className="state-code">{error.code}</span>
        {onRetry && error.retryable && (
          <button className="btn btn--sm" onClick={onRetry}>
            <IconRefresh size={12} />
            Retry
          </button>
        )}
      </div>
    </div>
  );
};

/* ==========================================================================
 * Inline notice
 * ========================================================================== */

export const Notice: React.FC<{
  tone?: 'info' | 'warn' | 'danger' | 'neutral';
  children: React.ReactNode;
  style?: React.CSSProperties;
}> = ({ tone = 'neutral', children, style }) => (
  <div className={`notice notice--${tone}`} style={style}>
    <span className="notice-glyph">
      {tone === 'warn' || tone === 'danger' ? <IconWarning size={13} /> : <IconInfo size={13} />}
    </span>
    <div className="notice-body">{children}</div>
  </div>
);
