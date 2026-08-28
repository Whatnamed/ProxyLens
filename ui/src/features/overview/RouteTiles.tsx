import React from 'react';
import { UsageSummary } from '../../api/types';
import { RouteKey } from '../../lib/semantics';
import { ByteValue, Tip } from '../../components/ui/primitives';
import { formatBytes } from '../../utils/format';
import { SkeletonLine } from '../../components/ui/states';

/**
 * Traffic totals for the three routing classes.
 *
 * These tiles always come from the unfiltered (ALL) summary: the backend
 * skips non-matching rows when a route filter is applied, so a route-scoped
 * summary cannot show the other two classes. Clicking a tile sets Route Focus.
 */

interface Props {
  summary: UsageSummary | undefined;
  isLoading: boolean;
  route: RouteKey;
  onFocus: (route: RouteKey) => void;
}

interface TileSpec {
  key: Exclude<RouteKey, 'ALL'>;
  up: number;
  down: number;
  color: string;
}

export const RouteTiles: React.FC<Props> = ({ summary, isLoading, route, onFocus }) => {
  if (isLoading && !summary) {
    return (
      <div className="route-tiles">
        {[0, 1, 2].map((i) => (
          <div key={i} className="route-tile">
            <SkeletonLine width="50%" height={9} />
            <SkeletonLine width="70%" height={24} />
            <SkeletonLine width="90%" height={9} />
          </div>
        ))}
      </div>
    );
  }

  const tiles: TileSpec[] = [
    {
      key: 'PROXY',
      up: summary?.proxyUpload ?? 0,
      down: summary?.proxyDownload ?? 0,
      color: 'var(--route-proxy)'
    },
    {
      key: 'DIRECT',
      up: summary?.directUpload ?? 0,
      down: summary?.directDownload ?? 0,
      color: 'var(--route-direct)'
    },
    {
      key: 'REJECT',
      up: summary?.rejectUpload ?? 0,
      down: summary?.rejectDownload ?? 0,
      color: 'var(--route-reject)'
    }
  ];

  return (
    <div className="route-tiles">
      {tiles.map((t) => {
        const total = t.up + t.down;
        const focused = route === t.key;
        return (
          <button
            key={t.key}
            className={`route-tile ${focused ? 'is-focused' : ''}`}
            style={{ ['--focus-color' as string]: t.color }}
            onClick={() => onFocus(t.key)}
            title={`Set route focus to ${t.key}`}
            aria-pressed={focused}
          >
            <div className="route-tile-head">
              <span className="route-tile-dot" />
              <span className="route-tile-name">{t.key}</span>
              {focused && <span className="route-tile-focusmark">Focus</span>}
            </div>

            <div className="route-tile-total">{formatBytes(total)}</div>

            <div className="route-tile-split">
              <Tip
                content={
                  <>
                    <div className="tip-row">
                      <span className="tip-label">Exact upload</span>
                      <span>{t.up.toLocaleString('en-US')} B</span>
                    </div>
                  </>
                }
              >
                <span>
                  <span className="route-tile-arrow">↑</span> <ByteValue bytes={t.up} />
                </span>
              </Tip>
              <Tip
                content={
                  <>
                    <div className="tip-row">
                      <span className="tip-label">Exact download</span>
                      <span>{t.down.toLocaleString('en-US')} B</span>
                    </div>
                  </>
                }
              >
                <span>
                  <span className="route-tile-arrow">↓</span> <ByteValue bytes={t.down} />
                </span>
              </Tip>
            </div>
          </button>
        );
      })}
    </div>
  );
};
