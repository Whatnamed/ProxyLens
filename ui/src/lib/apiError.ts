/**
 * Maps transport/API failures onto real product states.
 *
 * These are product states, not developer error messages: a missing accounting
 * run, an unavailable database and an unreachable Query API are three very
 * different things to an auditor and must never collapse into one "Error".
 */

import { ApiClientError } from '../api/client';

export type ErrorKind =
  | 'api_unavailable'
  | 'unauthorized'
  | 'db_unavailable'
  | 'db_incompatible'
  | 'no_accounting_run'
  | 'not_found'
  | 'query_failed'
  | 'invalid_request'
  | 'unknown';

export interface ProductError {
  kind: ErrorKind;
  /** Short headline. */
  title: string;
  /** What actually happened, in auditor language. */
  detail: string;
  /** Raw code, useful when reporting a problem. */
  code: string;
  /** True when the condition is expected to resolve on its own. */
  retryable: boolean;
}

function fromCode(code: string, message: string): ProductError {
  switch (code) {
    case 'API_UNAVAILABLE':
      return {
        kind: 'api_unavailable',
        title: 'Query API unavailable',
        detail:
          'The local ProxyLens Query API is not responding. Evidence cannot be read until it is running again.',
        code,
        retryable: true
      };
    case 'UNAUTHORIZED':
      return {
        kind: 'unauthorized',
        title: 'Session not authorised',
        detail:
          'This window lost its session token. Reopen ProxyLens to establish a new authenticated local session.',
        code,
        retryable: false
      };
    case 'DB_UNAVAILABLE':
      return {
        kind: 'db_unavailable',
        title: 'Database unavailable',
        detail:
          'The ProxyLens database could not be opened in read-only mode. Nothing in this window can be trusted until it is available.',
        code,
        retryable: true
      };
    case 'DB_INCOMPATIBLE':
      return {
        kind: 'db_incompatible',
        title: 'Database schema incompatible',
        detail:
          'The database schema version does not match this ProxyLens build. Reading it could produce wrong accounting, so reads are refused.',
        code,
        retryable: false
      };
    case 'NO_COMPLETED_ACCOUNTING_RUN':
      return {
        kind: 'no_accounting_run',
        title: 'No completed accounting run',
        detail:
          'Raw observations exist but no accounting run has completed yet. Aggregates are deliberately withheld rather than shown as incomplete numbers.',
        code,
        retryable: true
      };
    case 'CONNECTION_NOT_FOUND':
      return {
        kind: 'not_found',
        title: 'Connection not found',
        detail:
          'This connection is no longer present in the audit database. It may have been removed by the retention policy or belong to an older session.',
        code,
        retryable: false
      };
    case 'QUERY_FAILED':
      return {
        kind: 'query_failed',
        title: 'Query failed',
        detail: message || 'The query could not be executed.',
        code,
        retryable: true
      };
    case 'INVALID_TIMESTAMP':
    case 'INVALID_ROUTE':
    case 'INVALID_PATH':
    case 'INVALID_EPOCH_ID':
      return {
        kind: 'invalid_request',
        title: 'Invalid request',
        detail: message || 'The request was rejected because a parameter was invalid.',
        code,
        retryable: false
      };
    default:
      return {
        kind: 'unknown',
        title: 'Unexpected error',
        detail: message || 'An unclassified error occurred while reading evidence.',
        code: code || 'UNKNOWN',
        retryable: true
      };
  }
}

export function toProductError(err: unknown): ProductError {
  if (err instanceof ApiClientError) {
    return fromCode(err.code, err.message);
  }
  if (err instanceof Error) {
    return fromCode('UNKNOWN', err.message);
  }
  return fromCode('UNKNOWN', String(err));
}

/** True for the transient conditions where an automatic retry makes sense. */
export function isRetryable(err: unknown): boolean {
  return toProductError(err).retryable;
}
