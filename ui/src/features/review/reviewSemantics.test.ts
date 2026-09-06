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

function finding(kind: AuditFinding['kind'], subject: AuditFinding['subject'], evidence = base.evidence): AuditFinding {
  return { ...base, kind, subject, evidence };
}

describe('Review finding semantics', () => {
  it('maps MATCH findings to process, target, and exact detector rule without network', () => {
    assert.deepEqual(historyFiltersForFinding(base), {
      process: 'app.exe',
      host: 'cdn.example',
      rule: 'MATCH',
    });
    assert.equal(findingTargetLabel(base), 'cdn.example');
    assert.equal(findingIsEstimated(base), false);
  });

  it('maps broad UDP findings to the detector rule and UDP network', () => {
    const udp = finding('broad_udp_proxy', { process: 'ntp.exe', targetKind: 'host', host: 'pool.example' }, {
      ...base.evidence,
      rule: 'NETWORK,udp',
      network: 'udp',
    });
    assert.deepEqual(historyFiltersForFinding(udp), {
      process: 'ntp.exe',
      host: 'pool.example',
      network: 'udp',
      rule: 'NETWORK,udp',
    });
  });

  it('maps IP-only findings to process and destination IP only', () => {
    const ipOnly = finding('ip_only_proxy_target', { process: 'app.exe', targetKind: 'destination_ip', destinationIp: '192.0.2.7' }, {
      ...base.evidence,
      rule: 'DomainSuffix',
      network: 'tcp',
      estimatedUploadBytes: 3,
    });
    assert.deepEqual(historyFiltersForFinding(ipOnly), {
      process: 'app.exe',
      destinationIp: '192.0.2.7',
    });
    assert.equal(findingIsEstimated(ipOnly), true);
    assert.equal(findingKindKey(ipOnly.kind), 'review.kindIp');
  });

  it('maps large physical findings to process and target without incidental metadata filters', () => {
    const large = finding('large_proxy_connection', {
      process: 'backup.exe',
      targetKind: 'host',
      host: 'archive.example',
      sessionId: 'session-a',
      epochId: 1,
      connectionId: 'connection-a',
    }, {
      ...base.evidence,
      rule: 'DomainSuffix',
      network: 'tcp',
      thresholdBytes: 100 * 1024 * 1024,
    });
    assert.deepEqual(historyFiltersForFinding(large), {
      process: 'backup.exe',
      host: 'archive.example',
    });
  });

  it('maps catalog findings to process and the canonical target without rule/network/path filters', () => {
    const knowledge = {
      catalogVersion: 'background-processes-v1',
      entryId: 'microsoft-defender-antivirus-service',
      category: 'security_service',
      publisher: 'Microsoft',
      family: 'Microsoft Defender Antivirus',
      matchBasis: 'process_name_and_path' as const,
      sources: [],
    };
    const cases: Array<[AuditFinding['subject'], Record<string, string>]> = [
      [{ process: 'MsMpEng.exe', targetKind: 'host', host: 'defender.example', processPath: 'C:\\synthetic\\MsMpEng.exe' }, { process: 'MsMpEng.exe', host: 'defender.example' }],
      [{ process: 'MsMpEng.exe', targetKind: 'sniff_host', sniffHost: 'sniff.example', processPath: 'C:\\synthetic\\MsMpEng.exe' }, { process: 'MsMpEng.exe', host: 'sniff.example' }],
      [{ process: 'MsMpEng.exe', targetKind: 'destination_ip', destinationIp: '192.0.2.9', processPath: 'C:\\synthetic\\MsMpEng.exe' }, { process: 'MsMpEng.exe', destinationIp: '192.0.2.9' }],
      [{ process: 'MsMpEng.exe', targetKind: 'missing', targetValue: '(missing target evidence)', processPath: 'C:\\synthetic\\MsMpEng.exe' }, { process: 'MsMpEng.exe' }],
    ];
    for (const [subject, expected] of cases) {
      const catalog = { ...finding('cataloged_background_process_proxy', subject, { ...base.evidence, rule: 'DomainSuffix', network: 'tcp' }), knowledge };
      assert.deepEqual(historyFiltersForFinding(catalog), expected);
      assert.equal(findingKindKey(catalog.kind), 'review.kindCatalog');
    }
  });
});
