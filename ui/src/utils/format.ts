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
