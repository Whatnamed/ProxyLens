import React, {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState
} from 'react';
import { getQuickWindow, QuickWindowType } from '../utils/time';
import { RouteKey } from '../lib/semantics';

/* ==========================================================================
 * Model
 * ========================================================================== */

export type SurfaceKey = 'overview' | 'history' | 'coverage';

export type WindowKind = QuickWindowType | 'custom';

export interface ResolvedRange {
  kind: WindowKind;
  /** UTC ISO — inclusive lower bound of the `[from, to)` window. */
  from: string;
  /** UTC ISO — exclusive upper bound. This is the frozen snapshot bound. */
  to: string;
  /** Epoch ms at which this window was resolved. */
  resolvedAt: number;
}

export interface HistoryFilters {
  process?: string;
  host?: string;
  destinationIp?: string;
  network?: string;
}

export type FilterKey = keyof HistoryFilters;

export interface ConnectionRef {
  sessionId: string;
  epochId: number;
  connectionId: string;
}

export const PAGE_SIZES = [50, 100, 200] as const;
export type PageSize = (typeof PAGE_SIZES)[number];

/**
 * A drill-down whose filter the Query API cannot express.
 * We record it and tell the user, instead of silently dropping the intent or
 * faking a filter the backend does not support.
 */
export interface DrillLimitations {
  /** Dimensions that were requested but cannot be applied. */
  unsupported: string[];
  /** What the backend query would need to support. */
  detail: string;
}

interface AuditContextValue {
  surface: SurfaceKey;
  navigate: (surface: SurfaceKey) => void;

  range: ResolvedRange;
  setWindow: (kind: WindowKind, custom?: { from: string; to: string }) => void;
  refresh: () => void;

  route: RouteKey;
  setRoute: (route: RouteKey) => void;

  filters: HistoryFilters;
  activeFilterCount: number;
  setFilter: (key: FilterKey, value: string) => void;
  removeFilter: (key: FilterKey) => void;
  clearFilters: () => void;

  page: number;
  pageSize: PageSize;
  setPage: (page: number) => void;
  setPageSize: (size: PageSize) => void;

  selection: ConnectionRef | null;
  selectConnection: (ref: ConnectionRef) => void;
  closeInspector: () => void;

  limitations: DrillLimitations | null;
  setLimitations: (l: DrillLimitations | null) => void;

  /** Bumped on manual refresh to force a refetch of an identical range. */
  nonce: number;

  /** Enter History preserving time range and route focus. */
  drillToHistory: (filter?: Partial<HistoryFilters>, limitations?: DrillLimitations | null) => void;
}

const AuditContext = createContext<AuditContextValue | null>(null);

/* ==========================================================================
 * Window resolution
 * ========================================================================== */

function resolveWindow(
  kind: WindowKind,
  custom?: { from: string; to: string }
): ResolvedRange {
  const resolvedAt = Date.now();
  if (kind === 'custom' && custom) {
    return { kind, from: custom.from, to: custom.to, resolvedAt };
  }
  // Quick windows are local-day aware and half-open; `to` is frozen to the
  // current moment at resolution time, which is what makes the History
  // snapshot stable while new data keeps arriving.
  const w = getQuickWindow((kind === 'custom' ? 'today' : kind) as QuickWindowType, new Date(resolvedAt));
  return { kind, from: w.from, to: w.to, resolvedAt };
}

/* ==========================================================================
 * Provider
 * ========================================================================== */

