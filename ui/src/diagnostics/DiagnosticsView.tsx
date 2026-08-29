import React, { useEffect, useRef } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { localeTag } from '../i18n';
import { useLocale } from '../state/AuditContext';
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
  const { locale, t } = useLocale();
  const formatNumber = (value: number) => value.toLocaleString(localeTag(locale));
  const diagRange = React.useMemo(
    () => ({ from: new Date(0).toISOString(), to: new Date().toISOString() }),
    []
  );
  const metaQuery = useMetaQuery(client);
  const summaryQuery = useSummaryQuery(client, diagRange.from, diagRange.to);
  const topProcQuery = useTopProcessesQuery(client, diagRange.from, diagRange.to, 'ALL', 5);
  const coverageQuery = useCoverageQuery(client, diagRange.from, diagRange.to);
  const connsQuery = useConnectionsQuery(client, {
    from: diagRange.from,
    to: diagRange.to,
    route: 'ALL',
    filters: {},
    limit: 5,
    offset: 0,
  });

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
          {t('diagnostics.platformTitle')}
          <span className={`diag-badge ${isTauri ? 'badge-ready' : 'badge-stale'}`}>
            {isTauri ? t('diagnostics.tauriShell') : t('diagnostics.browserMode')}
          </span>
        </h2>
        <p className="diag-description">
          {t('diagnostics.description')}
        </p>
      </div>

      {sessionError && (
        <div className="alert-banner alert-err">
          <strong>{t('diagnostics.sessionError')}:</strong> {sessionError}
        </div>
      )}

      {/* Grid 1: Meta & Freshness */}
      <div className="diag-grid">
        <div className="diag-card">
          <h3>{t('diagnostics.metaTitle')}</h3>
          {metaQuery.isLoading ? (
            <p>{t('diagnostics.loadingMetadata')}</p>
          ) : metaQuery.isError ? (
            <div className="alert-banner alert-err">
              <strong>{t('diagnostics.error')}:</strong> {metaQuery.error.message}
            </div>
          ) : (
            <div>
              <table className="diag-table">
                <tbody>
                  <tr><th>{t('diagnostics.apiVersion')}</th><td>{metaQuery.data?.apiVersion}</td></tr>
                  <tr><th>{t('diagnostics.appVersion')}</th><td>{metaQuery.data?.appVersion}</td></tr>
                  <tr><th>{t('diagnostics.dbState')}</th><td><strong>{metaQuery.data?.dbState}</strong></td></tr>
                  <tr><th>{t('diagnostics.schemaVersion')}</th><td>{metaQuery.data?.schemaVersion} ({t('diagnostics.maxVersion')}: {metaQuery.data?.maxBinarySchemaVersion})</td></tr>
                  <tr>
                    <th>{t('diagnostics.freshness')}</th>
                    <td>
                      {metaQuery.data?.freshness ? (
                        <span className={`diag-badge ${metaQuery.data.freshness.isFresh ? 'badge-ready' : 'badge-stale'}`}>
                          {metaQuery.data.freshness.isFresh
                            ? t('diagnostics.fresh', { lag: 0 })
                            : t('diagnostics.stale', { lag: metaQuery.data.freshness.lagEvents })}
                        </span>
                      ) : t('common.notAvailable')}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div className="diag-card">
          <h3>{t('diagnostics.summaryTitle')}</h3>
          {summaryQuery.isLoading ? (
            <p>{t('diagnostics.loadingSummary')}</p>
          ) : summaryQuery.isError ? (
            <div className="alert-banner alert-err">
              <strong>{t('diagnostics.error')}:</strong> {summaryQuery.error.message}
            </div>
          ) : (
            <div>
              <table className="diag-table">
                <tbody>
                  <tr><th>{t('diagnostics.proxyTraffic')}</th><td>{formatNumber(summaryQuery.data?.proxyUpload ?? 0)} B / {formatNumber(summaryQuery.data?.proxyDownload ?? 0)} B</td></tr>
                  <tr><th>{t('diagnostics.directTraffic')}</th><td>{formatNumber(summaryQuery.data?.directUpload ?? 0)} B / {formatNumber(summaryQuery.data?.directDownload ?? 0)} B</td></tr>
                  <tr><th>{t('diagnostics.missingAttribution')}</th><td>{formatNumber(summaryQuery.data?.missingAttributionDownload ?? 0)} B</td></tr>
                  <tr><th>{t('diagnostics.ambiguousRelay')}</th><td>{formatNumber(summaryQuery.data?.ambiguousRelayDownload ?? 0)} B</td></tr>
                  <tr><th>{t('diagnostics.accountingVersion')}</th><td><code>{summaryQuery.data?.accountingVersion}</code></td></tr>
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>

      {/* Grid 2: Top Processes & Coverage */}
      <div className="diag-grid">
        <div className="diag-card">
          <h3>{t('diagnostics.processTitle', { count: 5 })}</h3>
          {topProcQuery.isLoading ? (
            <p>{t('diagnostics.loadingProcesses')}</p>
          ) : topProcQuery.isError ? (
            <div className="alert-banner alert-err">
              <strong>{t('diagnostics.error')}:</strong> {topProcQuery.error.message}
            </div>
          ) : (
            <table className="diag-table">
              <thead>
                <tr>
                  <th>{t('diagnostics.process')}</th>
                  <th>{t('diagnostics.route')}</th>
                  <th>{t('diagnostics.totalBytes')}</th>
                  <th>{t('diagnostics.connections')}</th>
                </tr>
              </thead>
              <tbody>
                {topProcQuery.data?.items?.map((item, idx) => (
                  <tr key={idx}>
                    <td><code>{item.key || '(empty)'}</code></td>
                    <td>{item.route}</td>
                    <td>{formatNumber(item.totalBytes)} B</td>
                    <td>{item.connectionCount}</td>
                  </tr>
                ))}
                {(!topProcQuery.data?.items || topProcQuery.data.items.length === 0) && (
                  <tr><td colSpan={4} className="diag-empty">{t('diagnostics.noProcesses')}</td></tr>
                )}
              </tbody>
            </table>
          )}
        </div>

        <div className="diag-card">
          <h3>{t('diagnostics.coverageTitle')}</h3>
          {coverageQuery.isLoading ? (
            <p>{t('diagnostics.loadingCoverage')}</p>
          ) : coverageQuery.isError ? (
            <div className="alert-banner alert-err">
              <strong>{t('diagnostics.error')}:</strong> {coverageQuery.error.message}
            </div>
          ) : (
            <div>
              <table className="diag-table">
                <tbody>
                  <tr>
                    <th>{t('diagnostics.coverageRatio')}</th>
                    <td>
                      {coverageQuery.data?.coverageRatio !== undefined ? (
                        <strong>{(coverageQuery.data.coverageRatio * 100).toFixed(1)}%</strong>
                      ) : t('common.notAvailable')}
                    </td>
                  </tr>
                  <tr><th>{t('diagnostics.coveredDuration')}</th><td>{((coverageQuery.data?.coveredDurationMs ?? 0) / 1000).toFixed(1)} s</td></tr>
                  <tr><th>{t('diagnostics.uncoveredDuration')}</th><td>{((coverageQuery.data?.uncoveredDurationMs ?? 0) / 1000).toFixed(1)} s</td></tr>
                  <tr><th>{t('diagnostics.mergedGaps')}</th><td>{t('diagnostics.gapCount', { count: coverageQuery.data?.mergedGaps?.length ?? 0 })}</td></tr>
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>

      {/* Grid 3: Connections Sample */}
      <div className="diag-card">
        <h3>{t('diagnostics.connectionTitle', { count: 5 })}</h3>
        {connsQuery.isLoading ? (
          <p>{t('diagnostics.loadingConnections')}</p>
        ) : connsQuery.isError ? (
          <div className="alert-banner alert-err">
            <strong>{t('diagnostics.error')}:</strong> {connsQuery.error.message}
          </div>
        ) : (
          <table className="diag-table">
            <thead>
              <tr>
                <th>{t('diagnostics.id')}</th>
                <th>{t('diagnostics.process')}</th>
                <th>{t('diagnostics.hostDest')}</th>
                <th>{t('diagnostics.route')}</th>
                <th>{t('diagnostics.rule')}</th>
                <th>{t('diagnostics.proxyChain')}</th>
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
                <tr><td colSpan={6} className="diag-empty">{t('diagnostics.noConnections')}</td></tr>
              )}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
};
