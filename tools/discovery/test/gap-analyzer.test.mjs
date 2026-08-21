/**
 * gap-analyzer.test.mjs
 * 
 * Stage R7: Gap Analyzer & Coverage Gap Unit Tests
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { analyzeGapExperiment } from '../analyze-gap-reconnect.mjs';

function createGapTestSession() {
  const tmpBase = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-gap-test-'));
  const p1Dir = path.join(tmpBase, 'phase1');
  const p2Dir = path.join(tmpBase, 'phase2');
  fs.mkdirSync(p1Dir);
  fs.mkdirSync(p2Dir);

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

  fs.writeFileSync(path.join(p1Dir, 'connections.ndjson'), p1Frames.map(f => JSON.stringify(f)).join('\n'));
  fs.writeFileSync(path.join(p2Dir, 'connections.ndjson'), p2Frames.map(f => JSON.stringify(f)).join('\n'));

  fs.writeFileSync(path.join(tmpBase, 'gap-meta.json'), JSON.stringify({
    gapDurationMs: 4000,
    phase1Dir: p1Dir,
    phase2Dir: p2Dir
  }));

  return tmpBase;
}

test('Gap: analyzeGapExperiment must compute accurate coverageGap and classify connections', async () => {
  const gapDir = createGapTestSession();
  try {
    const res = await analyzeGapExperiment(gapDir);
    assert.equal(res.coverageGap.actualObservationGapMs, 4000, 'Exact 4000ms gap computed');
    assert.equal(res.coverageGap.actualObservationGapSec, '4.00');
    assert.equal(res.globalGap.gapUploadDelta, 300, '1500 - 1200 = 300B upload');
    assert.equal(res.globalGap.gapDownloadDelta, 2000, '8000 - 6000 = 2000B download');
    assert.equal(res.connectionContinuity.survivedAcrossGap, 1, 'survived-1 preserved across gap');
    assert.equal(res.connectionContinuity.firstObservedAfterGapInsideGap, 1, 'new-in-gap classified correctly');
  } finally {
    fs.rmSync(gapDir, { recursive: true, force: true });
  }
});
