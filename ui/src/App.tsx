import React, { useEffect, useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { getQueryApiSession, isTauriEnvironment } from './platform/tauri';
import { QueryApiClient } from './api/client';
import { AppFiltersProvider } from './state/AppFilters';
import { Shell } from './app/Shell';
import { usePlatformProbe } from './app/usePlatformProbe';
import './styles/tokens.css';
import './styles/app.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 10_000,
      refetchOnWindowFocus: false,
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
          setSessionError(err instanceof Error ? err.message : String(err));
        }
      }
    }

    void initSession();
    return () => {
      active = false;
    };
  }, []);

  usePlatformProbe(apiClient);

  return (
    <QueryClientProvider client={queryClient}>
      <AppFiltersProvider>
        <Shell client={apiClient} isTauri={isTauri} sessionError={sessionError} />
      </AppFiltersProvider>
    </QueryClientProvider>
  );
};
