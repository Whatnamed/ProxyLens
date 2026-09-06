import { AuditFinding, AuditFindingKind } from '../../api/types';
import { HistoryFilters } from '../../state/AuditContext';

export const AUDIT_FINDING_KINDS: AuditFindingKind[] = [
  'match_fallback_proxy',
  'broad_udp_proxy',
  'ip_only_proxy_target',
  'large_proxy_connection',
];

export function findingTargetLabel(finding: AuditFinding): string {
  return finding.subject.targetValue || finding.subject.destinationIp || finding.subject.host || finding.subject.sniffHost || '';
}

/**
 * Converts a finding into the narrow, read-only History filters that can
 * reproduce its evidence. No detector-specific state is persisted in UI.
 */
export function historyFiltersForFinding(finding: AuditFinding): Partial<HistoryFilters> {
  const filters: Partial<HistoryFilters> = {};
  const subject = finding.subject;
  const evidence = finding.evidence;
  if (subject.process) filters.process = subject.process;

  switch (subject.targetKind) {
    case 'destination_ip':
      if (subject.destinationIp) filters.destinationIp = subject.destinationIp;
      break;
    case 'host':
      if (subject.host) filters.host = subject.host;
      break;
    case 'sniff_host':
      if (subject.sniffHost) filters.host = subject.sniffHost;
      break;
  }

  if (evidence.network) filters.network = evidence.network;
  if (evidence.rule) filters.rule = evidence.rule;
  return filters;
}

export function findingIsEstimated(finding: AuditFinding): boolean {
  return finding.evidence.estimatedUploadBytes > 0 || finding.evidence.estimatedDownloadBytes > 0;
}

export function findingKindKey(kind: AuditFindingKind): string {
  switch (kind) {
    case 'match_fallback_proxy':
      return 'review.kindMatch';
    case 'broad_udp_proxy':
      return 'review.kindUdp';
    case 'ip_only_proxy_target':
      return 'review.kindIp';
    case 'large_proxy_connection':
      return 'review.kindLarge';
  }
}
