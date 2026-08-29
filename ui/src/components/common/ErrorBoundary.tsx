import React from 'react';

interface ErrorBoundaryState {
  error: Error | null;
}

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
    return (
      <div className="pl-gate">
        <div className="pl-gate__panel">
          <div className="pl-state pl-state--error">
            <div className="pl-state__title">Interface error</div>
            <div className="pl-state__body">
              A rendering error occurred: {error.message || String(error)}. The audit data itself
              is unaffected — reloading restores the interface.
            </div>
            <button className="pl-btn" style={{ marginTop: 12 }} onClick={() => window.location.reload()}>
              Reload interface
            </button>
          </div>
        </div>
      </div>
    );
  }
}
