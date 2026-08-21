/**
 * capture-matcher.test.mjs
 * 
 * Stage R7: Capture Rate Matcher & Route Verification Unit Tests
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { classifyRawRoute, analyzeCaptureSession } from '../analyze-capture-rate.mjs';

function createCaptureTestFixture(gtList, connFrames) {
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-capture-test-'));
  
  const gtPath = path.join(tmpDir, 'ground-truth.ndjson');
  fs.writeFileSync(gtPath, gtList.map(g => JSON.stringify(g)).join('\n'));

  const connPath = path.join(tmpDir, 'connections.ndjson');
  fs.writeFileSync(connPath, connFrames.map(f => JSON.stringify(f)).join('\n'));

  const manifestPath = path.join(tmpDir, 'manifest.json');
  fs.writeFileSync(manifestPath, JSON.stringify({
    probeVersion: '0.3.1-test',
    requestedConnectionsIntervalMs: 250
  }));

  return { tmpDir, gtPath };
}

test('Matcher: classifyRawRoute must correctly distinguish DIRECT vs PROXY vs REJECT', () => {
  assert.equal(classifyRawRoute(['DIRECT']), 'DIRECT');
  assert.equal(classifyRawRoute(['🇭🇰 香港W06', 'Proxy-Group']), 'PROXY');
  assert.equal(classifyRawRoute(['REJECT']), 'REJECT');
  assert.equal(classifyRawRoute([]), 'UNKNOWN');
  assert.equal(classifyRawRoute(null), 'UNKNOWN');
});

test('Matcher: Window violation detection must flag requests after probe end', async () => {
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
    {
      receivedAt: '2026-08-21T00:00:00.000Z',
      frame: { connections: [] }
    },
    {
      receivedAt: '2026-08-21T00:00:02.000Z',
      frame: { connections: [] }
    }
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

test('Matcher: 1-to-1 mapping must prevent single connection from matching multiple requests', async () => {
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
      localPort: 54321, // 假设复用了同一端口
      targetUrl: 'https://example.com',
      requestedAt: '2026-08-21T00:00:00.700Z',
      completedAt: '2026-08-21T00:00:00.800Z'
    }
  ];

  const connFrames = [
    {
      receivedAt: '2026-08-21T00:00:00.000Z',
      frame: { connections: [] }
    },
    {
      receivedAt: '2026-08-21T00:00:00.600Z',
      frame: {
        connections: [
          {
            id: 'conn-single-1',
            metadata: { process: 'node.exe', sourcePort: 54321, host: 'example.com' },
            chains: ['DIRECT'],
            rule: 'Match'
          }
        ]
      }
    },
    {
      receivedAt: '2026-08-21T00:00:02.000Z',
      frame: { connections: [] }
    }
  ];

  const { tmpDir, gtPath } = createCaptureTestFixture(gtList, connFrames);
  try {
    const res = await analyzeCaptureSession(tmpDir, gtPath, { expectedRoute: 'DIRECT' });
    assert.equal(res.matched, 1, 'Only 1 request can match');
    assert.equal(res.routeVerification.matchedWithKnownRoute, 1);
    assert.equal(res.routeVerification.routeMismatches, 0);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});
