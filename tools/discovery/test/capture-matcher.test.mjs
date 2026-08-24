/**
 * capture-matcher.test.mjs
 * 
 * Stage F3: Capture Rate Matcher & Route Verification Unit Tests
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { classifyRawRoute, analyzeCaptureSession } from '../analyze-capture-rate.mjs';

function createCaptureTestFixture(gtList, connFrames, manifestOptions = {}) {
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-capture-test-'));
  
  const gtPath = path.join(tmpDir, 'ground-truth.ndjson');
  fs.writeFileSync(gtPath, gtList.map(g => JSON.stringify(g)).join('\n'));

  const connPath = path.join(tmpDir, 'connections.ndjson');
  fs.writeFileSync(connPath, connFrames.map(f => JSON.stringify(f)).join('\n'));

  const manifestPath = path.join(tmpDir, 'manifest.json');
  fs.writeFileSync(manifestPath, JSON.stringify({
    probeVersion: '0.3.1-test',
    requestedConnectionsIntervalMs: 250,
    evidenceQuality: {
      isHealthySession: manifestOptions.isHealthySession ?? true,
      issues: manifestOptions.issues ?? []
    }
  }));

  return { tmpDir, gtPath };
}

test('F3-1: classifyRawRoute must correctly distinguish DIRECT vs PROXY vs REJECT vs UNKNOWN', () => {
  assert.equal(classifyRawRoute(['DIRECT']), 'DIRECT');
  assert.equal(classifyRawRoute(['🇭🇰 香港W06', 'Proxy-Group']), 'PROXY');
  assert.equal(classifyRawRoute(['REJECT']), 'REJECT');
  assert.equal(classifyRawRoute([]), 'UNKNOWN');
  assert.equal(classifyRawRoute(null), 'UNKNOWN');
});

test('F3-2: Window violation detection must flag requests after probe end', async () => {
  const gtList = [
    {
      eligibleConnected: true,
      success: true,
      localPort: 54321,
      targetUrl: 'https://example.com',
      requestedAt: '2026-08-21T00:00:00.500Z',
      completedAt: '2026-08-21T00:00:00.600Z'
    },
    {
      eligibleConnected: true,
      success: true,
      localPort: 54322,
      targetUrl: 'https://example.com',
      requestedAt: '2026-08-21T00:00:05.500Z', // 超出 probe 监控窗口 (2s 结束)
      completedAt: '2026-08-21T00:00:05.600Z'
    }
  ];

  const connFrames = [
    { receivedAt: '2026-08-21T00:00:00.000Z', frame: { connections: [] } },
    { receivedAt: '2026-08-21T00:00:02.000Z', frame: { connections: [] } }
  ];

  const { tmpDir, gtPath } = createCaptureTestFixture(gtList, connFrames);
  try {
    const res = await analyzeCaptureSession(tmpDir, gtPath, { expectedRoute: 'DIRECT' });
    assert.equal(res.windowViolations, 1, 'Must detect 1 request outside probe window');
    assert.equal(res.matched, 0);
    assert.equal(res.missed, 2);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test('F3-3: Route mismatch must be flagged when actual route differs from expectedRoute', async () => {
  const gtList = [
    {
      eligibleConnected: true,
      success: true,
      localPort: 54321,
      targetUrl: 'https://example.com',
      requestedAt: '2026-08-21T00:00:00.500Z',
      completedAt: '2026-08-21T00:00:00.600Z'
    }
  ];

  const connFrames = [
    { receivedAt: '2026-08-21T00:00:00.000Z', frame: { connections: [] } },
    {
      receivedAt: '2026-08-21T00:00:00.600Z',
      frame: {
        connections: [
          {
            id: 'conn-proxy-1',
            metadata: { process: 'node.exe', sourcePort: 54321, host: 'example.com' },
            chains: ['Node-A', 'Proxy-Group'], // 实际为 PROXY
            rule: 'Match'
          }
        ]
      }
    },
    { receivedAt: '2026-08-21T00:00:02.000Z', frame: { connections: [] } }
  ];

  // 期望是 DIRECT，但实际连接为 PROXY
  const { tmpDir, gtPath } = createCaptureTestFixture(gtList, connFrames);
  try {
    const res = await analyzeCaptureSession(tmpDir, gtPath, { expectedRoute: 'DIRECT' });
    assert.equal(res.matched, 1);
    assert.equal(res.routeVerification.routeMismatches, 1, 'Must flag route mismatch');
    assert.equal(res.routeVerification.matchedWithKnownRoute, 1);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test('F3-4: Route unknown must be flagged when chains are empty/unknown', async () => {
  const gtList = [
    {
      eligibleConnected: true,
      success: true,
      localPort: 54321,
      targetUrl: 'https://example.com',
      requestedAt: '2026-08-21T00:00:00.500Z',
      completedAt: '2026-08-21T00:00:00.600Z'
    }
  ];

  const connFrames = [
    { receivedAt: '2026-08-21T00:00:00.000Z', frame: { connections: [] } },
    {
      receivedAt: '2026-08-21T00:00:00.600Z',
      frame: {
        connections: [
          {
            id: 'conn-empty-chain-1',
            metadata: { process: 'node.exe', sourcePort: 54321, host: 'example.com' },
            chains: [], // 缺少 chains
            rule: ''
          }
        ]
      }
    },
    { receivedAt: '2026-08-21T00:00:02.000Z', frame: { connections: [] } }
  ];

  const { tmpDir, gtPath } = createCaptureTestFixture(gtList, connFrames);
  try {
    const res = await analyzeCaptureSession(tmpDir, gtPath, { expectedRoute: 'PROXY' });
    assert.equal(res.matched, 1);
    assert.equal(res.routeVerification.routeUnknown, 1, 'Must flag route unknown');
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test('F3-5: 1-to-1 conflict and ambiguous detection when single connection maps to multiple distinct GT requests', async () => {
  const gtList = [
    {
      eligibleConnected: true,
      success: true,
      localPort: 54321,
      targetUrl: 'https://example.com',
      requestedAt: '2026-08-21T00:00:00.500Z',
      completedAt: '2026-08-21T00:00:00.600Z'
    },
    {
      eligibleConnected: true,
      success: true,
      localPort: 54321, // 模拟冲突复用
      targetUrl: 'https://example.com',
      requestedAt: '2026-08-21T00:00:00.700Z',
      completedAt: '2026-08-21T00:00:00.800Z'
    }
  ];

  // 只有一条连接跨越了两个 GT 请求的时间
  const connFrames = [
    { receivedAt: '2026-08-21T00:00:00.000Z', frame: { connections: [] } },
    {
      receivedAt: '2026-08-21T00:00:00.600Z',
      frame: {
        connections: [
          {
            id: 'conn-shared-conflict',
            metadata: { process: 'node.exe', sourcePort: 54321, host: 'example.com' },
            chains: ['DIRECT'],
            rule: 'Match'
          }
        ]
      }
    },
    {
      receivedAt: '2026-08-21T00:00:00.750Z',
      frame: {
        connections: [
          {
            id: 'conn-shared-conflict',
            metadata: { process: 'node.exe', sourcePort: 54321, host: 'example.com' },
            chains: ['DIRECT'],
            rule: 'Match'
          }
        ]
      }
    },
    { receivedAt: '2026-08-21T00:00:02.000Z', frame: { connections: [] } }
  ];

  const { tmpDir, gtPath } = createCaptureTestFixture(gtList, connFrames);
  try {
    const res = await analyzeCaptureSession(tmpDir, gtPath, { expectedRoute: 'DIRECT' });
    // 第一个请求 matched，第二个请求发现该 ID 已被映射，标记为 AMBIGUOUS 并增加 oneToOneConflicts
    assert.equal(res.matched, 1);
    assert.equal(res.ambiguous, 1, 'Second request must be ambiguous');
    assert.equal(res.oneToOneConflicts, 1, 'Must detect 1 1-to-1 mapping conflict');
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});
