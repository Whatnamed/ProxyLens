import { useEffect, useRef } from 'react';
import { QueryApiClient } from '../api/client';
import { isTauriEnvironment } from '../platform/tauri';

/**
 * Platform integrity probe.
 *
 * This is not a product feature; it is the assertion that the locked query boundary actually
 * holds at runtime: React → Tauri command → authenticated Go sidecar → read-only SQLite.
 * It only performs any network work when PROXYLENS_E2E_MODE=1, so a normal launch issues
 * no extra requests and the session token is never exposed in a report.
 *
 * It was carried over from the temporary diagnostics surface so the Phase 3A platform smoke
 * test keeps verifying the real path against the real product UI.
 */
export function usePlatformProbe(client: QueryApiClient | null) {
  const reportedRef = useRef(false);

  useEffect(() => {
    if (!client || !isTauriEnvironment() || reportedRef.current) return;

    const runProbe = async () => {
      const { invoke } = await import('@tauri-apps/api/core');
      try {
        const isE2E = await invoke<boolean>('get_e2e_mode');
        if (!isE2E) return;

        const meta = await client.getMeta();
        const summary = await client.getSummary();
        const conns = await client.getConnections({ limit: 1 });

        reportedRef.current = true;
        await invoke('report_e2e_probe', {
          report: {
            metaOk: !!(meta && meta.dbState === 'READY'),
            summaryOk: !!(summary && summary.accountingVersion),
            connectionsOk: !!(conns && Array.isArray(conns.items)),
            error: null,
          },
        });
      } catch (err: unknown) {
        reportedRef.current = true;
        try {
          const { invoke: invokeAgain } = await import('@tauri-apps/api/core');
          await invokeAgain('report_e2e_probe', {
            report: {
              metaOk: false,
              summaryOk: false,
              connectionsOk: false,
              error: err instanceof Error ? err.message : String(err),
            },
          });
        } catch {
          /* The probe must never break the UI, even when reporting fails. */
        }
      }
    };

    void runProbe();
  }, [client]);
}
