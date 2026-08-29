/**
 * Monitoring-gap provenance classification, mirroring the Go Coverage
 * backend semantics: only `controller_stream` gaps are controller gaps;
 * every other source (`collector_session_boundary`,
 * `collector_runtime_liveness`, ...) is collector-side. Merged gaps can
 * carry multiple sources, so classification must tolerate mixed provenance.
 */
export const CONTROLLER_GAP_SOURCE = 'controller_stream';

export type GapProvenanceKind = 'controller' | 'collector' | 'mixed';

export interface GapProvenance {
  kind: GapProvenanceKind;
  label: string;
}

export function classifyGapSources(sources: Array<string | null | undefined> | null | undefined): GapProvenance {
  const set = new Set((sources ?? []).filter((s): s is string => typeof s === 'string' && s.length > 0));
  const hasController = set.has(CONTROLLER_GAP_SOURCE);
  const collectorSources = [...set].filter((s) => s !== CONTROLLER_GAP_SOURCE);
  const hasCollector = collectorSources.length > 0;

  if (hasController && hasCollector) {
    return { kind: 'mixed', label: 'Mixed provenance (controller + collector)' };
  }
  if (hasController) {
    return { kind: 'controller', label: 'Controller gap' };
  }
  // Unknown/collector-side sources all count as collector offline, matching
  // the backend accounting split (controller_stream vs everything else).
  return { kind: 'collector', label: 'Collector offline' };
}
