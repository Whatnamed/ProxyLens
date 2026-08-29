/**
 * The Query API has shipped `qualityFlags` both as a string list and as a
 * boolean flag map (`{"missingProcess": false, ...}`). Rendering must never
 * depend on which shape arrives.
 */
export type QualityFlagsWire = string[] | Record<string, boolean> | null | undefined;

export function qualityFlagLabels(flags: QualityFlagsWire): string[] {
  if (Array.isArray(flags)) {
    return flags.filter((f) => typeof f === 'string' && f.length > 0);
  }
  if (flags && typeof flags === 'object') {
    return Object.entries(flags)
      .filter(([, v]) => v === true)
      .map(([k]) => k);
  }
  return [];
}
