import React, { useEffect, useRef } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { QueryApiClient } from '../api/client';
import { ConnectionRecord } from '../api/types';
import {
  useMetaQuery,
  useSummaryQuery,
  useTopProcessesQuery,
  useCoverageQuery,
  useConnectionsQuery
} from '../api/queries';

interface Props {
  client: QueryApiClient | null;
  isTauri: boolean;
  sessionError: string | null;
}

export const DiagnosticsView: React.FC<Props> = ({ client, isTauri, sessionError }) => {
  const metaQuery = useMetaQuery(client);
  const summaryQuery = useSummaryQuery(client);
  const topProcQuery = useTopProcessesQuery(client, 5);
  const coverageQuery = useCoverageQuery(client);
  const connsQuery = useConnectionsQuery(client, 5, 0);

  const probeReportedRef = useRef(false);

  useEffect(() => {
    if (!client || !isTauri || probeReportedRef.current) return;

    // 仅在显式开启 PROXYLENS_E2E_MODE=1 时执行 WebView -> Tauri -> authenticated Sidecar E2E 探针自检 (B2)
    const runE2EProbe = async () => {
      try {
        const isE2E = await invoke<boolean>('get_e2e_mode');
        if (!isE2E) {
          return;
        }

        const meta = await client.getMeta();
        const summary = await client.getSummary();
        const conns = await client.getConnections({ limit: 1 });

        const metaOk = !!(meta && meta.dbState === 'READY');
        const summaryOk = !!(summary && summary.accountingVersion);
        const connectionsOk = !!(conns && Array.isArray(conns.items));

        probeReportedRef.current = true;
        await invoke('report_e2e_probe', {
          report: {
            metaOk,
            summaryOk,
            connectionsOk,
            error: null
          }
        });
      } catch (err: any) {
        probeReportedRef.current = true;
        await invoke('report_e2e_probe', {
          report: {
            metaOk: false,
            summaryOk: false,
            connectionsOk: false,
            error: err?.message || String(err)
          }
        });
      }
    };

    runE2EProbe();
  }, [client, isTauri]);

  return (
    <div className="diag-container">
      <div className="diag-header">
        <h2>
          ProxyLens Platform Diagnostics
          <span className={`diag-badge ${isTauri ? 'badge-ready' : 'badge-stale'}`}>
            {isTauri ? 'Tauri Native Shell' : 'Browser / Dev Mode'}
          </span>
        </h2>
        <p style={{ color: '#6b7280', fontSize: '13px', margin: 0 }}>
          Phase 3A Temporary Developer Surface — Proves secure Go local query sidecar IPC, SQLite read-only query path, and typed API client contract.
        </p>
      </div>

      {sessionError && (
        <div className="alert-banner alert-err">
          <strong>Session Initialization Error:</strong> {sessionError}
        </div>
      )}

      {/* Grid 1: Meta & Freshness */}
      <div className="diag-grid">
        <div className="diag-card">
          <h3>1. API & System Meta</h3>
          {metaQuery.isLoading ? (
            <p>Loading metadata...</p>
          ) : metaQuery.isError ? (
            <div className="alert-banner alert-err">
              <strong>Error:</strong> {metaQuery.error.message}
            </div>
          ) : (
            <div>
              <table className="diag-table">
                <tbody>
                  <tr><th>API Version</th><td>{metaQuery.data?.apiVersion}</td></tr>
                  <tr><th>App Version</th><td>{metaQuery.data?.appVersion}</td></tr>
                  <tr><th>DB State</th><td><strong>{metaQuery.data?.dbState}</strong></td></tr>
                  <tr><th>Schema Version</th><td>{metaQuery.data?.schemaVersion} (Max: {metaQuery.data?.maxBinarySchemaVersion})</td></tr>
                  <tr>
                    <th>Freshness</th>
                    <td>
                      {metaQuery.data?.freshness ? (
                        <span className={`diag-badge ${metaQuery.data.freshness.isFresh ? 'badge-ready' : 'badge-stale'}`}>
                          {metaQuery.data.freshness.isFresh ? 'FRESH (Lag: 0)' : `STALE (Lag: ${metaQuery.data.freshness.lagEvents})`}
                        </span>
                      ) : 'N/A'}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div className="diag-card">
          <h3>2. Analytics Summary (Latest Reconciled Run)</h3>
          {summaryQuery.isLoading ? (
            <p>Loading summary...</p>
          ) : summaryQuery.isError ? (
            <div className="alert-banner alert-err">
              <strong>Error:</strong> {summaryQuery.error.message}
            </div>
          ) : (
            <div>
              <table className="diag-table">
                <tbody>
                  <tr><th>Proxy (Up / Down)</th><td>{(summaryQuery.data?.proxyUpload ?? 0).toLocaleString()} B / {(summaryQuery.data?.proxyDownload ?? 0).toLocaleString()} B</td></tr>
                  <tr><th>Direct (Up / Down)</th><td>{(summaryQuery.data?.directUpload ?? 0).toLocaleString()} B / {(summaryQuery.data?.directDownload ?? 0).toLocaleString()} B</td></tr>
                  <tr><th>Missing Attribution</th><td>{(summaryQuery.data?.missingAttributionDownload ?? 0).toLocaleString()} B</td></tr>
                  <tr><th>Ambiguous Relay</th><td>{(summaryQuery.data?.ambiguousRelayDownload ?? 0).toLocaleString()} B</td></tr>
                  <tr><th>Accounting Version</th><td><code>{summaryQuery.data?.accountingVersion}</code></td></tr>
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>

      {/* Grid 2: Top Processes & Coverage */}
      <div className="diag-grid">
        <div className="diag-card">
          <h3>3. Top 5 Processes</h3>
          {topProcQuery.isLoading ? (
            <p>Loading top processes...</p>
          ) : topProcQuery.isError ? (
            <div className="alert-banner alert-err">
              <strong>Error:</strong> {topProcQuery.error.message}
            </div>
          ) : (
            <table className="diag-table">
              <thead>
                <tr>
                  <th>Process</th>
                  <th>Route</th>
                  <th>Total Bytes</th>
                  <th>Conns</th>
                </tr>
              </thead>
              <tbody>
                {topProcQuery.data?.items?.map((item, idx) => (
                  <tr key={idx}>
                    <td><code>{item.key || '(empty)'}</code></td>
                    <td>{item.route}</td>
                    <td>{item.totalBytes.toLocaleString()} B</td>
                    <td>{item.connectionCount}</td>
                  </tr>
                ))}
                {(!topProcQuery.data?.items || topProcQuery.data.items.length === 0) && (
                  <tr><td colSpan={4} style={{ textAlign: 'center', color: '#9ca3af' }}>No processes recorded</td></tr>
                )}
              </tbody>
            </table>
          )}
        </div>

        <div className="diag-card">
          <h3>4. Monitoring Coverage</h3>
          {coverageQuery.isLoading ? (
            <p>Loading coverage...</p>
          ) : coverageQuery.isError ? (
            <div className="alert-banner alert-err">
              <strong>Error:</strong> {coverageQuery.error.message}
            </div>
          ) : (
            <div>
              <table className="diag-table">
                <tbody>
                  <tr>
                    <th>Coverage Ratio</th>
                    <td>
                      {coverageQuery.data?.coverageRatio !== undefined ? (
                        <strong>{(coverageQuery.data.coverageRatio * 100).toFixed(1)}%</strong>
                      ) : 'N/A'}
                    </td>
                  </tr>
                  <tr><th>Covered Duration</th><td>{((coverageQuery.data?.coveredDurationMs ?? 0) / 1000).toFixed(1)} s</td></tr>
                  <tr><th>Uncovered Duration</th><td>{((coverageQuery.data?.uncoveredDurationMs ?? 0) / 1000).toFixed(1)} s</td></tr>
                  <tr><th>Merged Gaps</th><td>{coverageQuery.data?.mergedGaps?.length ?? 0} gap(s)</td></tr>
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>

      {/* Grid 3: Connections Sample */}
      <div className="diag-card">
        <h3>5. Connection Sample (Limit: 5)</h3>
        {connsQuery.isLoading ? (
          <p>Loading connections...</p>
        ) : connsQuery.isError ? (
          <div className="alert-banner alert-err">
            <strong>Error:</strong> {connsQuery.error.message}
          </div>
        ) : (
          <table className="diag-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Process</th>
                <th>Host / Dest</th>
                <th>Route</th>
                <th>Rule</th>
                <th>Proxy Chain</th>
              </tr>
            </thead>
            <tbody>
              {connsQuery.data?.items?.map((c: ConnectionRecord) => (
                <tr key={`${c.sessionId}-${c.epochId}-${c.connectionId}`}>
                  <td><code>{c.connectionId}</code></td>
                  <td>{c.metadata.process || '-'}</td>
                  <td>{c.metadata.host || c.metadata.destinationIP || '-'}</td>
                  <td>{c.route}</td>
                  <td>{c.rule || '-'}</td>
                  <td>{c.chains?.join(' → ') || '-'}</td>
                </tr>
              ))}
              {(!connsQuery.data?.items || connsQuery.data.items.length === 0) && (
                <tr><td colSpan={6} style={{ textAlign: 'center', color: '#9ca3af' }}>No connections in database</td></tr>
              )}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
};
