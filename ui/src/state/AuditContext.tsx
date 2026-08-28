import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react';
import { getQuickWindow, QuickWindowType } from '../utils/time';

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

interface TimeRangeState {
  kind: WindowKind;
  customFrom?: string;
  customTo?: string;
}

function rangeSourceKey(t: TimeRangeState): string {
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
}

export type ThemeName = 'light' | 'dark';

interface AuditContextValue {
  view: ViewName;
  setView: (v: ViewName) => void;

  timeRange: TimeRangeState;
  resolvedRange: ResolvedRange;
  setQuickWindow: (k: QuickWindowType) => void;
  setCustomRange: (fromLocal: string, toLocal: string) => void;

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
}

const AuditContext = createContext<AuditContextValue | null>(null);

const GAP_CONTEXT_MS = 15 * 60 * 1000;

export const AuditProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [view, setViewRaw] = useState<ViewName>('overview');
  const [timeRange, setTimeRange] = useState<TimeRangeState>({ kind: 'today' });
  const [routeFocus, setRouteFocus] = useState<RouteFocus>('PROXY');
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

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    try {
      localStorage.setItem('pl-theme', theme);
    } catch {}
  }, [theme]);

  const resolvedRange = useMemo(() => resolveTimeRange(timeRange), [timeRange]);

  const freezeSnapshot = useCallback((t: TimeRangeState) => {
    const r = resolveTimeRange(t);
    setSnapshot({ from: r.from, to: r.to, frozenAt: Date.now(), sourceKey: rangeSourceKey(t) });
    setPage(0);
    setSelected(null);
  }, []);

  const setView = useCallback(
    (v: ViewName) => {
      setViewRaw(v);
      if (v === 'history') {
        const key = rangeSourceKey(timeRange);
        if (!snapshot || snapshot.sourceKey !== key) {
          freezeSnapshot(timeRange);
        }
      }
    },
    [timeRange, snapshot, freezeSnapshot]
  );

  const setQuickWindow = useCallback((k: QuickWindowType) => {
    setTimeRange({ kind: k });
  }, []);

  const setCustomRange = useCallback((fromLocal: string, toLocal: string) => {
    setTimeRange({ kind: 'custom', customFrom: fromLocal, customTo: toLocal });
  }, []);

  const refreshHistory = useCallback(() => {
    freezeSnapshot(timeRange);
  }, [timeRange, freezeSnapshot]);

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
      setFilters((prev) => ({ ...prev, ...f }));
      setPage(0);
      setSelected(null);
      setView('history');
    },
    [setView]
  );

  const inspectAroundGap = useCallback(
    (startIso: string, endIso: string) => {
      const start = new Date(new Date(startIso).getTime() - GAP_CONTEXT_MS);
      const end = new Date(new Date(endIso).getTime() + GAP_CONTEXT_MS);
      const toLocalInput = (d: Date) => {
        const pad = (n: number) => String(n).padStart(2, '0');
        return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
      };
      setTimeRange({ kind: 'custom', customFrom: toLocalInput(start), customTo: toLocalInput(end) });
      setFilters({});
      setPage(0);
      setSelected(null);
      setViewRaw('history');
      const r = { from: start.toISOString(), to: end.toISOString() };
      setSnapshot({
        from: r.from,
        to: r.to,
        frozenAt: Date.now(),
        sourceKey: `custom|${toLocalInput(start)}|${toLocalInput(end)}`,
      });
    },
    []
  );

  const toggleTheme = useCallback(() => {
    setTheme((t) => (t === 'light' ? 'dark' : 'light'));
  }, []);

  const value: AuditContextValue = {
    view,
    setView,
    timeRange,
    resolvedRange,
    setQuickWindow,
    setCustomRange,
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
  };

  return <AuditContext.Provider value={value}>{children}</AuditContext.Provider>;
};

export function useAuditContext(): AuditContextValue {
  const ctx = useContext(AuditContext);
  if (!ctx) throw new Error('useAuditContext must be used within AuditProvider');
  return ctx;
}
