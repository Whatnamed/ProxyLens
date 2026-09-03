import React, { useEffect, useState, useRef } from 'react';
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
  const isTauri = isTauriEnvironment();

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

  const probeReportedRef = useRef(false);

  useEffect(() => {
    if (!apiClient || !isTauri || probeReportedRef.current) return;
    const runE2EProbe = async () => {
      try {
        const { invoke } = await import('@tauri-apps/api/core');
        const meta = await apiClient.getMeta();
        const summary = await apiClient.getSummary();
        const conns = await apiClient.getConnections({ limit: 1 });

        const metaOk = !!(meta && meta.dbState === 'READY');
        const summaryOk = !!(summary && summary.accountingVersion);
        const connectionsOk = !!(conns && Array.isArray(conns.items));

        probeReportedRef.current = true;
        await invoke('report_e2e_probe', {
          report: { metaOk, summaryOk, connectionsOk, error: null },
        });
      } catch (err: unknown) {
        probeReportedRef.current = true;
        try {
          const { invoke } = await import('@tauri-apps/api/core');
          await invoke('report_e2e_probe', {
            report: {
              metaOk: false,
              summaryOk: false,
              connectionsOk: false,
              error: err instanceof Error ? err.message : String(err),
            },
          });
        } catch {}
      }
    };
    runE2EProbe();
  }, [apiClient, isTauri]);

  return (
    <QueryClientProvider client={queryClient}>
      <AuditProvider>
        <ErrorBoundary>
          <AppShell client={apiClient} isTauri={isTauri} sessionError={sessionError} />
        </ErrorBoundary>
      </AuditProvider>
    </QueryClientProvider>
  );
};
