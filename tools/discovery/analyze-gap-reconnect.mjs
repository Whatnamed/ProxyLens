/**
 * analyze-gap-reconnect.mjs
 * 
 * 分析 Collector 断线重连与 Monitoring Gap 场景下的流量差分与状态机表现
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

  const p1Last = p1Frames[p1Frames.length - 1];
  const p2First = p2Frames[0];
  const p2Last = p2Frames[p2Frames.length - 1];

  const p1LastData = p1Last.frame || p1Last.payload || {};
  const p2FirstData = p2First.frame || p2First.payload || {};
  const p2LastData = p2Last.frame || p2Last.payload || {};

  // 1. 全局计数器变化
  const gapUploadDelta = p2FirstData.uploadTotal - p1LastData.uploadTotal;
  const gapDownloadDelta = p2FirstData.downloadTotal - p1LastData.downloadTotal;

  const p2ObservedUploadDelta = p2LastData.uploadTotal - p2FirstData.uploadTotal;
  const p2ObservedDownloadDelta = p2LastData.downloadTotal - p2FirstData.downloadTotal;

  // 2. 连接 ID 连续性与跨 Gap 追踪
  const p1ConnMap = new Map();
  (p1LastData.connections || []).forEach(c => p1ConnMap.set(c.id, c));

  const p2FirstConnMap = new Map();
  (p2FirstData.connections || []).forEach(c => p2FirstConnMap.set(c.id, c));

  let survivingConnsCount = 0;
  let newConnsAtReconnectCount = 0;
  let disappearedDuringGapCount = 0;

  const survivingDeltas = [];

  for (const [id, c2] of p2FirstConnMap.entries()) {
    if (p1ConnMap.has(id)) {
      survivingConnsCount++;
      const c1 = p1ConnMap.get(id);
      const upDelta = c2.upload - c1.upload;
      const downDelta = c2.download - c1.download;
      if (upDelta > 0 || downDelta > 0) {
        survivingDeltas.push({
          id,
          process: c2.metadata?.process,
          host: c2.metadata?.host || c2.metadata?.destinationIP,
          upDelta,
          downDelta
        });
      }
    } else {
      newConnsAtReconnectCount++;
    }
  }

  for (const id of p1ConnMap.keys()) {
    if (!p2FirstConnMap.has(id)) {
      disappearedDuringGapCount++;
    }
  }

  // 3. 重连首帧如果误用 MetaCubeXD 算法 (current-from-zero) 会产生的虚假增量
  let naiveBootstrapUpload = 0;
  let naiveBootstrapDownload = 0;
  (p2FirstData.connections || []).forEach(c => {
    naiveBootstrapUpload += (c.upload ?? 0);
    naiveBootstrapDownload += (c.download ?? 0);
  });

  return {
    gapBaseDir,
    gapDurationMs: meta.gapDurationMs,
    globalGap: {
      gapUploadDelta,
      gapDownloadDelta,
      p2ObservedUploadDelta,
      p2ObservedDownloadDelta
    },
    connectionContinuity: {
      p1LastConnsCount: p1ConnMap.size,
      p2FirstConnsCount: p2FirstConnMap.size,
      survivingAcrossGap: survivingConnsCount,
      newAtReconnect: newConnsAtReconnectCount,
      disappearedDuringGap: disappearedDuringGapCount,
      survivingWithTraffic: survivingDeltas.length,
      sampleSurvivingDeltas: survivingDeltas.slice(0, 5)
    },
    naiveBootstrapError: {
      fakeUploadExplosion: naiveBootstrapUpload,
      fakeDownloadExplosion: naiveBootstrapDownload
    }
  };
}

if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const gapDir = process.argv[2];
  if (!gapDir) {
    console.error('Usage: node tools/discovery/analyze-gap-reconnect.mjs <gapBaseDir>');
    process.exit(1);
  }

  analyzeGapExperiment(path.resolve(gapDir))
    .then(r => {
      console.log('================================================================');
      console.log('STAGE B2: CONTROLLER GAP & RECONNECT RECOVERY REPORT');
      console.log('================================================================');
      console.log(`Gap Duration                    : ${(r.gapDurationMs / 1000).toFixed(2)}s`);
      console.log('----------------------------------------------------------------');
      console.log('1. UNMONITORED GAP TRAFFIC (Delta during Collector Disconnect):');
      console.log(`   Physical Upload during Gap   : ${r.globalGap.gapUploadDelta.toLocaleString()} Bytes`);
      console.log(`   Physical Download during Gap : ${r.globalGap.gapDownloadDelta.toLocaleString()} Bytes`);
      console.log('----------------------------------------------------------------');
      console.log('2. CONNECTION CONTINUITY ACROSS GAP:');
      console.log(`   Connections before Gap (P1)  : ${r.connectionContinuity.p1LastConnsCount}`);
      console.log(`   Connections after Gap (P2)   : ${r.connectionContinuity.p2FirstConnsCount}`);
      console.log(`   Surviving Across Gap         : ${r.connectionContinuity.survivingAcrossGap} conns (ID remains stable!)`);
      console.log(`   Disappeared during Gap       : ${r.connectionContinuity.disappearedDuringGap} conns`);
      console.log(`   New Conns at Reconnect       : ${r.connectionContinuity.newAtReconnect} conns`);
      console.log('----------------------------------------------------------------');
      console.log('3. NAIVE BOOTSTRAP FAKE EXPLOSION COMPARISON:');
      console.log(`   Actual Gap Download          : ${r.globalGap.gapDownloadDelta.toLocaleString()} Bytes`);
      console.log(`   Naive "First-Seen" Explosion : ${r.naiveBootstrapError.fakeDownloadExplosion.toLocaleString()} Bytes (FAKELY INFLATED!)`);
      console.log('================================================================');
    })
    .catch(err => {
      console.error(err);
      process.exit(1);
    });
}
