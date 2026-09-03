import React from 'react';
import { MetaResponse } from '../../api/types';
import { ApiClientError } from '../../api/client';
import { ErrorState, SkeletonRows } from '../../components/ui/primitives';
import { useLocale } from '../../state/AuditContext';

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
  const { t } = useLocale();

  if (sessionError) {
    return (
      <div className="pl-gate">
        <div className="pl-gate__panel">
          <ErrorState
            title={t('gate.apiUnavailable')}
            body={
              <>
                {t('gate.apiUnavailableBody')}
                <br />
                <span className="pl-secondary">
                  {t('gate.commonCause')} <code className="pl-state__code">PROXYLENS_DB_PATH</code> /{' '}
                  <code className="pl-state__code">PROXYLENS_DATA_DIR</code> {t('gate.restart')}
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
            title={incompatible ? t('gate.databaseIncompatible') : t('gate.databaseUnavailable', { state: meta.dbState.toLowerCase() })}
            body={
              incompatible ? (
                <>{t('gate.incompatibleBody', { version: meta.schemaVersion, max: meta.maxBinarySchemaVersion })}</>
              ) : (
                <>{t('gate.unavailableBody')}</>
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
