/**
 * Route semantics.
 *
 * PRODUCT.md 3.2: the system must clearly separate DIRECT / PROXY / REJECT and must never
 * treat everything passing through the TUN as "proxy traffic". The UI encodes this by making
 * PROXY the emphasised default and DIRECT visually quiet, rather than presenting all routes
 * as peers in a neutral palette.
 */

export type RouteScope = 'PROXY' | 'DIRECT' | 'REJECT' | 'ALL';

export type RouteKey = 'PROXY' | 'DIRECT' | 'REJECT' | 'UNKNOWN';

interface RouteSemantics {
  /** Short label used in badges and table cells. */
  label: string;
  /** Explains what this route means, used in tooltips and legend. */
  meaning: string;
  /** CSS custom property carrying the route colour. */
  colorVar: string;
  /** CSS custom property carrying a low-alpha tint of the route colour. */
  tintVar: string;
  /** Whether this route represents traffic that actually consumed a proxy node. */
  isProxyEgress: boolean;
}

const SEMANTICS: Record<RouteKey, RouteSemantics> = {
  PROXY: {
    label: 'PROXY',
    meaning: '经由代理节点出站。这部分才真正消耗节点流量。',
    colorVar: '--pl-route-proxy',
    tintVar: '--pl-route-proxy-dim',
    isProxyEgress: true,
  },
  DIRECT: {
    label: 'DIRECT',
    meaning: '绕过代理节点直接出站。绝不计入代理节点消耗。',
    colorVar: '--pl-route-direct',
    tintVar: '--pl-route-direct-dim',
    isProxyEgress: false,
  },
  REJECT: {
    label: 'REJECT',
    meaning: '在出站之前被规则拦截，未产生任何节点流量。',
    colorVar: '--pl-route-reject',
    tintVar: '--pl-route-reject-dim',
    isProxyEgress: false,
  },
  UNKNOWN: {
    label: 'UNKNOWN',
    meaning: '无法从观测到的代理链推导出站方式，保留为未归因流量。',
    colorVar: '--pl-route-unknown',
    tintVar: '--pl-route-unknown-dim',
    isProxyEgress: false,
  },
};

/** Normalise any backend route string into the closed set the UI renders. */
export function normalizeRoute(route: string | undefined | null): RouteKey {
  switch ((route ?? '').toUpperCase()) {
    case 'PROXY':
      return 'PROXY';
    case 'DIRECT':
      return 'DIRECT';
    case 'REJECT':
      return 'REJECT';
    default:
      return 'UNKNOWN';
  }
}

export function routeSemantics(route: string | undefined | null): RouteSemantics {
  return SEMANTICS[normalizeRoute(route)];
}

export function routeColor(route: string | undefined | null): string {
  return `var(${routeSemantics(route).colorVar})`;
}

export function routeTint(route: string | undefined | null): string {
  return `var(${routeSemantics(route).tintVar})`;
}

/** The route filter values accepted by the Query API. */
export const ROUTE_SCOPES: { value: RouteScope; label: string; hint: string }[] = [
  { value: 'PROXY', label: '代理', hint: '经由代理节点出站的流量（默认视角）' },
  { value: 'DIRECT', label: '直连', hint: '绕过代理节点直接出站的流量' },
  { value: 'REJECT', label: '拦截', hint: '在出站前被规则拦截的流量' },
  { value: 'ALL', label: '全部', hint: '不限出站方式的全部已观测连接' },
];

/** Byte totals for a given route, extracted from a UsageSummary. */
export function routeBytes(
  summary:
    | {
        proxyUpload?: number;
        proxyDownload?: number;
        directUpload?: number;
        directDownload?: number;
        rejectUpload?: number;
        rejectDownload?: number;
        unknownRouteUpload?: number;
        unknownRouteDownload?: number;
      }
    | null
    | undefined,
  route: RouteKey
): { upload: number; download: number; total: number } {
  const s = summary ?? {};
  const pick = (u?: number, d?: number) => ({
    upload: u ?? 0,
    download: d ?? 0,
    total: (u ?? 0) + (d ?? 0),
  });
  switch (route) {
    case 'PROXY':
      return pick(s.proxyUpload, s.proxyDownload);
    case 'DIRECT':
      return pick(s.directUpload, s.directDownload);
    case 'REJECT':
      return pick(s.rejectUpload, s.rejectDownload);
    default:
      return pick(s.unknownRouteUpload, s.unknownRouteDownload);
  }
}

/**
 * Ordered route segments for the composition bar.
 * UNKNOWN is included whenever non-zero, because hiding it would re-create the
 * "unknown black box" failure mode this product exists to eliminate.
 */
export function routeComposition(
  summary: Parameters<typeof routeBytes>[0]
): { route: RouteKey; total: number }[] {
  return (['PROXY', 'DIRECT', 'REJECT', 'UNKNOWN'] as RouteKey[])
    .map((route) => ({ route, total: routeBytes(summary, route).total }))
    .filter((seg) => seg.total > 0);
}
