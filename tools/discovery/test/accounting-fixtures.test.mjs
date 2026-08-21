/**
 * accounting-fixtures.test.mjs
 * 
 * Stage R2: Accounting, Residual & Relay Pairing Fixture Unit Tests
 * 使用 Node 内置 node:test 模块
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { analyzeSessionAccounting } from '../analyze-accounting.mjs';

function createTempSession(connFrames, trafficFrames = []) {
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-test-accounting-'));
  
  const connLines = connFrames.map(f => JSON.stringify(f)).join('\n');
  fs.writeFileSync(path.join(tmpDir, 'connections.ndjson'), connLines);

  const trafficLines = trafficFrames.map(f => JSON.stringify(f)).join('\n');
  fs.writeFileSync(path.join(tmpDir, 'traffic.ndjson'), trafficLines);

  fs.writeFileSync(path.join(tmpDir, 'manifest.json'), JSON.stringify({
    probeVersion: '0.3.1-test',
    requestedConnectionsIntervalMs: 250
  }));

  return tmpDir;
}

test('Accounting: Missing attribution connection without structural relation must NOT be deduped as relay', async () => {
  const connFrames = [
    {
      receivedAt: '2026-08-21T00:00:00.000Z',
      frame: {
        uploadTotal: 1000,
        downloadTotal: 2000,
        connections: []
      }
    },
    {
      receivedAt: '2026-08-21T00:00:01.000Z',
      frame: {
        uploadTotal: 1500,
        downloadTotal: 3000,
        connections: [
          {
            id: 'conn-missing-attr',
            upload: 500,
            download: 1000,
            metadata: { process: '', host: '1.2.3.4' },
            rule: '',
            chains: ['DIRECT'] // 单跳 DIRECT，无上层代理结构关系
          }
        ]
      }
    }
  ];

  const sessionDir = createTempSession(connFrames);
  try {
    const res = await analyzeSessionAccounting(sessionDir);
    assert.equal(res.hierarchicalAttribution.confirmedRelayDuplicateUpload, 0, 'Must NOT be deduped');
    assert.equal(res.hierarchicalAttribution.unpairedMissingAttributionUpload, 500, 'Must be kept as unpairedMissingAttribution');
    assert.equal(res.hierarchicalAttribution.knownApplicationUpload, 0, 'Must NOT masquerade as knownApplication');
    assert.equal(res.hierarchicalAttribution.uniqueObservedUpload, 500, 'Must be counted into uniqueObserved');
  } finally {
    fs.rmSync(sessionDir, { recursive: true, force: true });
  }
});

test('Accounting: Confirmed Relay Pair with structural chain relation must be deduped', async () => {
  const connFrames = [
    {
      receivedAt: '2026-08-21T00:00:00.000Z',
      frame: {
        uploadTotal: 10000,
        downloadTotal: 20000,
        connections: []
      }
    },
    {
      receivedAt: '2026-08-21T00:00:01.000Z',
      frame: {
        uploadTotal: 15000,
        downloadTotal: 30000,
        connections: [
          // 上层应用连接
          {
            id: 'app-conn-1',
            upload: 5000,
            download: 10000,
            metadata: { process: 'curl.exe', host: 'example.com' },
            rule: 'Match',
            chains: ['Node-A', 'Proxy-Group-B', 'Top-Rule-Group'] // 多跳链路
          },
          // 底层中继连接 (同一时间段、相同流量、无 process/rule)
          {
            id: 'relay-conn-1',
            upload: 5000,
            download: 10000,
            metadata: { process: '', host: '192.168.1.1' },
            rule: '',
            chains: ['Node-A']
          }
        ]
      }
    }
  ];

  const sessionDir = createTempSession(connFrames);
  try {
    const res = await analyzeSessionAccounting(sessionDir);
    assert.equal(res.hierarchicalAttribution.knownApplicationUpload, 5000, 'Known App traffic');
    assert.equal(res.hierarchicalAttribution.confirmedRelayDuplicateUpload, 5000, 'Relay duplicate recognized');
    assert.equal(res.hierarchicalAttribution.uniqueObservedUpload, 5000, 'Deduped Unique Observed matches physical delta');
    assert.equal(res.residual.residualUpload, 0, 'Zero residual with perfect dedup');
  } finally {
    fs.rmSync(sessionDir, { recursive: true, force: true });
  }
});

test('Accounting: Counter Reset / Epoch Break in the middle of session must be detected', async () => {
  const connFrames = [
    {
      receivedAt: '2026-08-21T00:00:00.000Z',
      frame: { uploadTotal: 50000, downloadTotal: 100000, connections: [] }
    },
    {
      receivedAt: '2026-08-21T00:00:01.000Z',
      frame: { uploadTotal: 60000, downloadTotal: 120000, connections: [] }
    },
    // 中间发生内核重启，计数器回退为 1000 (尽管最后值可能变大)
    {
      receivedAt: '2026-08-21T00:00:02.000Z',
      frame: { uploadTotal: 1000, downloadTotal: 2000, connections: [] }
    },
    {
      receivedAt: '2026-08-21T00:00:03.000Z',
      frame: { uploadTotal: 2000, downloadTotal: 4000, connections: [] }
    }
  ];

  const sessionDir = createTempSession(connFrames);
  try {
    const res = await analyzeSessionAccounting(sessionDir);
    assert.equal(res.epochIntegrity.hasCounterEpochBreak, true, 'Must detect epoch break');
    assert.equal(res.epochIntegrity.epochBreaksCount, 1, 'Must record 1 break');
    assert.equal(res.globalCounters.globalUploadDelta, null, 'Must invalidate cross-epoch global delta');
  } finally {
    fs.rmSync(sessionDir, { recursive: true, force: true });
  }
});

test('Accounting: Bootstrap preexisting connection must not count historical bytes into current delta', async () => {
  const connFrames = [
    // 首帧冷启动已有连接，自带 1MB 历史下载
    {
      receivedAt: '2026-08-21T00:00:00.000Z',
      frame: {
        uploadTotal: 1000000,
        downloadTotal: 5000000,
        connections: [
          {
            id: 'preexisting-conn-1',
            upload: 100000,
            download: 1000000,
            metadata: { process: 'chrome.exe', host: 'example.com' },
            rule: 'Match',
            chains: ['DIRECT']
          }
        ]
      }
    },
    // 第二帧该长连接产生了 500 字节增量
    {
      receivedAt: '2026-08-21T00:00:01.000Z',
      frame: {
        uploadTotal: 1000100,
        downloadTotal: 5000500,
        connections: [
          {
            id: 'preexisting-conn-1',
            upload: 100100,
            download: 1000500,
            metadata: { process: 'chrome.exe', host: 'example.com' },
            rule: 'Match',
            chains: ['DIRECT']
          }
        ]
      }
    }
  ];

  const sessionDir = createTempSession(connFrames);
  try {
    const res = await analyzeSessionAccounting(sessionDir);
    assert.equal(res.hierarchicalAttribution.knownApplicationUpload, 100, 'Only delta (100B) counted, not 100KB');
    assert.equal(res.hierarchicalAttribution.knownApplicationDownload, 500, 'Only delta (500B) counted, not 1MB');
  } finally {
    fs.rmSync(sessionDir, { recursive: true, force: true });
  }
});
