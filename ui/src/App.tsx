import React, { useEffect, useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { getQueryApiSession, isTauriEnvironment } from './platform/tauri';
import { QueryApiClient } from './api/client';
import { AuditProvider } from './state/AuditContext';
import { AppShell } from './components/shell/AppShell';
import { ErrorBoundary } from './components/common/ErrorBoundary';
import './styles/fonts.css';
import './styles/tokens.css';
import './styles/globals.css';
import './styles/components.css';
import './styles/shell.css';
import './styles/diagnostics.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 2000,
    },
  },
});

export const App: React.FC = () => {
  const [apiClient, setApiClient] = useState<QueryApiClient | null>(null);
  const [sessionError, setSessionError] = useState<string | null>(null);
  const [SurfaceLab, setSurfaceLab] = useState<React.ComponentType | null>(null);
  const isTauri = isTauriEnvironment();
  const surfaceLabRequested = import.meta.env.DEV && typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('surfacelab') === '1';

  useEffect(() => {
    let active = true;

    async function initSession() {
      try {
        const session = await getQueryApiSession();
        if (active) {
          setApiClient(new QueryApiClient(session));
          setSessionError(null);
        }
      } catch (err: unknown) {
        if (active) {
          const msg = err instanceof Error ? err.message : String(err);
          setSessionError(msg);
        }
      }
    }

    initSession();
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!surfaceLabRequested) return;
    let active = true;
    import('./dev/surfaceLab/SurfaceLab').then(({ SurfaceLab: SurfaceLabComponent }) => {
      if (active) setSurfaceLab(() => SurfaceLabComponent);
    });
    return () => {
      active = false;
    };
  }, [surfaceLabRequested]);

  return (
    <QueryClientProvider client={queryClient}>
      <AuditProvider>
        <ErrorBoundary>
          <AppShell client={apiClient} isTauri={isTauri} sessionError={sessionError} />
          {SurfaceLab ? <SurfaceLab /> : null}
        </ErrorBoundary>
      </AuditProvider>
    </QueryClientProvider>
  );
};
