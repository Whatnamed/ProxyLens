/**
 * ProxyLens Non-visual API Error Mapping Utilities
 */

import type { ApiErrorResponse } from '../api/types';

export function mapApiErrorCode(code: string | undefined): string {
  switch (code) {
    case 'UNAUTHORIZED':
      return 'Session authentication failed. Please reopen ProxyLens.';
    case 'FORBIDDEN':
      return 'Request origin not allowed by security CORS policy.';
    case 'DB_UNAVAILABLE':
      return 'Database file is unavailable or not configured.';
    case 'DB_INCOMPATIBLE':
      return 'Database schema version does not match this ProxyLens version.';
    case 'CONNECTION_NOT_FOUND':
      return 'The requested connection was not found in audit records.';
    case 'INVALID_TIMESTAMP':
      return 'Invalid time format. Please provide RFC3339 timestamps.';
    case 'QUERY_FAILED':
      return 'Failed to execute query. Check local query logs for details.';
    default:
      return 'An unexpected error occurred.';
  }
}

export function formatErrorMessage(err: unknown): string {
  if (!err) return 'Unknown error';
  if (typeof err === 'string') return err;
  if (typeof err === 'object' && err !== null) {
    const apiErr = err as Partial<ApiErrorResponse>;
    if (apiErr.error && typeof apiErr.error === 'object') {
      const codeMsg = mapApiErrorCode(apiErr.error.code);
      return apiErr.error.message ? `${codeMsg} (${apiErr.error.message})` : codeMsg;
    }
    if ('message' in err && typeof (err as Error).message === 'string') {
      return (err as Error).message;
    }
  }
  return String(err);
}
