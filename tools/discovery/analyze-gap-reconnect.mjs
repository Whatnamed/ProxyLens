/**
 * analyze-gap-reconnect.mjs
 * 
 * 分析 Collector 断线重连与 Monitoring Gap 场景下的流量差分与状态机表现
 * 
 * 核心指标:
 * 1. coverageGap: 基于真实前后帧时间戳的实际数据覆盖盲区
 * 2. 精细化跨 Gap 连接生命周期分类
 * 3. 跨 Gap 存活连接 delta 的时间区间归档
 */

import fs from 'fs';
import path from 'path';
import readline from 'readline';
import { fileURLToPath } from 'url';

async function parseNdjson(filePath) {
  if (!fs.existsSync(filePath)) return [];
  const lines = [];
  const rl = readline.createInterface({ input: fs.createReadStream(filePath), crlfDelay: Infinity });
  for await (const line of rl) {
    if (line.trim()) {
      try { lines.push(JSON.parse(line)); } catch {}
    }
  }
  return lines;
}

export async function analyzeGapExperiment(gapBaseDir) {
  const metaPath = path.join(gapBaseDir, 'gap-meta.json');
  if (!fs.existsSync(metaPath)) {
    throw new Error(`gap-meta.json not found in ${gapBaseDir}`);
  }
  const meta = JSON.parse(fs.readFileSync(metaPath, 'utf8'));

  const p1Frames = await parseNdjson(path.join(meta.phase1Dir, 'connections.ndjson'));
  const p2Frames = await parseNdjson(path.join(meta.phase2Dir, 'connections.ndjson'));

  if (p1Frames.length === 0 || p2Frames.length === 0) {
    throw new Error(`Phase frames empty in ${gapBaseDir}`);
  }

  const p1Last = p1Frames[p1Frames.length - 1];
  const p2First = p2Frames[0];
  const p2Last = p2Frames[p2Frames.length - 1];

  const p1LastTs = p1Last.receivedAt || p1Last.timestamp;
  const p2FirstTs = p2First.receivedAt || p2First.timestamp;
  const p2LastTs = p2Last.receivedAt || p2Last.timestamp;

  const actualObservationGapMs = new Date(p2FirstTs).getTime() - new Date(p1LastTs).getTime();

  const p1LastData = p1Last.frame || p1Last.payload || {};
  const p2FirstData = p2First.frame || p2First.payload || {};
  const p2LastData = p2Last.frame || p2Last.payload || {};

  // 1. 全局计数器变化
  const gapUploadDelta = p2FirstData.uploadTotal - p1LastData.uploadTotal;
  const gapDownloadDelta = p2FirstData.downloadTotal - p1LastData.downloadTotal;

  const p2ObservedUploadDelta = p2LastData.uploadTotal - p2FirstData.uploadTotal;
  const p2ObservedDownloadDelta = p2LastData.downloadTotal - p2FirstData.downloadTotal;

  // 2. 连接 ID 连续性与精细化跨 Gap 分类
  const p1ConnMap = new Map();
  (p1LastData.connections || []).forEach(c => p1ConnMap.set(c.id, c));

  const p2FirstConnMap = new Map();
  (p2FirstData.connections || []).forEach(c => p2FirstConnMap.set(c.id, c));

  let survivedAcrossGap = 0;
  let firstObservedAfterGapInsideGap = 0;
  let firstObservedAfterGapBeforeGap = 0;
  let disappearedDuringGap = 0;

  const survivingDeltas = [];
  const gapStartMs = new Date(p1LastTs).getTime();

  for (const [id, c2] of p2FirstConnMap.entries()) {
    if (p1ConnMap.has(id)) {
      survivedAcrossGap++;
      const c1 = p1ConnMap.get(id);
      const upDelta = c2.upload - c1.upload;
      const downDelta = c2.download - c1.download;
      if (upDelta > 0 || downDelta > 0) {
        survivingDeltas.push({
          id,
          process: c2.metadata?.process,
          host: c2.metadata?.host || c2.metadata?.destinationIP,
          attributionInterval: [p1LastTs, p2FirstTs],
          upDelta,
          downDelta
        });
      }
    } else {
      const connStartMs = c2.start ? new Date(c2.start).getTime() : 0;
      if (connStartMs >= gapStartMs) {
        firstObservedAfterGapInsideGap++;
      } else {
        firstObservedAfterGapBeforeGap++;
      }
    }
  }

  for (const id of p1ConnMap.keys()) {
    if (!p2FirstConnMap.has(id)) {
      disappearedDuringGap++;
    }
  }

  // 3. Naive Bootstrap 错误量化
  let naiveBootstrapUpload = 0;
  let naiveBootstrapDownload = 0;
  (p2FirstData.connections || []).forEach(c => {
    naiveBootstrapUpload += (c.upload ?? 0);
    naiveBootstrapDownload += (c.download ?? 0);
  });

  return {
    gapBaseDir,
    processWallClockGapMs: meta.gapDurationMs,
    coverageGap: {
      lastObservedAt: p1LastTs,
      firstObservedAfterRecoveryAt: p2FirstTs,
      actualObservationGapMs,
      actualObservationGapSec: (actualObservationGapMs / 1000).toFixed(2)
    },
    globalGap: {
      gapUploadDelta,
      gapDownloadDelta,
      p2ObservedUploadDelta,
      p2ObservedDownloadDelta
    },
    connectionContinuity: {
      p1LastConnsCount: p1ConnMap.size,
      p2FirstConnsCount: p2FirstConnMap.size,
      survivedAcrossGap,
      firstObservedAfterGapInsideGap,
      firstObservedAfterGapBeforeGap,
      disappearedDuringGap,
      survivingWithTrafficCount: survivingDeltas.length,
      sampleSurvivingDeltas: survivingDeltas.slice(0, 5)
    },
    naiveBootstrapError: {
      fakeUploadExplosion: naiveBootstrapUpload,
      fakeDownloadExplosion: naiveBootstrapDownload
    }
  };
}

