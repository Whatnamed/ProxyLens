import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { RouteScope } from '../domain/route';
import {
  QuickWindowType,
  QUICK_WINDOW_LABELS,
  TimeRangeRFC3339,
  getCustomWindow,
  getQuickWindow,
} from '../utils/time';

export type SurfaceId = 'overview' | 'attribution' | 'rules' | 'history' | 'coverage' | 'system';

export type WindowKind = QuickWindowType | 'custom';

/**
 * A drill filter is how the product moves from an aggregate to evidence.
 * Clicking any ranked row narrows the connection ledger to exactly the connections
 * that produced that row, so every number stays traceable back to its source records.
 */
export interface DrillFilter {
  process?: string;
  host?: string;
  destinationIp?: string;
  network?: string;
}

interface AppFiltersValue {
  surface: SurfaceId;
  setSurface: (s: SurfaceId) => void;

  windowKind: WindowKind;
  setWindowKind: (k: WindowKind) => void;
  windowLabel: string;

  customFrom: string;
  customTo: string;
  setCustomRange: (from: string, to: string) => void;
  customRangeValid: boolean;

  /**
   * The resolved UTC RFC3339 window, or null when a custom range is incomplete.
   * Named `timeWindow` rather than `window` so it can never shadow the DOM global.
   */
  timeWindow: TimeRangeRFC3339 | null;

  routeScope: RouteScope;
  setRouteScope: (r: RouteScope) => void;

  drill: DrillFilter;
  clearDrill: () => void;

  /** Jump to the connection ledger narrowed to one dimension value. */
  drillToHistory: (filter: DrillFilter) => void;

  liveMode: boolean;
  setLiveMode: (v: boolean) => void;

  /** Monotonic token bumped on every manual refresh; part of every query key. */
  refreshToken: number;
  refresh: () => void;
}

const AppFiltersContext = createContext<AppFiltersValue | null>(null);

export function AppFiltersProvider({ children }: { children: React.ReactNode }) {
  const [surface, setSurface] = useState<SurfaceId>('overview');
  const [windowKind, setWindowKind] = useState<WindowKind>('today');
  const [customFrom, setCustomFrom] = useState('');
  const [customTo, setCustomTo] = useState('');
  const [routeScope, setRouteScope] = useState<RouteScope>('PROXY');
  const [drill, setDrill] = useState<DrillFilter>({});
  const [liveMode, setLiveMode] = useState(true);
  const [refreshToken, setRefreshToken] = useState(0);

  // The "today"/"7d" windows end at "now", so they have to slide. Ticking once a minute
  // keeps the window honest without rebuilding query keys on every render.
  const [nowTick, setNowTick] = useState(0);
  useEffect(() => {
    const id = window.setInterval(() => setNowTick((n) => n + 1), 60_000);
    return () => window.clearInterval(id);
  }, []);

  const customWindow = useMemo(
    () => (windowKind === 'custom' ? getCustomWindow(customFrom, customTo) : null),
    [windowKind, customFrom, customTo]
  );
  const customRangeValid = windowKind !== 'custom' || customWindow !== null;

  const timeWindow = useMemo<TimeRangeRFC3339 | null>(() => {
    if (windowKind === 'custom') return customWindow;
    void nowTick; // re-evaluate "now" on each tick
    return getQuickWindow(windowKind);
  }, [windowKind, customWindow, nowTick, refreshToken]);

  const windowLabel = useMemo(() => {
    if (windowKind === 'custom') {
      if (!customWindow) return '自定义区间（不完整）';
      return `自定义 ${customFrom.replace('T', ' ')} → ${customTo.replace('T', ' ')}`;
    }
    return QUICK_WINDOW_LABELS[windowKind];
  }, [windowKind, customWindow, customFrom, customTo]);

  const setCustomRange = useCallback((from: string, to: string) => {
    setCustomFrom(from);
    setCustomTo(to);
  }, []);

  const clearDrill = useCallback(() => setDrill({}), []);

  const drillToHistory = useCallback((filter: DrillFilter) => {
    setDrill(filter);
    setSurface('history');
  }, []);

  const refresh = useCallback(() => setRefreshToken((n) => n + 1), []);

  const value = useMemo<AppFiltersValue>(
    () => ({
      surface,
      setSurface,
      windowKind,
      setWindowKind,
      windowLabel,
      customFrom,
      customTo,
      setCustomRange,
      customRangeValid,
      timeWindow,
      routeScope,
      setRouteScope,
      drill,
      clearDrill,
      drillToHistory,
      liveMode,
      setLiveMode,
      refreshToken,
      refresh,
    }),
    [
      surface,
      windowKind,
      windowLabel,
      customFrom,
      customTo,
      setCustomRange,
      customRangeValid,
      timeWindow,
      routeScope,
      drill,
      clearDrill,
      drillToHistory,
      liveMode,
      refreshToken,
      refresh,
    ]
  );

  return <AppFiltersContext.Provider value={value}>{children}</AppFiltersContext.Provider>;
}

export function useAppFilters(): AppFiltersValue {
  const ctx = useContext(AppFiltersContext);
  if (!ctx) throw new Error('useAppFilters must be used within AppFiltersProvider');
  return ctx;
}
