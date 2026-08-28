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

/** `HH:mm:ss` — the resolution that matters when inspecting a connection timeline. */
export function formatLocalTime(isoString: string | null | undefined): string {
  if (!isoString) return '-';
  try {
    const d = new Date(isoString);
    if (isNaN(d.getTime())) return isoString;
    return d.toLocaleTimeString('zh-CN', { hour12: false });
  } catch {
    return isoString;
  }
}

/** `MM-DD HH:mm` — compact enough for dense tables and timeline ticks. */
export function formatLocalShort(isoString: string | null | undefined): string {
  if (!isoString) return '-';
  try {
    const d = new Date(isoString);
    if (isNaN(d.getTime())) return isoString;
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
  } catch {
    return isoString;
  }
}

/** Build an explicit custom window from two local `datetime-local` inputs. */
export function getCustomWindow(fromLocal: string, toLocal: string): TimeRangeRFC3339 | null {
  if (!fromLocal || !toLocal) return null;
  const from = new Date(fromLocal);
  const to = new Date(toLocal);
  if (isNaN(from.getTime()) || isNaN(to.getTime())) return null;
  if (from.getTime() >= to.getTime()) return null;
  return { from: from.toISOString(), to: to.toISOString() };
}

/** `datetime-local` input value (local, minute precision) from an ISO instant. */
export function toLocalInputValue(isoString: string | null | undefined): string {
  if (!isoString) return '';
  const d = new Date(isoString);
  if (isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export const QUICK_WINDOW_LABELS: Record<QuickWindowType, string> = {
  today: '今天',
  yesterday: '昨天',
  '7d': '近 7 天',
  '30d': '近 30 天',
};
