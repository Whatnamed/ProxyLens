/**
 * Proxy chain hop ordering.
 *
 * ARCHITECTURE.md 4.4 fixes the semantics of the `chains` array as delivered by Mihomo:
 *   chains[0]                 -> final physical egress node (or DIRECT)
 *   chains[1 .. length-2]     -> intermediate policy selectors
 *   chains[length-1]          -> the top-level policy group the rule matched
 *
 * The UI must therefore render the array REVERSED so the reading order matches the
 * causal order: rule target group -> intermediate selectors -> physical egress node.
 * Rendering it unreversed would tell the causality story backwards.
 */

/** Final physical egress node as recorded at the time the connection existed. */
export function egressNode(chains: string[] | undefined | null): string | null {
  if (!chains || chains.length === 0) return null;
  return chains[0] ?? null;
}

/** The top-level policy group that the matched rule pointed at. */
export function topPolicyGroup(chains: string[] | undefined | null): string | null {
  if (!chains || chains.length === 0) return null;
  return chains[chains.length - 1] ?? null;
}

/** Intermediate hops between the rule target and the egress node. */
export function intermediateHops(chains: string[] | undefined | null): string[] {
  if (!chains || chains.length < 3) return [];
  return chains.slice(1, chains.length - 1).filter(Boolean);
}

/**
 * Chains in causal (human reading) order: rule target -> ... -> egress node.
 * This is the only ordering that should ever be painted on screen.
 */
export function causalChain(chains: string[] | undefined | null): string[] {
  if (!chains || chains.length === 0) return [];
  return [...chains].reverse().filter(Boolean);
}

/** True when the chain carries no routing evidence at all. */
export function isMissingChain(chains: string[] | undefined | null): boolean {
  return !chains || chains.length === 0;
}
