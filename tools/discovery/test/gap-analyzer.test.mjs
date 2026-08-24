/**
 * gap-analyzer.test.mjs
 * 
 * Stage F4: Gap Analyzer, Coverage Gap & Epoch Semantics Unit Tests
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { analyzeGapExperiment } from '../analyze-gap-reconnect.mjs';

function createGapTestSession(p1Frames, p2Frames, gapDurationMs = 4000) {
  const tmpBase = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-gap-test-'));
  const p1Dir = path.join(tmpBase, 'phase1');
  const p2Dir = path.join(tmpBase, 'phase2');
  fs.mkdirSync(p1Dir);
  fs.mkdirSync(p2Dir);

  fs.writeFileSync(path.join(p1Dir, 'connections.ndjson'), p1Frames.map(f => JSON.stringify(f)).join('\n'));
  fs.writeFileSync(path.join(p2Dir, 'connections.ndjson'), p2Frames.map(f => JSON.stringify(f)).join('\n'));

  fs.writeFileSync(path.join(tmpBase, 'gap-meta.json'), JSON.stringify({
    gapDurationMs,
    phase1Dir: p1Dir,
    phase2Dir: p2Dir
  }));

  return tmpBase;
}

test('F4-1: analyzeGapExperiment must compute accurate coverageGap and classify connections', async () => {
  const p1Frames = [
    {
      receivedAt: '2026-08-21T00:00:00.000Z',
      frame: { uploadTotal: 1000, downloadTotal: 5000, connections: [{ id: 'survived-1', upload: 100, download: 500, metadata: { host: 'a.com' } }] }
    },
    {
      receivedAt: '2026-08-21T00:00:01.000Z',
      frame: { uploadTotal: 1200, downloadTotal: 6000, connections: [{ id: 'survived-1', upload: 200, download: 1000, metadata: { host: 'a.com' } }] }
    }
  ];

  const p2Frames = [
    {
      receivedAt: '2026-08-21T00:00:05.000Z', // 4s Gap
      frame: {
        uploadTotal: 1500,
        downloadTotal: 8000,
        connections: [
          { id: 'survived-1', upload: 300, download: 1500, metadata: { host: 'a.com' } },
          { id: 'new-in-gap', upload: 50, download: 200, start: '2026-08-21T00:00:03.000Z', metadata: { host: 'b.com' } }
        ]
      }
    }
  ];

  const gapDir = createGapTestSession(p1Frames, p2Frames, 4000);
  try {
    const res = await analyzeGapExperiment(gapDir);
    assert.equal(res.coverageGap.actualObservationGapMs, 4000, 'Exact 4000ms gap computed');
    assert.equal(res.coverageGap.actualObservationGapSec, '4.00');
    assert.equal(res.epochIntegrity.epochBreakAcrossGap, false, 'No epoch break');
    assert.equal(res.globalGap.gapUploadDelta, 300, '1500 - 1200 = 300B upload');
    assert.equal(res.globalGap.gapDownloadDelta, 2000, '8000 - 6000 = 2000B download');
    assert.equal(res.connectionContinuity.survivedAcrossGap, 1, 'survived-1 preserved across gap');
    assert.equal(res.connectionContinuity.firstObservedAfterGapInsideGap, 1, 'new-in-gap classified correctly');
    assert.equal(res.connectionContinuity.firstObservedAfterGapStartUnknown, 0);
  } finally {
    fs.rmSync(gapDir, { recursive: true, force: true });
  }
});

test('F4-2: Missing or invalid start timestamp must be classified as firstObservedAfterGapStartUnknown', async () => {
  const p1Frames = [
    {
      receivedAt: '2026-08-21T00:00:00.000Z',
      frame: { uploadTotal: 1000, downloadTotal: 5000, connections: [] }
    }
  ];

  const p2Frames = [
    {
      receivedAt: '2026-08-21T00:00:05.000Z',
      frame: {
        uploadTotal: 1500,
        downloadTotal: 8000,
        connections: [
          // 缺失 start 字段的连接
          { id: 'conn-missing-start', upload: 50, download: 200, metadata: { host: 'unknown.com' } }
        ]
      }
    }
  ];

  const gapDir = createGapTestSession(p1Frames, p2Frames, 5000);
  try {
    const res = await analyzeGapExperiment(gapDir);
    assert.equal(res.connectionContinuity.firstObservedAfterGapStartUnknown, 1, 'Must classify as start unknown');
    assert.equal(res.connectionContinuity.firstObservedAfterGapBeforeGap, 0, 'Must NOT wrongly classify as before gap');
    assert.equal(res.connectionContinuity.firstObservedAfterGapInsideGap, 0);
  } finally {
    fs.rmSync(gapDir, { recursive: true, force: true });
  }
});

test('F4-3: Epoch break / counter reset across gap must invalidate gapPhysicalDelta', async () => {
  const p1Frames = [
    {
      receivedAt: '2026-08-21T00:00:00.000Z',
      frame: { uploadTotal: 50000, downloadTotal: 100000, connections: [] }
    }
  ];

  const p2Frames = [
    {
      // 内核在 Gap 期间重启，计数器回退为 1000/2000
      receivedAt: '2026-08-21T00:00:05.000Z',
      frame: {
        uploadTotal: 1000,
        downloadTotal: 2000,
        connections: []
      }
    }
  ];

  const gapDir = createGapTestSession(p1Frames, p2Frames, 5000);
  try {
    const res = await analyzeGapExperiment(gapDir);
    assert.equal(res.epochIntegrity.epochBreakAcrossGap, true, 'Must detect epoch break across gap');
    assert.equal(res.globalGap.gapUploadDelta, null, 'Must invalidate gap physical upload delta');
    assert.equal(res.globalGap.gapDownloadDelta, null, 'Must invalidate gap physical download delta');
  } finally {
    fs.rmSync(gapDir, { recursive: true, force: true });
  }
});
