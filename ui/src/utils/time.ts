/**
 * ProxyLens Non-visual Timezone & Range Utilities (Local-Day Aware UTC Conversion)
 */

import { Locale, localeTag } from '../i18n';

export interface TimeRangeRFC3339 {
  from: string;
  to: string;
}

export type QuickWindowType = 'today' | 'yesterday' | '7d' | '30d';

export type TemporalWindowKind = QuickWindowType | 'custom';

export interface TemporalComparisonRange {
  baselineRequested: TimeRangeRFC3339;
  recentRequested: TimeRangeRFC3339;
  baselineEffective: TimeRangeRFC3339 | null;
  recentEffective: TimeRangeRFC3339 | null;
  unavailableReason?: 'insufficient_full_hours';
}

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

function shiftLocalCalendarDays(isoString: string, days: number): string {
  const value = new Date(isoString);
  value.setDate(value.getDate() + days);
  return value.toISOString();
}

export function floorToUtcHour(value: Date | string): Date {
  const input = typeof value === 'string' ? new Date(value) : new Date(value);
  return new Date(Date.UTC(
    input.getUTCFullYear(),
    input.getUTCMonth(),
    input.getUTCDate(),
    input.getUTCHours(),
    0,
    0,
    0,
  ));
}

export function ceilToUtcHour(value: Date | string): Date {
  const input = typeof value === 'string' ? new Date(value) : new Date(value);
  const floored = floorToUtcHour(input);
  return floored.getTime() === input.getTime()
    ? floored
    : new Date(floored.getTime() + 60 * 60 * 1000);
}

function effectiveFullHourRange(requested: TimeRangeRFC3339): TimeRangeRFC3339 | null {
  const from = ceilToUtcHour(requested.from);
  const to = floorToUtcHour(requested.to);
  if (!(to.getTime() > from.getTime())) return null;
  return { from: from.toISOString(), to: to.toISOString() };
}

/**
 * Derives the explicit comparison interval from the Review range, then clips
 * both sides inward to complete UTC-hour buckets only. The comparison query
 * must never claim precision for a partial hourly materialization.
 */
export function deriveTemporalComparisonRange(
  kind: TemporalWindowKind,
  recentRequested: TimeRangeRFC3339,
): TemporalComparisonRange {
  let baselineRequested: TimeRangeRFC3339;
  if (kind === 'custom') {
    const recentFrom = new Date(recentRequested.from);
    const recentTo = new Date(recentRequested.to);
    const duration = recentTo.getTime() - recentFrom.getTime();
    baselineRequested = {
      from: new Date(recentFrom.getTime() - duration).toISOString(),
      to: recentFrom.toISOString(),
    };
  } else {
    const shiftDays = kind === 'today' || kind === 'yesterday' ? -1 : kind === '7d' ? -7 : -30;
    baselineRequested = {
      from: shiftLocalCalendarDays(recentRequested.from, shiftDays),
      to: shiftLocalCalendarDays(recentRequested.to, shiftDays),
    };
  }

  const baselineEffective = effectiveFullHourRange(baselineRequested);
  const recentEffective = effectiveFullHourRange(recentRequested);
  return {
    baselineRequested,
    recentRequested,
    baselineEffective,
    recentEffective,
    ...(baselineEffective && recentEffective ? {} : { unavailableReason: 'insufficient_full_hours' as const }),
  };
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