export const AuditContextProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [surface, setSurface] = useState<SurfaceKey>('overview');
  const [range, setRange] = useState<ResolvedRange>(() => resolveWindow('today'));
  const [route, setRouteState] = useState<RouteKey>('PROXY');
  const [filters, setFilters] = useState<HistoryFilters>({});
  const [page, setPage] = useState(0);
  const [pageSize, setPageSizeState] = useState<PageSize>(50);
  const [selection, setSelection] = useState<ConnectionRef | null>(null);
  const [limitations, setLimitations] = useState<DrillLimitations | null>(null);
  const [nonce, setNonce] = useState(0);

  const setWindow = useCallback((kind: WindowKind, custom?: { from: string; to: string }) => {
    setRange(resolveWindow(kind, custom));
    setPage(0);
    setSelection(null);
    setLimitations(null);
    setNonce((n) => n + 1);
  }, []);

  /**
   * Advances the snapshot to the current time.
   * Frozen rule: refresh returns the investigation to the first page.
   */
  const refresh = useCallback(() => {
    setRange((prev) => {
      // Yesterday and custom ranges are already closed; re-resolving them would
      // not change anything, so only live shortcuts advance their upper bound.
      if (prev.kind === 'yesterday' || prev.kind === 'custom') {
        return { ...prev, resolvedAt: Date.now() };
      }
      return resolveWindow(prev.kind);
    });
    setPage(0);
    setNonce((n) => n + 1);
  }, []);

  const setRoute = useCallback((r: RouteKey) => {
    setRouteState(r);
    setPage(0);
    setSelection(null);
  }, []);

  const setFilter = useCallback((key: FilterKey, value: string) => {
    const trimmed = value.trim();
    setFilters((prev) => {
      const next = { ...prev };
      if (trimmed === '') delete next[key];
      else next[key] = trimmed;
      return next;
    });
    setPage(0);
    setSelection(null);
  }, []);

  const removeFilter = useCallback((key: FilterKey) => {
    setFilters((prev) => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
    setPage(0);
    setSelection(null);
  }, []);

  const clearFilters = useCallback(() => {
    setFilters({});
    setPage(0);
    setSelection(null);
    setLimitations(null);
  }, []);

  const setPageSize = useCallback((size: PageSize) => {
    setPageSizeState(size);
    setPage(0);
  }, []);

  const selectConnection = useCallback((ref: ConnectionRef) => setSelection(ref), []);
  const closeInspector = useCallback(() => setSelection(null), []);

  const navigate = useCallback((s: SurfaceKey) => setSurface(s), []);

  /**
   * Overview -> History drill-down.
   * Time range and route focus are carried over unchanged; only the supplied
   * dimension filter is applied on top.
   */
  const drillToHistory = useCallback(
    (filter?: Partial<HistoryFilters>, limitationsOverride: DrillLimitations | null = null) => {
      setSurface('history');
      setPage(0);
      setSelection(null);
      setLimitations(limitationsOverride);
      if (filter) {
        setFilters((prev) => {
          const next = { ...prev };
          (Object.keys(filter) as FilterKey[]).forEach((k) => {
            const v = filter[k];
            if (v === undefined) return;
            if (v === '') delete next[k];
            else next[k] = v;
          });
          return next;
        });
      }
    },
    []
  );

  const activeFilterCount = useMemo(
    () => Object.values(filters).filter((v) => v && v.trim() !== '').length,
    [filters]
  );

  const value = useMemo<AuditContextValue>(
    () => ({
      surface,
      navigate,
      range,
      setWindow,
      refresh,
      route,
      setRoute,
      filters,
      activeFilterCount,
      setFilter,
      removeFilter,
      clearFilters,
      page,
      pageSize,
      setPage,
      setPageSize,
      selection,
      selectConnection,
      closeInspector,
      limitations,
      setLimitations,
      nonce,
      drillToHistory
    }),
    [
      surface,
      navigate,
      range,
      setWindow,
      refresh,
      route,
      setRoute,
      filters,
      activeFilterCount,
      setFilter,
      removeFilter,
      clearFilters,
      page,
      pageSize,
      setPage,
      setPageSize,
      selection,
      selectConnection,
      closeInspector,
      limitations,
      nonce,
      drillToHistory
    ]
  );

  return <AuditContext.Provider value={value}>{children}</AuditContext.Provider>;
};

export function useAudit(): AuditContextValue {
  const ctx = useContext(AuditContext);
  if (!ctx) throw new Error('useAudit must be used inside AuditContextProvider');
  return ctx;
}
