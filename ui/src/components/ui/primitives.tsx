import React, { useEffect, useId, useMemo, useRef, useState } from 'react';
import { localeTag } from '../../i18n';
import { useLocale } from '../../state/AuditContext';
import {
  calendarDays,
  formatLocalTimeInput,
  parseLocalDateTimeInput,
  withLocalDateAndTime,
} from '../../utils/localDateTime';
import { IconCalendar, IconCheck, IconChevronDown, IconChevronLeft, IconChevronRight } from './icons';

/* ---------- Route badge ---------- */

export const RouteBadge: React.FC<{ route: string; quiet?: boolean }> = ({ route, quiet }) => {
  const r = (route || '').toUpperCase();
  if (quiet || r === 'ALL') {
    return <span className="pl-route-badge pl-route-badge--all">{r || 'UNKNOWN'}</span>;
  }
  const cls =
    r === 'PROXY' ? 'pl-route-badge--proxy' : r === 'DIRECT' ? 'pl-route-badge--direct' : r === 'REJECT' ? 'pl-route-badge--reject' : 'pl-route-badge--all';
  return <span className={`pl-route-badge ${cls}`}>{r}</span>;
};

/* ---------- Status indicator ---------- */

export type StatusKind = 'fresh' | 'stale' | 'gap' | 'offline' | 'neutral';

export const StatusIndicator: React.FC<{ kind: StatusKind; label: string; title?: string }> = ({
  kind,
  label,
  title,
}) => (
  <span className={`pl-status pl-status--${kind}`} title={title}>
    <span className="pl-status__dot" />
    {label}
  </span>
);

/* ---------- Evidence chip ---------- */

export type EvidenceKind = 'estimated' | 'ambiguous' | 'missing' | 'neutral';

export const EvidenceChip: React.FC<{ kind: EvidenceKind; label: string; title?: string }> = ({
  kind,
  label,
  title,
}) => (
  <span className={`pl-evidence-chip pl-evidence-chip--${kind}`} title={title}>
    {label}
  </span>
);

/* ---------- Segmented control ---------- */

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
  dotColor?: string;
  title?: string;
}

export function Segmented<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
}: {
  options: SegmentedOption<T>[];
  value: T;
  onChange: (v: T) => void;
  ariaLabel?: string;
}) {
  return (
    <div className="pl-segmented" role="tablist" aria-label={ariaLabel}>
      {options.map((o) => (
        <button
          key={o.value}
          role="tab"
          aria-selected={o.value === value}
          title={o.title}
          className={`pl-segmented__item${o.value === value ? ' pl-segmented__item--active' : ''}`}
          onClick={() => onChange(o.value)}
        >
          {o.dotColor && <span className="pl-segmented__dot" style={{ background: o.dotColor }} />}
          {o.label}
        </button>
      ))}
    </div>
  );
}

/* ---------- Design-system select/listbox ---------- */

export interface SelectMenuOption<T extends string | number> {
  value: T;
  label: string;
  title?: string;
}

