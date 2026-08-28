import React, { useEffect, useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { getQueryApiSession } from './platform/tauri';
import { QueryApiClient } from './api/client';
import { AuditContextProvider } from './state/AuditContext';
import { AppShell } from './components/shell/AppShell';
import { ErrorState } from './components/ui/states';

import './styles/tokens.css';
import './styles/base.css';
import './styles/primitives.css';
import './styles/shell.css';
import './styles/overview.css';
import './styles/history.css';
import './styles/coverage.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 5_000,
      // Evidence windows are frozen deliberately; aggressive refetch would
      // move the ground under an active investigation.
      refetchOnWindowFocus: false
    }
  }
});

const SessionFailure: React.FC<{ message: string; onRetry: () => void }> = ({ message, onRetry }) => (
  <div
    style={{
      height: '100vh',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      padding: 'var(--sp-8)'
    }}
  >
    <div style={{ maxWidth: 560, width: '100%' }}>
      <ErrorState
        error={{
          kind: 'api_unavailable',
          title: 'Query API session unavailable',
          detail: `ProxyLens could not establish an authenticated session with the local Query API. ${message}`,
          code: 'SESSION_INIT_FAILED',
          retryable: true
        }}
        onRetry={onRetry}
      />
    </div>
  </div>
);

export const App: React.FC = () => {
  const [client, setClient] = useState<QueryApiClient | null>(null);
  const [sessionError, setSessionError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let active = true;
    async function init() {
      try {
        const session = await getQueryApiSession();
        if (active) {
          setClient(new QueryApiClient(session));
          setSessionError(null);
        }
      } catch (err: unknown) {
        if (active) setSessionError(err instanceof Error ? err.message : String(err));
      }
    }
    init();
    return () => {
      active = false;
    };
  }, [attempt]);

  if (sessionError) {
    return (
      <QueryClientProvider client={queryClient}>
        <SessionFailure message={sessionError} onRetry={() => setAttempt((a) => a + 1)} />
      </QueryClientProvider>
    );
  }

  return (
    <QueryClientProvider client={queryClient}>
      <AuditContextProvider>
        <AppShell client={client} />
      </AuditContextProvider>
    </QueryClientProvider>
  );
};
