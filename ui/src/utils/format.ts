/**
 * ProxyLens Non-visual Byte Formatting Utilities (IEC 60027-2 Binary Bytes)
 */

export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || isNaN(bytes)) {
    return '0 B';
  }

  const abs = Math.abs(bytes);
  const sign = bytes < 0 ? '-' : '';

  if (abs < 1024) {
    return `${sign}${Math.round(abs)} B`;
  }

  const units = ['KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  let u = -1;
  let val = abs;

  while (val >= 1024 && u < units.length - 1) {
    val /= 1024;
    u++;
  }

  return `${sign}${val.toFixed(2)} ${units[u]}`;
}

/**
 * Unrounded byte count with thousands separators.
 * Frozen rule: formatting is for readability only — reconciliation and
 * drill-down must always be able to reach the exact integer.
 */
export function formatBytesExact(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || isNaN(bytes)) {
    return '0 B';
  }
  return `${Math.round(bytes).toLocaleString('en-US')} B`;
}

/** Duration in milliseconds to a compact human string (e.g. "1h 30m", "45s"). */
export function formatDuration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || isNaN(ms) || ms < 0) return '-';

  const totalSeconds = Math.floor(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;

  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  if (days > 0) {
    return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
  }
  if (hours > 0) {
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
  }
  return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`;
}

/** Ratio (0..1) to percentage string. Returns null when undefined. */
export function formatPercent(ratio: number | null | undefined, digits = 1): string {
  if (ratio === null || ratio === undefined || isNaN(ratio)) return '—';
  return `${(ratio * 100).toFixed(digits)}%`;
}

export function formatCount(n: number | null | undefined): string {
  if (n === null || n === undefined || isNaN(n)) return '—';
  return n.toLocaleString('en-US');
}