export function SelectMenu<T extends string | number>({
  options,
  value,
  onChange,
  ariaLabel,
  placement = 'down',
  className,
}: {
  options: SelectMenuOption<T>[];
  value: T;
  onChange: (value: T) => void;
  ariaLabel: string;
  placement?: 'up' | 'down';
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const selectedIndex = Math.max(0, options.findIndex((option) => option.value === value));
  const [highlightedIndex, setHighlightedIndex] = useState(selectedIndex);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const optionRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const listboxId = useId();
  const selected = options[selectedIndex];

  useEffect(() => {
    if (!open) return;
    setHighlightedIndex(selectedIndex);
    requestAnimationFrame(() => optionRefs.current[selectedIndex]?.focus());
  }, [open, selectedIndex]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        setOpen(false);
        triggerRef.current?.focus();
      }
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  const choose = (option: SelectMenuOption<T>) => {
    onChange(option.value);
    setOpen(false);
    requestAnimationFrame(() => triggerRef.current?.focus());
  };

  const moveHighlight = (nextIndex: number) => {
    const next = (nextIndex + options.length) % options.length;
    setHighlightedIndex(next);
    optionRefs.current[next]?.focus();
  };

  const onTriggerKeyDown = (event: React.KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (!open) {
        setOpen(true);
      } else {
        moveHighlight(highlightedIndex + (event.key === 'ArrowDown' ? 1 : -1));
      }
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      setOpen((current) => !current);
    }
  };

  return (
    <div
      ref={rootRef}
      className={`pl-select-menu pl-select-menu--${placement}${className ? ` ${className}` : ''}`}
      data-select-menu
    >
      <button
        ref={triggerRef}
        type="button"
        className="pl-select-menu__trigger"
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listboxId}
        data-value={String(value)}
        onClick={() => setOpen((current) => !current)}
        onKeyDown={onTriggerKeyDown}
      >
        <span className="pl-select-menu__value">{selected?.label ?? String(value)}</span>
        <IconChevronDown size={12} />
      </button>
      {open && (
        <div id={listboxId} className="pl-select-menu__popover" role="listbox" aria-label={ariaLabel}>
          {options.map((option, index) => (
            <button
              key={String(option.value)}
              ref={(element) => { optionRefs.current[index] = element; }}
              type="button"
              role="option"
              aria-selected={option.value === value}
              className={`pl-select-menu__option${option.value === value ? ' pl-select-menu__option--selected' : ''}${index === highlightedIndex ? ' pl-select-menu__option--highlighted' : ''}`}
              title={option.title}
              onClick={() => choose(option)}
              onKeyDown={(event) => {
                if (event.key === 'ArrowDown') {
                  event.preventDefault();
                  moveHighlight(index + 1);
                } else if (event.key === 'ArrowUp') {
                  event.preventDefault();
                  moveHighlight(index - 1);
                } else if (event.key === 'Home') {
                  event.preventDefault();
                  moveHighlight(0);
                } else if (event.key === 'End') {
                  event.preventDefault();
                  moveHighlight(options.length - 1);
                } else if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault();
                  choose(option);
                }
              }}
            >
              <span className="pl-select-menu__option-label">{option.label}</span>
              <span className="pl-select-menu__option-check" aria-hidden="true">
                {option.value === value && <IconCheck size={13} />}
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

/* ---------- Controlled date/time picker ---------- */

function sameCalendarDate(left: Date | null, right: Date): boolean {
  return !!left && left.getFullYear() === right.getFullYear() && left.getMonth() === right.getMonth() && left.getDate() === right.getDate();
}

function isValidTime(value: string): boolean {
  return /^(?:[01]\d|2[0-3]):[0-5]\d$/.test(value);
}

export const DateTimePicker: React.FC<{
  value: string;
  onChange: (value: string) => void;
  label: string;
  placeholder?: string;
  defaultTime?: string;
  disabled?: boolean;
  className?: string;
}> = ({ value, onChange, label, placeholder, defaultTime = '00:00', disabled, className }) => {
  const { locale, t } = useLocale();
  const [open, setOpen] = useState(false);
  const [viewDate, setViewDate] = useState(() => {
    const current = parseLocalDateTimeInput(value) ?? new Date();
    return new Date(current.getFullYear(), current.getMonth(), 1);
  });
  const [timeDraft, setTimeDraft] = useState(defaultTime);
  const [timeError, setTimeError] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const timeInputId = useId();
  const selectedDate = parseLocalDateTimeInput(value);
  const monthLabel = new Intl.DateTimeFormat(localeTag(locale), { year: 'numeric', month: 'long' }).format(viewDate);
  const displayedValue = selectedDate
    ? new Intl.DateTimeFormat(localeTag(locale), {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        hourCycle: 'h23',
      }).format(selectedDate)
    : placeholder ?? t('datePicker.placeholder');
  const weekdays = t('datePicker.weekdays').split(',');
  const days = useMemo(() => calendarDays(viewDate.getFullYear(), viewDate.getMonth()), [viewDate]);

  useEffect(() => {
    if (!open) return;
    const current = parseLocalDateTimeInput(value);
    const anchor = current ?? new Date();
    setViewDate(new Date(anchor.getFullYear(), anchor.getMonth(), 1));
    setTimeDraft(current ? formatLocalTimeInput(current) : defaultTime);
    setTimeError(false);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        setOpen(false);
        triggerRef.current?.focus();
      }
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  const chooseDate = (day: number) => {
    const date = new Date(viewDate.getFullYear(), viewDate.getMonth(), day);
    const time = isValidTime(timeDraft) ? timeDraft : defaultTime;
    const next = withLocalDateAndTime(date, time);
    if (next) {
      onChange(next);
      setTimeDraft(time);
      setTimeError(false);
    }
  };

  const onTimeChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const digits = event.target.value.replace(/\D/g, '').slice(0, 4);
    const nextDraft = digits.length > 2 ? `${digits.slice(0, 2)}:${digits.slice(2)}` : digits;
    setTimeDraft(nextDraft);
    if (!isValidTime(nextDraft)) {
      setTimeError(nextDraft.length > 0);
      return;
    }
    setTimeError(false);
    if (selectedDate) {
      const next = withLocalDateAndTime(selectedDate, nextDraft);
      if (next) onChange(next);
    }
  };

  const moveMonth = (offset: number) => {
    setViewDate((current) => new Date(current.getFullYear(), current.getMonth() + offset, 1));
  };

  return (
    <div ref={rootRef} className={`pl-date-picker${className ? ` ${className}` : ''}`}>
      <button
        ref={triggerRef}
        type="button"
        className={`pl-date-picker__trigger${!value ? ' pl-date-picker__trigger--placeholder' : ''}`}
        aria-label={t('datePicker.open', { label })}
        aria-haspopup="dialog"
        aria-expanded={open}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
      >
        <IconCalendar size={13} />
        <span className="pl-date-picker__value">{displayedValue}</span>
      </button>
      {open && (
        <div className="pl-date-picker__popover" role="dialog" aria-label={t('datePicker.dialog', { label })}>
          <div className="pl-date-picker__header">
            <strong>{monthLabel}</strong>
            <div className="pl-date-picker__nav">
              <button type="button" className="pl-icon-btn" aria-label={t('datePicker.previousMonth')} title={t('datePicker.previousMonth')} onClick={() => moveMonth(-1)}>
                <IconChevronLeft size={14} />
              </button>
              <button type="button" className="pl-icon-btn" aria-label={t('datePicker.nextMonth')} title={t('datePicker.nextMonth')} onClick={() => moveMonth(1)}>
                <IconChevronRight size={14} />
              </button>
            </div>
          </div>
          <div className="pl-date-picker__weekdays" aria-hidden="true">
            {weekdays.map((weekday) => <span key={weekday}>{weekday}</span>)}
          </div>
          <div className="pl-date-picker__grid" role="grid" aria-label={monthLabel}>
            {days.map((day, index) => {
              if (day === null) return <span key={`empty-${index}`} className="pl-date-picker__day pl-date-picker__day--empty" aria-hidden="true" />;
              const date = new Date(viewDate.getFullYear(), viewDate.getMonth(), day);
              const selected = sameCalendarDate(selectedDate, date);
              const today = sameCalendarDate(new Date(), date);
              const dateLabel = new Intl.DateTimeFormat(localeTag(locale), { dateStyle: 'full' }).format(date);
              return (
                <button
                  key={day}
                  type="button"
                  className={`pl-date-picker__day${selected ? ' pl-date-picker__day--selected' : ''}${today ? ' pl-date-picker__day--today' : ''}`}
                  role="gridcell"
                  aria-label={dateLabel}
                  aria-selected={selected}
                  onClick={() => chooseDate(day)}
                >
                  {day}
                </button>
              );
            })}
          </div>
          <div className="pl-date-picker__time">
            <label htmlFor={timeInputId}>
              <span>{t('datePicker.time')}</span>
              <span className="pl-date-picker__time-hint">{t('datePicker.timeHint')}</span>
            </label>
            <input
              id={timeInputId}
              className="pl-input pl-input--mono pl-date-picker__time-input"
              value={timeDraft}
              inputMode="numeric"
              maxLength={5}
              placeholder={t('datePicker.timePlaceholder')}
              aria-label={t('datePicker.time')}
              aria-invalid={timeError}
              onChange={onTimeChange}
            />
          </div>
          {timeError && <div className="pl-date-picker__error" role="alert">{t('datePicker.invalidTime')}</div>}
        </div>
      )}
    </div>
  );
};

/* ---------- States ---------- */

export const EmptyState: React.FC<{ title: string; body?: React.ReactNode; actions?: React.ReactNode }> = ({
  title,
  body,
  actions,
}) => (
  <div className="pl-state">
    <div className="pl-state__title">{title}</div>
    {body && <div className="pl-state__body">{body}</div>}
    {actions}
  </div>
);

export const ErrorState: React.FC<{
  title: string;
  body?: React.ReactNode;
  code?: string;
  actions?: React.ReactNode;
}> = ({ title, body, code, actions }) => (
  <div className="pl-state pl-state--error">
    <div className="pl-state__title">{title}</div>
    {body && <div className="pl-state__body">{body}</div>}
    {code && <span className="pl-state__code">{code}</span>}
    {actions}
  </div>
);

export const Skeleton: React.FC<{ width?: number | string; height?: number; style?: React.CSSProperties }> = ({
  width = '100%',
  height = 14,
  style,
}) => <div className="pl-skeleton" style={{ width, height, ...style }} />;

export const SkeletonRows: React.FC<{ rows?: number }> = ({ rows = 6 }) => (
  <div style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: '12px 8px' }}>
    {Array.from({ length: rows }).map((_, i) => (
      <Skeleton key={i} height={16} width={`${88 - (i % 3) * 9}%`} />
    ))}
  </div>
);

/* ---------- Key/value ---------- */

export const KeyValue: React.FC<{
  items: { key: string; value: React.ReactNode; mono?: boolean }[];
}> = ({ items }) => (
  <div className="pl-kv">
    {items.map((it) => (
      <React.Fragment key={it.key}>
        <span className="pl-kv__key">{it.key}</span>
        <span className={`pl-kv__value${it.mono ? ' pl-kv__value--mono' : ''}`}>{it.value ?? '-'}</span>
      </React.Fragment>
    ))}
  </div>
);

/* ---------- Copy icon button ---------- */

export const CopyButton: React.FC<{ text: string; title?: string }> = ({ text, title }) => {
  const { t } = useLocale();
  const [copied, setCopied] = React.useState(false);
  return (
    <button
      className="pl-icon-btn"
      title={copied ? t('common.copied') : title ?? t('common.copy')}
      aria-label={title ?? t('common.copy')}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text);
          setCopied(true);
          setTimeout(() => setCopied(false), 1200);
        } catch {}
      }}
    >
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
        <rect x="9" y="9" width="11" height="11" rx="1.5" />
        <path d="M5 15V5a1 1 0 0 1 1-1h9" />
      </svg>
    </button>
  );
};

/* ---------- Section ---------- */

export const Section: React.FC<{
  title: string;
  sub?: React.ReactNode;
  right?: React.ReactNode;
  children: React.ReactNode;
}> = ({ title, sub, right, children }) => (
  <section className="pl-section">
    <div style={{ display: 'flex', alignItems: 'baseline', gap: 'var(--pl-space-3)' }}>
      <h2 className="pl-section__title">{title}</h2>
      {right && <div style={{ marginLeft: 'auto', display: 'flex', gap: 'var(--pl-space-2)' }}>{right}</div>}
    </div>
    {sub && <div className="pl-section__sub">{sub}</div>}
    {children}
  </section>
);
