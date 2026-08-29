import React from 'react';
import { useLocale } from '../../state/AuditContext';

interface ErrorBoundaryState {
  error: Error | null;
}

const ErrorFallback: React.FC<{ error: Error; onReload: () => void }> = ({ error, onReload }) => {
  const { t } = useLocale();
  return (
    <div className="pl-gate">
      <div className="pl-gate__panel">
        <div className="pl-state pl-state--error">
          <div className="pl-state__title">{t('error.interface')}</div>
          <div className="pl-state__body">
            {t('error.interfaceBody', { message: error.message || String(error) })}
          </div>
          <button className="pl-btn" style={{ marginTop: 12 }} onClick={onReload}>
            {t('error.reload')}
          </button>
        </div>
      </div>
    </div>
  );
};

/**
 * Last-line guard: a render exception in any page must degrade to an
 * explainable, recoverable state — never a blank window.
 */
export class ErrorBoundary extends React.Component<{ children: React.ReactNode }, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    return <ErrorFallback error={error} onReload={() => window.location.reload()} />;
  }
}
