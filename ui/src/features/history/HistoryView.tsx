import React, { useCallback, useEffect, useRef, useState } from 'react';
import { QueryApiClient } from '../../api/client';
import { useHistoryPage, useCoverage } from '../../api/queries';
import { useAudit, PAGE_SIZES, PageSize } from '../../state/AuditContext';
import { ConnectionRecord } from '../../api/types';
import { toProductError } from '../../lib/apiError';
import { Segmented } from '../../components/ui/primitives';
import { ErrorState, EmptyState, Notice, SkeletonRows } from '../../components/ui/states';
import { IconChevronLeft, IconChevronRight, IconInfo } from '../../components/ui/icons';
import { HistoryFilterBar } from './HistoryFilterBar';
import { HistoryTable } from './HistoryTable';
import { ConnectionInspector } from './ConnectionInspector';

/**
 * History is the authoritative evidence surface.
 *
 * Ordering stays authoritative (newest first) and pagination stays explicit:
 * offset-based paging with `hasMore`, no fabricated totals, no client-side
 * global sorting, no infinite scroll that would destroy the investigator's
 * position.
 */

interface Props {
  client: QueryApiClient | null;
}

const SPLIT_MIN_WIDTH = 1180;
const INSPECTOR_MIN = 380;
const INSPECTOR_MAX = 820;

