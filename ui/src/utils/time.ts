/**
 * ProxyLens Non-visual Timezone & Range Utilities (Local-Day Aware UTC Conversion)
 */

export interface TimeRangeRFC3339 {
  from: string;
  to: string;
}

export type QuickWindowType = 'today' | 'yesterday' | '7d' | '30d';

export function getQuickWindow(type: QuickWindowType, now = new Date()): TimeRangeRFC3339 {
  const current = new Date(now);
  const year = current.getFullYear();
  const month = current.getMonth();
  const date = current.getDate();

  switch (type) {
    case 'today': {
      const start = new Date(year, month, date, 0, 0, 0, 0);
      return {
        from: start.toISOString(),
        to: current.toISOString()
      };
    }
    case 'yesterday': {
      const start = new Date(year, month, date - 1, 0, 0, 0, 0);
      const end = new Date(year, month, date, 0, 0, 0, 0);
      return {
        from: start.toISOString(),
        to: end.toISOString()
      };
    }
    case '7d': {
      const start = new Date(year, month, date - 6, 0, 0, 0, 0);
      return {
        from: start.toISOString(),
        to: current.toISOString()
      };
    }
    case '30d': {
      const start = new Date(year, month, date - 29, 0, 0, 0, 0);
      return {
        from: start.toISOString(),
        to: current.toISOString()
      };
    }
  }
}

export function formatLocalDateTime(isoString: string | null | undefined): string {
  if (!isoString) return '-';
  try {
    const d = new Date(isoString);
    if (isNaN(d.getTime())) return isoString;
    return d.toLocaleString();
  } catch {
    return isoString;
  }
}

/* -------------------------------------------------------------------------
 * Presentation helpers. Storage and API use UTC; everything below renders in
 * the operating system's local timezone (frozen semantic rule).
 * ---------------------------------------------------------------------- */

function toDate(value: string | Date | null | undefined): Date | null {
  if (!value) return null;
  const d = value instanceof Date ? value : new Date(value);
  return isNaN(d.getTime()) ? null : d;
}

/** "14:32:08" */
export function formatLocalTime(value: string | Date | null | undefined): string {
  const d = toDate(value);
  if (!d) return '-';
  return d.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false
  });
}

/** "14:32" */
export function formatLocalTimeShort(value: string | Date | null | undefined): string {
  const d = toDate(value);
  if (!d) return '-';
  return d.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  });
}

/** "2026-08-28" */
export function formatLocalDate(value: string | Date | null | undefined): string {
  const d = toDate(value);
  if (!d) return '-';
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

/** "2026-08-28 14:32:08" — local, unambiguous, sortable. */
export function formatLocalDateTimeCompact(value: string | Date | null | undefined): string {
  const d = toDate(value);
  if (!d) return '-';
  return `${formatLocalDate(d)} ${formatLocalTime(d)}`;
}

/** Date is shown only when the row falls outside the compared day. */
export function formatLocalDateTimeContextual(
  value: string | Date | null | undefined,
  reference: string | Date | null | undefined
): { date: string | null; time: string } {
  const d = toDate(value);
  if (!d) return { date: null, time: '-' };
  const ref = toDate(reference);
  const sameDay =
    !ref ||
    (d.getFullYear() === ref.getFullYear() &&
      d.getMonth() === ref.getMonth() &&
      d.getDate() === ref.getDate());
  return {
    date: sameDay ? null : formatLocalDate(d),
    time: formatLocalTime(d)
  };
}

/** "just now" / "4m ago" / "2h 10m ago" */
export function formatRelative(value: string | Date | null | undefined, now = Date.now()): string {
  const d = toDate(value);
  if (!d) return '-';
  const diff = now - d.getTime();
  if (diff < 0) return formatLocalTime(d);
  if (diff < 45_000) return 'just now';
  const mins = Math.floor(diff / 60_000);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ${mins % 60}m ago`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h ago`;
}

/** Human description of a resolved window in local time. */
export function describeWindow(from: string | Date, to: string | Date): string {
  const f = toDate(from);
  const t = toDate(to);
  if (!f || !t) return '-';
  const sameDay =
    f.getFullYear() === t.getFullYear() &&
    f.getMonth() === t.getMonth() &&
    f.getDate() === t.getDate();
  if (sameDay) {
    return `${formatLocalDate(f)} ${formatLocalTimeShort(f)} – ${formatLocalTimeShort(t)}`;
  }
  return `${formatLocalDate(f)} ${formatLocalTimeShort(f)} – ${formatLocalDate(t)} ${formatLocalTimeShort(t)}`;
}

/**
 * Human description of a quick window relative to local today.
 * Kept separate from getQuickWindow so the frozen window math stays untouched.
 */
export function describeQuickWindow(type: QuickWindowType): string {
  switch (type) {
    case 'today':
      return 'Today (local)';
    case 'yesterday':
      return 'Yesterday (local)';
    case '7d':
      return 'Last 7 days';
    case '30d':
      return 'Last 30 days';
  }
}
