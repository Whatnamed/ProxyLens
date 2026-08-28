import React from 'react';
import { MetaResponse } from '../../api/types';
import { ApiClientError } from '../../api/client';
import { ErrorState, SkeletonRows } from '../../components/ui/primitives';

/**
 * Shared gate for page content: distinguishes session/API unavailability,
 * database unavailable/incompatible states, and normal loading.
 * Non-ideal states are rendered with explicit diagnostics, never a generic failure.
 */
export const PageGate: React.FC<{
  client: unknown | null;
  sessionError: string | null;
  meta: MetaResponse | undefined;
  children: React.ReactNode;
}> = ({ client, sessionError, meta, children }) => {
  if (sessionError) {
    return (
      <div className="pl-gate">
        <div className="pl-gate__panel">
          <ErrorState
            title="Local Query API unavailable"
            body={
              <>
                The desktop shell could not establish a session with the local read-only Query
                API. Audit data cannot be displayed until the sidecar starts successfully.
                <br />
                <span className="pl-secondary">
                  Common cause: no database configured. Set <code className="pl-state__code">PROXYLENS_DB_PATH</code> to
                  an existing ProxyLens SQLite database and restart the app.
                </span>
              </>
            }
            code={sessionError}
          />
        </div>
      </div>
    );
  }

  if (!client) {
    return (
      <div style={{ padding: 'var(--pl-space-6)' }}>
        <SkeletonRows rows={8} />
      </div>
    );
  }

  if (meta && meta.dbState !== 'READY') {
    const incompatible = meta.dbState === 'INCOMPATIBLE';
    return (
      <div className="pl-gate">
        <div className="pl-gate__panel">
          <ErrorState
            title={incompatible ? 'Database schema incompatible' : `Database ${meta.dbState.toLowerCase()}`}
            body={
              incompatible ? (
                <>
                  This database uses schema v{meta.schemaVersion}, newer than the v
                  {meta.maxBinarySchemaVersion} supported by this ProxyLens build. Data is shown
                  read-only once versions align; no migration is performed here.
                </>
              ) : (
                <>The database is not available for read-only queries right now.</>
              )
            }
            code={`dbState=${meta.dbState} schema=v${meta.schemaVersion}`}
          />
        </div>
      </div>
    );
  }

  return <>{children}</>;
};

/** True when the query failed because no completed accounting run exists (empty DB). */
export function isNoAccountingRunError(err: unknown): boolean {
  return err instanceof ApiClientError && err.code === 'NO_COMPLETED_ACCOUNTING_RUN';
}

export function errorCodeOf(err: unknown): string | undefined {
  return err instanceof ApiClientError ? err.code : undefined;
}
