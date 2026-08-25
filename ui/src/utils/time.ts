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

  switch (type) {
    case 'today': {
      const start = new Date(current.getFullYear(), current.getMonth(), current.getDate(), 0, 0, 0, 0);
      return {
        from: start.toISOString(),
        to: current.toISOString()
      };
    }
    case 'yesterday': {
      const start = new Date(current.getFullYear(), current.getMonth(), current.getDate() - 1, 0, 0, 0, 0);
      const end = new Date(current.getFullYear(), current.getMonth(), current.getDate() - 1, 23, 59, 59, 999);
      return {
        from: start.toISOString(),
        to: end.toISOString()
      };
    }
    case '7d': {
      const start = new Date(current.getTime() - 7 * 86400000);
      return {
        from: start.toISOString(),
        to: current.toISOString()
      };
    }
    case '30d': {
      const start = new Date(current.getTime() - 30 * 86400000);
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
