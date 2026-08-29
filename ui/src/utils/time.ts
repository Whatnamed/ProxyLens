/**
 * ProxyLens Non-visual Timezone & Range Utilities (Local-Day Aware UTC Conversion)
 */

import { Locale, localeTag } from '../i18n';

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

export function formatLocalDateTime(
  isoString: string | null | undefined,
  locale: Locale = 'en',
  options: Intl.DateTimeFormatOptions = {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  },
): string {
  if (!isoString) return '-';
  try {
    const d = new Date(isoString);
    if (isNaN(d.getTime())) return isoString;
    return new Intl.DateTimeFormat(localeTag(locale), options).format(d);
  } catch {
    return isoString;
  }
}

export function formatLocalDateTimeCompact(isoString: string | null | undefined, locale: Locale = 'en'): string {
  return formatLocalDateTime(isoString, locale, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  });
}
