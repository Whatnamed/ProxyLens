import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  addCalendarDays,
  calendarDays,
  calendarDateKey,
  clampCalendarDay,
  daysInCalendarMonth,
  formatLocalDateTimeInput,
  parseLocalDateTimeInput,
  withLocalDateAndTime,
} from './localDateTime.js';

describe('Local date/time picker utilities', () => {
  it('parses strict local minute values and rejects rollover dates', () => {
    const parsed = parseLocalDateTimeInput('2026-08-29T09:05');
    assert.ok(parsed);
    assert.equal(formatLocalDateTimeInput(parsed), '2026-08-29T09:05');
    assert.equal(parseLocalDateTimeInput('2026-02-30T09:05'), null);
    assert.equal(parseLocalDateTimeInput('2026-08-29T24:00'), null);
    assert.equal(parseLocalDateTimeInput('2026-8-29T09:05'), null);
  });

  it('combines a calendar date and a validated 24-hour time', () => {
    const date = new Date(2026, 7, 29);
    assert.equal(withLocalDateAndTime(date, '23:59'), '2026-08-29T23:59');
    assert.equal(withLocalDateAndTime(date, '24:00'), null);
  });

  it('always renders a six-row calendar grid', () => {
    const days = calendarDays(2026, 7);
    assert.equal(days.length, 42);
    assert.equal(days.filter((day) => day !== null).length, 31);
    assert.equal(days[6], 1);
  });

  it('moves calendar focus by local calendar days across month boundaries', () => {
    const next = addCalendarDays(new Date(2026, 7, 31), 1);
    const previous = addCalendarDays(new Date(2026, 8, 1), -1);
    assert.equal(calendarDateKey(next), '2026-09-01');
    assert.equal(calendarDateKey(previous), '2026-08-31');
  });

  it('clamps month navigation to the last valid day', () => {
    assert.equal(daysInCalendarMonth(2026, 1), 28);
    assert.equal(calendarDateKey(clampCalendarDay(2026, 1, 31)), '2026-02-28');
    assert.equal(calendarDateKey(clampCalendarDay(2026, 1, 0)), '2026-02-01');
  });
});
