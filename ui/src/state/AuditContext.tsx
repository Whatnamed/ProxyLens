import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react';
import { getQuickWindow, QuickWindowType } from '../utils/time';
import { DEFAULT_LOCALE, LOCALE_STORAGE_KEY, Locale, translate, TranslationVars } from '../i18n';

export type ViewName = 'overview' | 'history' | 'coverage';
export type RouteFocus = 'ALL' | 'PROXY' | 'DIRECT' | 'REJECT';
export type WindowKind = QuickWindowType | 'custom';

export interface HistoryFilters {
  process?: string;
  host?: string;
  destinationIp?: string;
  network?: string;
}

export interface ConnectionKey {
  sessionId: string;
  epochId: number;
  connectionId: string;
}

export interface ResolvedRange {
  from: string;
  to: string;
}

export interface TimeRangeState {
  kind: WindowKind;
  customFrom?: string;
  customTo?: string;
}

export function isLiveRangeKind(kind: WindowKind): boolean {
  return kind === 'today' || kind === '7d' || kind === '30d';
}

export function rangeSourceKey(t: TimeRangeState): string {
  return `${t.kind}|${t.customFrom ?? ''}|${t.customTo ?? ''}`;
}

export function resolveTimeRange(t: TimeRangeState, now: Date = new Date()): ResolvedRange {
  if (t.kind === 'custom') {
    if (t.customFrom && t.customTo) {
      const from = new Date(t.customFrom);
      const to = new Date(t.customTo);
      if (!isNaN(from.getTime()) && !isNaN(to.getTime())) {
        return { from: from.toISOString(), to: to.toISOString() };
      }
    }
    return getQuickWindow('7d', now);
  }
  return getQuickWindow(t.kind, now);
}

export interface HistorySnapshot {
  from: string;
  to: string;
  frozenAt: number;
  sourceKey: string;
  kind: WindowKind;
}

export function createHistorySnapshot(t: TimeRangeState, now: Date = new Date()): HistorySnapshot {
  const r = resolveTimeRange(t, now);
  return {
    from: r.from,
    to: r.to,
    frozenAt: now.getTime(),
    sourceKey: rangeSourceKey(t),
    kind: t.kind,
  };
}

export function refreshSnapshot(
  prev: HistorySnapshot,
  t: TimeRangeState,
  now: Date = new Date()
): HistorySnapshot {
  if (isLiveRangeKind(prev.kind)) {
    const r = resolveTimeRange(t, now);
    return {
      from: r.from,
      to: r.to,
      frozenAt: now.getTime(),
      sourceKey: rangeSourceKey(t),
      kind: prev.kind,
    };
  }
  return {
    ...prev,
    frozenAt: now.getTime(),
  };
}

export type ThemeName = 'light' | 'dark';

interface AuditContextValue {
  view: ViewName;
  setView: (v: ViewName) => void;

  timeRange: TimeRangeState;
  resolvedRange: ResolvedRange;
  setQuickWindow: (k: QuickWindowType) => void;
  setCustomRange: (fromLocal: string, toLocal: string) => void;
  /** Custom editor is open; the applied range only changes on Apply. */
  customEditorOpen: boolean;
  openCustomEditor: () => void;

  routeFocus: RouteFocus;
  setRouteFocus: (r: RouteFocus) => void;

  filters: HistoryFilters;
  setFilter: (key: keyof HistoryFilters, value: string) => void;
  clearFilters: () => void;

  page: number;
  setPage: (p: number) => void;
  pageSize: number;
  setPageSize: (s: number) => void;

  snapshot: HistorySnapshot | null;
  refreshHistory: () => void;

  selected: ConnectionKey | null;
  setSelected: (k: ConnectionKey | null) => void;

  drillToHistory: (filters: Partial<HistoryFilters>) => void;
  inspectAroundGap: (startIso: string, endIso: string) => void;

  theme: ThemeName;
  toggleTheme: () => void;

  locale: Locale;
  setLocale: (locale: Locale) => void;
  toggleLocale: () => void;
}

const AuditContext = createContext<AuditContextValue | null>(null);

const GAP_CONTEXT_MS = 15 * 60 * 1000;

