import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { AuditFinding } from '../../api/types.js';
import { findingIsEstimated, findingKindKey, findingTargetLabel, historyFiltersForFinding } from './reviewSemantics.js';

const base: AuditFinding = {
  id: 'finding_test',
  kind: 'match_fallback_proxy',
  subject: { process: 'app.exe', targetKind: 'sniff_host', sniffHost: 'cdn.example' },
  evidence: {
    route: 'PROXY',
    rule: 'MATCH',
    network: 'tcp',
    uploadBytes: 4,
    downloadBytes: 8,
    totalBytes: 12,
    connectionCount: 1,
    exactUploadBytes: 4,
    exactDownloadBytes: 8,
    estimatedUploadBytes: 0,
    estimatedDownloadBytes: 0,
  },
};

describe('Review finding semantics', () => {
  it('maps an evidence-native finding to the supported History filters', () => {
    assert.deepEqual(historyFiltersForFinding(base), {
      process: 'app.exe',
      host: 'cdn.example',
      network: 'tcp',
      rule: 'MATCH',
    });
    assert.equal(findingTargetLabel(base), 'cdn.example');
    assert.equal(findingIsEstimated(base), false);
  });

  it('uses destination IP without inventing a host filter', () => {
    const finding: AuditFinding = {
      ...base,
      kind: 'ip_only_proxy_target',
      subject: { process: 'app.exe', targetKind: 'destination_ip', destinationIp: '192.0.2.7' },
      evidence: { ...base.evidence, estimatedUploadBytes: 3 },
    };
    assert.deepEqual(historyFiltersForFinding(finding), {
      process: 'app.exe',
      destinationIp: '192.0.2.7',
      network: 'tcp',
      rule: 'MATCH',
    });
    assert.equal(findingIsEstimated(finding), true);
    assert.equal(findingKindKey(finding.kind), 'review.kindIp');
  });
});
