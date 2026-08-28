/**
 * ProxyLens Non-visual Byte Formatting Utilities (IEC 60027-2 Binary Bytes)
 */

/**
 * Split a byte count into a value and its unit so a UI can style them differently
 * (large numeric glyph, smaller unit label). Keeps the same rounding as formatBytes.
 */
export function splitBytes(bytes: number | null | undefined): { value: string; unit: string } {
  if (bytes === null || bytes === undefined || isNaN(bytes)) {
    return { value: '0', unit: 'B' };
  }
  const abs = Math.abs(bytes);
  const sign = bytes < 0 ? '-' : '';
  if (abs < 1024) return { value: `${sign}${Math.round(abs)}`, unit: 'B' };

  const units = ['KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  let u = -1;
  let val = abs;
  while (val >= 1024 && u < units.length - 1) {
    val /= 1024;
    u++;
  }
  return { value: `${sign}${val.toFixed(2)}`, unit: units[u] };
}

/** Thousands-separated integer counter (connections, events, rows). */
export function formatCount(n: number | null | undefined): string {
  if (n === null || n === undefined || isNaN(n)) return '0';
  return Math.round(n).toLocaleString('en-US');
}

/** Render a 0..1 ratio as a percentage. Returns a placeholder when the ratio is undefined. */
export function formatPercent(ratio: number | null | undefined, digits = 1): string {
  if (ratio === null || ratio === undefined || isNaN(ratio)) return '—';
  return `${(Math.max(0, Math.min(1, ratio)) * 100).toFixed(digits)}%`;
}

/**
 * Human duration from milliseconds. Audit windows and gaps span seconds to weeks,
 * so the two largest units are enough and avoid noisy strings like "1d 3h 20m 9s".
 */
export function formatDuration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || isNaN(ms) || ms < 0) return '—';
  if (ms < 1000) return `${Math.round(ms)} ms`;

  const totalSeconds = Math.floor(ms / 1000);
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  if (days > 0) return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
  if (hours > 0) return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
  if (minutes > 0) return seconds > 0 ? `${minutes}m ${seconds}s` : `${minutes}m`;
  return `${seconds}s`;
}

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