if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const args = process.argv.slice(2);
  const jsonMode = args.includes('--json');
  const gapDir = args.find(a => !a.startsWith('-'));

  if (!gapDir) {
    console.error('Usage: node tools/discovery/analyze-gap-reconnect.mjs <gapBaseDir> [--json]');
    process.exit(1);
  }

  analyzeGapExperiment(path.resolve(gapDir))
    .then(r => {
      if (jsonMode) {
        console.log(JSON.stringify(r, null, 2));
        return;
      }

      console.log('================================================================');
      console.log('STAGE R4: CONTROLLER GAP & RECONNECT RECOVERY REPORT');
      console.log('================================================================');
      console.log(`Coverage Gap Window             : ${r.coverageGap.lastObservedAt} -> ${r.coverageGap.firstObservedAfterRecoveryAt}`);
      console.log(`Actual Observation Gap Duration : ${r.coverageGap.actualObservationGapSec}s (${r.coverageGap.actualObservationGapMs} ms)`);
      console.log('----------------------------------------------------------------');
      console.log('1. UNMONITORED GAP PHYSICAL TRAFFIC:');
      console.log(`   Physical Upload during Gap   : ${r.globalGap.gapUploadDelta.toLocaleString()} Bytes`);
      console.log(`   Physical Download during Gap : ${r.globalGap.gapDownloadDelta.toLocaleString()} Bytes`);
      console.log('----------------------------------------------------------------');
      console.log('2. FINE-GRAINED CONNECTION CLASSIFICATION:');
      console.log(`   Pre-Gap Active Conns (P1)    : ${r.connectionContinuity.p1LastConnsCount}`);
      console.log(`   Post-Gap Active Conns (P2)   : ${r.connectionContinuity.p2FirstConnsCount}`);
      console.log(`   Survived Across Gap          : ${r.connectionContinuity.survivedAcrossGap} conns (ID remains stable!)`);
      console.log(`   Started Inside Gap           : ${r.connectionContinuity.firstObservedAfterGapInsideGap} conns`);
      console.log(`   Started Pre-Gap (Late Seen)  : ${r.connectionContinuity.firstObservedAfterGapBeforeGap} conns`);
      console.log(`   Disappeared during Gap       : ${r.connectionContinuity.disappearedDuringGap} conns`);
      console.log(`   Survived with Traffic Delta  : ${r.connectionContinuity.survivingWithTrafficCount} conns`);
      console.log('----------------------------------------------------------------');
      console.log('3. NAIVE BOOTSTRAP FAKE EXPLOSION COMPARISON:');
      console.log(`   Actual Gap Download          : ${r.globalGap.gapDownloadDelta.toLocaleString()} Bytes`);
      console.log(`   Naive "First-Seen" Explosion : ${r.naiveBootstrapError.fakeDownloadExplosion.toLocaleString()} Bytes (FAKELY INFLATED!)`);
      console.log('================================================================\n');
    })
    .catch(err => {
      console.error(err);
      process.exit(1);
    });
}