/** Window width drives split-pane vs overlay presentation. */
function useWindowWidth(): number {
  const [w, setW] = useState(() => (typeof window === 'undefined' ? 1440 : window.innerWidth));
  useEffect(() => {
    const onResize = () => setW(window.innerWidth);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);
  return w;
}

export const HistoryView: React.FC<Props> = ({ client }) => {
  const {
    range,
    route,
    nonce,
    filters,
    activeFilterCount,
    clearFilters,
    setLimitations,
    limitations,
    page,
    setPage,
    pageSize,
    setPageSize,
    selection,
    selectConnection,
    closeInspector
  } = useAudit();

  const width = useWindowWidth();
  const [inspectorW, setInspectorW] = useState(480);
  const draggingRef = useRef(false);

  const query = useHistoryPage(client, range, route, filters, page, pageSize, nonce);
  const coverage = useCoverage(client, range, nonce);

  const items = query.data?.items ?? [];
  const hasMore = query.data?.hasMore ?? false;
  const offset = page * pageSize;

  const selectedIndex = selection
    ? items.findIndex(
        (c) =>
          c.sessionId === selection.sessionId &&
          c.epochId === selection.epochId &&
          c.connectionId === selection.connectionId
      )
    : -1;

  const useOverlay = width < SPLIT_MIN_WIDTH;
  const inspectorOpen = !!selection;

  /* ---- Inspector resize ---- */
  const onResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    draggingRef.current = true;
    const move = (ev: MouseEvent) => {
      if (!draggingRef.current) return;
      const next = Math.max(INSPECTOR_MIN, Math.min(INSPECTOR_MAX, window.innerWidth - ev.clientX));
      setInspectorW(next);
    };
    const up = () => {
      draggingRef.current = false;
      document.removeEventListener('mousemove', move);
      document.removeEventListener('mouseup', up);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
    document.addEventListener('mousemove', move);
    document.addEventListener('mouseup', up);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  }, []);

  const onSelect = (c: ConnectionRecord) =>
    selectConnection({ sessionId: c.sessionId, epochId: c.epochId, connectionId: c.connectionId });

  const step = (delta: number) => {
    if (selectedIndex < 0) return;
    const next = items[selectedIndex + delta];
    if (next) onSelect(next);
  };

  /* ---- Render ---- */

  const inspector = selection ? (
    <ConnectionInspector
      client={client}
      ref={selection}
      onClose={closeInspector}
      onStep={step}
      hasPrev={selectedIndex > 0}
      hasNext={selectedIndex >= 0 && selectedIndex < items.length - 1}
      position={
        selectedIndex >= 0
          ? `${offset + selectedIndex + 1} of ${offset + items.length}${hasMore ? '+' : ''}`
          : 'outside current page'
      }
    />
  ) : null;

  const tableArea = (
    <div className="history-main">
      <HistoryFilterBar />

      {limitations && (
        <div style={{ padding: 'var(--sp-4) var(--sp-6) 0' }}>
          <Notice tone="info">
            <strong>{limitations.unsupported.join(', ')} filter not applied.</strong>{' '}
            {limitations.detail}
            <button
              className="btn btn--sm btn--ghost"
              style={{ marginLeft: 'var(--sp-4)' }}
              onClick={() => setLimitations(null)}
            >
              Dismiss
            </button>
          </Notice>
        </div>
      )}

      {query.isError ? (
        <div className="htable-state">
          <div style={{ width: '100%', maxWidth: 560 }}>
            <ErrorState error={toProductError(query.error)} onRetry={() => query.refetch()} />
          </div>
        </div>
      ) : query.isLoading && items.length === 0 ? (
        <div style={{ padding: 'var(--sp-6)' }}>
          <SkeletonRows rows={12} />
        </div>
      ) : items.length === 0 ? (
        <EmptyState
          glyph={activeFilterCount > 0 ? 'search' : 'empty'}
          title={
            activeFilterCount > 0
              ? 'No connections match these filters'
              : coverage.data && !coverage.data.knownScopeStart
                ? 'No monitoring data exists yet'
                : 'No connections in this window'
          }
          desc={
            activeFilterCount > 0 ? (
              <>
                The current time range and route focus do contain traffic, but no connection
                satisfies all {activeFilterCount} filter(s). Widen a filter or clear them to see the
                underlying evidence.
              </>
            ) : coverage.data && !coverage.data.knownScopeStart ? (
              <>
                ProxyLens has never recorded a Collector session, so there is no connection
                evidence to show at any time range. This is an empty database, not a failed query.
              </>
            ) : (
              <>
                No connections were observed in this window with the current route focus. This may
                be a genuinely quiet period, or the window may fall inside a monitoring gap — check
                Coverage before concluding there was no traffic.
              </>
            )
          }
          actions={
            activeFilterCount > 0 ? (
              <button className="btn btn--sm" onClick={clearFilters}>
                Clear filters
              </button>
            ) : null
          }
        />
      ) : (
        <HistoryTable
          items={items}
          isLoading={query.isFetching}
          selected={selection}
          onSelect={onSelect}
          referenceTime={range.to}
        />
      )}

      <div className="history-footer">
        <div className="pager">
          <button
            className="btn btn--sm btn--icon"
            onClick={() => setPage(Math.max(0, page - 1))}
            disabled={page === 0}
            aria-label="Previous page"
            title="Previous page"
          >
            <IconChevronLeft size={12} />
          </button>
          <span className="pager-position">
            {items.length > 0 ? `${offset + 1}–${offset + items.length}` : '0'}
          </span>
          <button
            className="btn btn--sm btn--icon"
            onClick={() => setPage(page + 1)}
            disabled={!hasMore}
            aria-label="Next page"
            title={hasMore ? 'Next page' : 'No further records in this snapshot'}
          >
            <IconChevronRight size={12} />
          </button>
        </div>

        <span className="pager-note">
          Page {page + 1}
          {hasMore ? '' : ' · end of snapshot'}
          {hasMore ? ' · more records exist' : ''}
        </span>

        <div className="history-toolbar-spacer" />

        <span className="contextbar-label">Rows</span>
        <Segmented
          value={String(pageSize)}
          options={PAGE_SIZES.map((s) => ({ value: String(s), label: String(s) }))}
          onChange={(v) => setPageSize(Number(v) as PageSize)}
          ariaLabel="Rows per page"
        />

        <div className="contextbar-divider" />

        <span className="pager-note">
          <IconInfo size={11} />{' '}
          <span style={{ verticalAlign: 'middle' }}>
            Newest first — authoritative backend ordering. Total count is not provided by Query API
            v1.
          </span>
        </span>
      </div>
    </div>
  );

  if (!inspectorOpen) {
    return (
      <div className="history">
        <div className="history-main">{tableArea}</div>
      </div>
    );
  }

  if (useOverlay) {
    return (
      <div className="history">
        <div className="history-main">{tableArea}</div>
        <div className="inspector-overlay" onClick={closeInspector}>
          <div onClick={(e) => e.stopPropagation()}>{inspector}</div>
        </div>
      </div>
    );
  }

  return (
    <div className="history is-split" style={{ ['--inspector-w' as string]: `${inspectorW}px` }}>
      <div className="history-main">{tableArea}</div>
      <div className="history-inspector-cell">
        <div className="inspector-resizer" onMouseDown={onResizeStart} role="separator" aria-orientation="vertical" />
        {inspector}
      </div>
    </div>
  );
};