export const AuditProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [view, setViewRaw] = useState<ViewName>('overview');
  const [timeRange, setTimeRange] = useState<TimeRangeState>({ kind: 'today' });
  const [liveNow, setLiveNow] = useState<Date>(() => new Date());
  const [customEditorOpen, setCustomEditorOpen] = useState(false);
  const [routeFocus, setRouteFocusRaw] = useState<RouteFocus>('PROXY');
  const [filters, setFilters] = useState<HistoryFilters>({});
  const [page, setPage] = useState(0);
  const [pageSize, setPageSizeRaw] = useState(50);
  const [snapshot, setSnapshot] = useState<HistorySnapshot | null>(null);
  const [selected, setSelected] = useState<ConnectionKey | null>(null);
  const [theme, setTheme] = useState<ThemeName>(() => {
    try {
      const saved = localStorage.getItem('pl-theme');
      if (saved === 'light' || saved === 'dark') return saved;
    } catch {}
    if (typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: dark)').matches) {
      return 'dark';
    }
    return 'light';
  });
  const [locale, setLocale] = useState<Locale>(() => {
    try {
      const saved = localStorage.getItem(LOCALE_STORAGE_KEY);
      if (saved === 'en' || saved === 'zh-CN') return saved;
    } catch {}
    return DEFAULT_LOCALE;
  });

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    try {
      localStorage.setItem('pl-theme', theme);
    } catch {}
  }, [theme]);

  useEffect(() => {
    document.documentElement.lang = locale;
    try {
      localStorage.setItem(LOCALE_STORAGE_KEY, locale);
    } catch {}
  }, [locale]);

  useEffect(() => {
    // Low-frequency tick (30s) to advance live analysis upper bounds (Overview, Coverage)
    const timer = setInterval(() => {
      if (isLiveRangeKind(timeRange.kind)) {
        setLiveNow(new Date());
      }
    }, 30000);
    return () => clearInterval(timer);
  }, [timeRange.kind]);

  // Live Analysis range: advances with liveNow for today / 7d / 30d; fixed for yesterday / custom
  const resolvedRange = useMemo(
    () => resolveTimeRange(timeRange, isLiveRangeKind(timeRange.kind) ? liveNow : undefined),
    [timeRange, liveNow]
  );

  const freezeSnapshot = useCallback((t: TimeRangeState, now = new Date()) => {
    setSnapshot(createHistorySnapshot(t, now));
    setPage(0);
    setSelected(null);
  }, []);

  /**
   * Single path for applied time-range changes.
   */
  const applyTimeRange = useCallback(
    (t: TimeRangeState) => {
      const now = new Date();
      setTimeRange(t);
      setLiveNow(now);
      setCustomEditorOpen(false);
      freezeSnapshot(t, now);
    },
    [freezeSnapshot]
  );

  const setView = useCallback(
    (v: ViewName) => {
      setViewRaw(v);
      if (v === 'history') {
        const key = rangeSourceKey(timeRange);
        if (!snapshot || snapshot.sourceKey !== key) {
          // Entering History for the first time or with a different time range: freeze snapshot now
          freezeSnapshot(timeRange, new Date());
        }
        // If returning to History and timeRange matches, keep existing snapshot and investigation!
      }
    },
    [timeRange, snapshot, freezeSnapshot]
  );

  const setQuickWindow = useCallback(
    (k: QuickWindowType) => {
      applyTimeRange({ kind: k });
    },
    [applyTimeRange]
  );

  const setCustomRange = useCallback(
    (fromLocal: string, toLocal: string) => {
      if (!fromLocal || !toLocal) return;
      applyTimeRange({ kind: 'custom', customFrom: fromLocal, customTo: toLocal });
    },
    [applyTimeRange]
  );

  const openCustomEditor = useCallback(() => {
    setCustomEditorOpen(true);
  }, []);

  const refreshHistory = useCallback(() => {
    if (!snapshot) return;
    const now = new Date();
    setSnapshot((prev) => (prev ? refreshSnapshot(prev, timeRange, now) : null));
    setPage(0);
    setSelected(null);
    // filters are preserved on refresh!
  }, [snapshot, timeRange]);

  const setRouteFocus = useCallback((r: RouteFocus) => {
    setRouteFocusRaw(r);
    setPage(0);
    setSelected(null);
  }, []);

  const setPageSize = useCallback((s: number) => {
    setPageSizeRaw(s);
    setPage(0);
    setSelected(null);
  }, []);

  const setFilter = useCallback((key: keyof HistoryFilters, value: string) => {
    setFilters((prev) => {
      const next = { ...prev };
      if (value === '') {
        delete next[key];
      } else {
        next[key] = value;
      }
      return next;
    });
    setPage(0);
    setSelected(null);
  }, []);

  const clearFilters = useCallback(() => {
    setFilters({});
    setPage(0);
    setSelected(null);
  }, []);

  const drillToHistory = useCallback(
    (f: Partial<HistoryFilters>) => {
      // New investigation: clear old unrelated filters!
      setFilters(f);
      setPage(0);
      setSelected(null);
      const now = new Date();
      setSnapshot(createHistorySnapshot(timeRange, now));
      setViewRaw('history');
    },
    [timeRange]
  );

  const inspectAroundGap = useCallback(
    (startIso: string, endIso: string) => {
      const start = new Date(new Date(startIso).getTime() - GAP_CONTEXT_MS);
      const end = new Date(new Date(endIso).getTime() + GAP_CONTEXT_MS);
      const toLocalInput = (d: Date) => {
        const pad = (n: number) => String(n).padStart(2, '0');
        return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
      };
      setFilters({});
      setPage(0);
      setSelected(null);
      const customState: TimeRangeState = {
        kind: 'custom',
        customFrom: toLocalInput(start),
        customTo: toLocalInput(end),
      };
      setTimeRange(customState);
      setCustomEditorOpen(false);
      setSnapshot(createHistorySnapshot(customState, new Date()));
      setViewRaw('history');
    },
    []
  );

  const toggleTheme = useCallback(() => {
    setTheme((t) => (t === 'light' ? 'dark' : 'light'));
  }, []);

  const toggleLocale = useCallback(() => {
    setLocale((current) => (current === 'en' ? 'zh-CN' : 'en'));
  }, []);

  const value: AuditContextValue = {
    view,
    setView,
    timeRange,
    resolvedRange,
    setQuickWindow,
    setCustomRange,
    customEditorOpen,
    openCustomEditor,
    routeFocus,
    setRouteFocus,
    filters,
    setFilter,
    clearFilters,
    page,
    setPage,
    pageSize,
    setPageSize,
    snapshot,
    refreshHistory,
    selected,
    setSelected,
    drillToHistory,
    inspectAroundGap,
    theme,
    toggleTheme,
    locale,
    setLocale,
    toggleLocale,
  };

  return <AuditContext.Provider value={value}>{children}</AuditContext.Provider>;
};

export function useAuditContext(): AuditContextValue {
  const ctx = useContext(AuditContext);
  if (!ctx) throw new Error('useAuditContext must be used within AuditProvider');
  return ctx;
}

export function useLocale(): {
  locale: Locale;
  t: (key: string, vars?: TranslationVars) => string;
} {
  const { locale } = useAuditContext();
  const t = useCallback((key: string, vars?: TranslationVars) => translate(locale, key, vars), [locale]);
  return { locale, t };
}
