/**
 * analyze-accounting.mjs
 * 
 * ProxyLens Global Accounting, Residual Analysis & Traffic Integration Tool
 * 
 * 核心功能:
 * 1. Global Counter Delta: 基于相邻帧扫描检测 counter_epoch_break / reset
 * 2. 分层归因计算:
 *    - knownApplication: 有确凿 process + rule 的应用层连接
 *    - confirmedRelayDuplicate: 具备严密时间重叠、流量吻合、链路结构关系的底层中继连接
 *    - unpairedMissingAttribution: 缺归因且未配对的独立连接 (保留，绝不冒充 Known App)
 *    - uniqueObserved: 去重后的独立观测流量总和
 * 3. Residual: Global Delta - uniqueObserved
 * 4. Approximate Traffic Integration: /traffic 速率时间积分 (明确为近似校验 helper)
 */

import fs from 'fs';
import path from 'path';
import readline from 'readline';
import { fileURLToPath } from 'url';

async function parseNdjson(filePath) {
  if (!fs.existsSync(filePath)) return [];
  const lines = [];
  const rl = readline.createInterface({
    input: fs.createReadStream(filePath),
    crlfDelay: Infinity
  });
  for await (const line of rl) {
    if (line.trim()) {
      try {
        lines.push(JSON.parse(line));
      } catch (err) {}
    }
  }
  return lines;
}

export async function analyzeSessionAccounting(sessionDir) {
  const connPath = path.join(sessionDir, 'connections.ndjson');
  const trafficPath = path.join(sessionDir, 'traffic.ndjson');
  const manifestPath = path.join(sessionDir, 'manifest.json');

  const [connFrames, trafficFrames] = await Promise.all([
    parseNdjson(connPath),
    parseNdjson(trafficPath)
  ]);

  let manifest = null;
  if (fs.existsSync(manifestPath)) {
    try {
      manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
    } catch {}
  }

  if (connFrames.length === 0) {
    throw new Error(`No connections frames found in ${sessionDir}`);
  }

  // 1. 逐帧扫描相邻 frames 检测 Counter Reset / Epoch Break
  let hasCounterEpochBreak = false;
  const epochBreaks = [];

  for (let i = 1; i < connFrames.length; i++) {
    const prevData = connFrames[i - 1].frame || connFrames[i - 1].payload || {};
    const currData = connFrames[i].frame || connFrames[i].payload || {};
    const prevUp = prevData.uploadTotal ?? 0;
    const currUp = currData.uploadTotal ?? 0;
    const prevDown = prevData.downloadTotal ?? 0;
    const currDown = currData.downloadTotal ?? 0;

    if (currUp < prevUp || currDown < prevDown) {
      hasCounterEpochBreak = true;
      epochBreaks.push({
        frameIndex: i,
        timestamp: connFrames[i].receivedAt || connFrames[i].timestamp,
        prevCounters: { uploadTotal: prevUp, downloadTotal: prevDown },
        currCounters: { uploadTotal: currUp, downloadTotal: currDown }
      });
    }
  }

  const firstConn = connFrames[0];
  const lastConn = connFrames[connFrames.length - 1];
  const firstConnData = firstConn.frame || firstConn.payload || {};
  const lastConnData = lastConn.frame || lastConn.payload || {};

  const firstUploadTotal = firstConnData.uploadTotal ?? 0;
  const lastUploadTotal = lastConnData.uploadTotal ?? 0;
  const firstDownloadTotal = firstConnData.downloadTotal ?? 0;
  const lastDownloadTotal = lastConnData.downloadTotal ?? 0;

  const globalUploadDelta = hasCounterEpochBreak ? null : (lastUploadTotal - firstUploadTotal);
  const globalDownloadDelta = hasCounterEpochBreak ? null : (lastDownloadTotal - firstDownloadTotal);

  const firstConnTs = new Date(firstConn.receivedAt || firstConn.timestamp).getTime();
  const lastConnTs = new Date(lastConn.receivedAt || lastConn.timestamp).getTime();

  // 2. Per-connection 追踪与生命周期计算
  const connectionsMap = new Map();

  for (let frameIdx = 0; frameIdx < connFrames.length; frameIdx++) {
    const rawFrame = connFrames[frameIdx];
    const frameData = rawFrame.frame || rawFrame.payload || {};
    const connections = frameData.connections || [];
    const frameTs = new Date(rawFrame.receivedAt || rawFrame.timestamp).getTime();

    for (const c of connections) {
      if (!c.id) continue;
      const up = c.upload ?? 0;
      const down = c.download ?? 0;
      const isMissingAttribution = (!c.metadata?.process || !c.rule);
      const isRelayCandidate = (!c.metadata?.process && !c.rule && c.chains && c.chains.length > 0);

      if (!connectionsMap.has(c.id)) {
        connectionsMap.set(c.id, {
          id: c.id,
          firstSeen: frameTs,
          lastSeen: frameTs,
          initialUp: up,
          initialDown: down,
          finalUp: up,
          finalDown: down,
          snapshotsCount: 1,
          isPreExisting: (frameIdx === 0),
          isRelayCandidate,
          isMissingAttribution,
          isConfirmedRelay: false,
          pairedAppConnId: null,
          metadata: c.metadata,
          rule: c.rule,
          chains: c.chains
        });
      } else {
        const item = connectionsMap.get(c.id);
        item.lastSeen = frameTs;
        item.finalUp = up;
        item.finalDown = down;
        item.snapshotsCount += 1;
      }
    }
  }

  // 3. Relay 配对与 pair-specific 结构关系验证
  const appConns = [];
  const relayCandidates = [];

  for (const conn of connectionsMap.values()) {
    if (conn.isRelayCandidate) {
      relayCandidates.push(conn);
    } else if (conn.metadata?.process && conn.rule) {
      appConns.push(conn);
    }
  }

  const confirmedRelayEvidence = [];

  for (const rc of relayCandidates) {
    const rcUpDelta = rc.isPreExisting ? (rc.finalUp - rc.initialUp) : rc.finalUp;
    const rcDownDelta = rc.isPreExisting ? (rc.finalDown - rc.initialDown) : rc.finalDown;

    for (const ac of appConns) {
      const acUpDelta = ac.isPreExisting ? (ac.finalUp - ac.initialUp) : ac.finalUp;
      const acDownDelta = ac.isPreExisting ? (ac.finalDown - ac.initialDown) : ac.finalDown;

      // a. 时间窗口重叠
      const timeOverlap = (rc.firstSeen <= ac.lastSeen + 2000 && rc.lastSeen >= ac.firstSeen - 2000);

      // b. pair-specific 结构关系验证 (rc 的出站 hop 必须存在于 ac 的 chains 中且出口物理节点一致)
      const rcChains = Array.isArray(rc.chains) ? rc.chains : [];
      const acChains = Array.isArray(ac.chains) ? ac.chains : [];
      const sharedStructuralHops = rcChains.filter(hop => acChains.includes(hop));
      const structuralRelation = (rcChains.length > 0 && acChains.length > 1 && sharedStructuralHops.length > 0 && rcChains[0] === acChains[0]);

      // c. 保守流量匹配
      const upDiff = Math.abs(rcUpDelta - acUpDelta);
      const downDiff = Math.abs(rcDownDelta - acDownDelta);
      const upMatch = upDiff < 2000 || (acUpDelta > 0 && upDiff / acUpDelta < 0.05);
      const downMatch = downDiff < 2000 || (acDownDelta > 0 && downDiff / acDownDelta < 0.05);

      const hasTraffic = (rcUpDelta > 500 || rcDownDelta > 500 || acUpDelta > 500 || acDownDelta > 500);
      let trafficMatch = false;

      if (hasTraffic) {
        if (acUpDelta > 500 && acDownDelta > 500) {
          trafficMatch = upMatch && downMatch;
        } else if (acDownDelta > 2000 && acUpDelta <= 500) {
          trafficMatch = downMatch && (upDiff < 2000);
        } else if (acUpDelta > 2000 && acDownDelta <= 500) {
          trafficMatch = upMatch && (downDiff < 2000);
        } else {
          trafficMatch = upMatch && downMatch;
        }
      }

      if (timeOverlap && structuralRelation && trafficMatch) {
        rc.isConfirmedRelay = true;
        rc.pairedAppConnId = ac.id;
        confirmedRelayEvidence.push({
          candidateConnectionId: rc.id,
          logicalConnectionId: ac.id,
          evidence: {
            timeOverlap,
            uploadMatch: upMatch,
            downloadMatch: downMatch,
            trafficMatch,
            candidateChains: rcChains,
            logicalChains: acChains,
            sharedStructuralHops,
            structuralRelation
          }
        });
        break;
      }
    }
  }

  // 4. 分层流量汇总与归因
  let knownApplicationUpload = 0;
  let knownApplicationDownload = 0;
  let confirmedRelayDuplicateUpload = 0;
  let confirmedRelayDuplicateDownload = 0;
  let unpairedMissingAttributionUpload = 0;
  let unpairedMissingAttributionDownload = 0;
  let otherObservedUniqueUpload = 0;
  let otherObservedUniqueDownload = 0;

  for (const conn of connectionsMap.values()) {
    let connUp = conn.isPreExisting ? (conn.finalUp - conn.initialUp) : conn.finalUp;
    let connDown = conn.isPreExisting ? (conn.finalDown - conn.initialDown) : conn.finalDown;

    if (conn.isConfirmedRelay) {
      confirmedRelayDuplicateUpload += connUp;
      confirmedRelayDuplicateDownload += connDown;
    } else if (conn.isMissingAttribution) {
      unpairedMissingAttributionUpload += connUp;
      unpairedMissingAttributionDownload += connDown;
    } else if (conn.metadata?.process && conn.rule) {
      knownApplicationUpload += connUp;
      knownApplicationDownload += connDown;
    } else {
      otherObservedUniqueUpload += connUp;
      otherObservedUniqueDownload += connDown;
    }
  }

  const uniqueObservedUpload = knownApplicationUpload + unpairedMissingAttributionUpload + otherObservedUniqueUpload;
  const uniqueObservedDownload = knownApplicationDownload + unpairedMissingAttributionDownload + otherObservedUniqueDownload;

  // 5. 残差 (Residual) 计算
  let residualUpload = null;
  let residualDownload = null;
  let residualUploadPct = null;
  let residualDownloadPct = null;

  if (!hasCounterEpochBreak && globalUploadDelta !== null && globalDownloadDelta !== null) {
    residualUpload = globalUploadDelta - uniqueObservedUpload;
    residualDownload = globalDownloadDelta - uniqueObservedDownload;
    residualUploadPct = globalUploadDelta > 0 ? (residualUpload / globalUploadDelta) * 100 : 0;
    residualDownloadPct = globalDownloadDelta > 0 ? (residualDownload / globalDownloadDelta) * 100 : 0;
  }

  // 6. /traffic 近似速率时间积分 (Approximate Rate Integration)
  let estimatedUploadBytes = 0;
  let estimatedDownloadBytes = 0;
  let trafficFramesInWindow = 0;

  const windowedTraffic = trafficFrames
    .map(f => ({
      ts: new Date(f.receivedAt || f.timestamp).getTime(),
      up: (f.frame || f.payload)?.up ?? 0,
      down: (f.frame || f.payload)?.down ?? 0
    }))
    .filter(f => f.ts >= firstConnTs - 1500 && f.ts <= lastConnTs + 1500)
    .sort((a, b) => a.ts - b.ts);

  for (let i = 0; i < windowedTraffic.length; i++) {
    const curr = windowedTraffic[i];
    trafficFramesInWindow++;

    let dtSec = 1.0;
    if (i > 0) {
      const prev = windowedTraffic[i - 1];
      const diffMs = curr.ts - prev.ts;
      if (diffMs > 0 && diffMs < 5000) {
        dtSec = diffMs / 1000.0;
      }
    } else if (i === 0 && windowedTraffic.length > 1) {
      const next = windowedTraffic[1];
      const diffMs = next.ts - curr.ts;
      if (diffMs > 0 && diffMs < 5000) {
        dtSec = diffMs / 1000.0;
      }
    }

    estimatedUploadBytes += curr.up * dtSec;
    estimatedDownloadBytes += curr.down * dtSec;
  }

  let differenceVsGlobalPctUp = null;
  let differenceVsGlobalPctDown = null;
  if (!hasCounterEpochBreak && globalUploadDelta !== null && globalDownloadDelta !== null) {
    differenceVsGlobalPctUp = globalUploadDelta > 0 ? ((estimatedUploadBytes - globalUploadDelta) / globalUploadDelta) * 100 : 0;
    differenceVsGlobalPctDown = globalDownloadDelta > 0 ? ((estimatedDownloadBytes - globalDownloadDelta) / globalDownloadDelta) * 100 : 0;
  }

  return {
    session: path.basename(sessionDir),
    requestedIntervalMs: manifest?.requestedConnectionsIntervalMs ?? null,
    totalConnectionsTracked: connectionsMap.size,
    epochIntegrity: {
      hasCounterEpochBreak,
      epochBreaksCount: epochBreaks.length,
      epochBreaks
    },
    globalCounters: {
      firstUploadTotal,
      lastUploadTotal,
      firstDownloadTotal,
      lastDownloadTotal,
      globalUploadDelta,
      globalDownloadDelta
    },
    hierarchicalAttribution: {
      knownApplicationUpload,
      knownApplicationDownload,
      confirmedRelayDuplicateUpload,
      confirmedRelayDuplicateDownload,
      unpairedMissingAttributionUpload,
      unpairedMissingAttributionDownload,
      otherObservedUniqueUpload,
      otherObservedUniqueDownload,
      uniqueObservedUpload,
      uniqueObservedDownload,
      confirmedRelayEvidence
    },
    residual: {
      residualUpload,
      residualDownload,
      residualUploadPct,
      residualDownloadPct
    },
    trafficRateIntegration: {
      method: "rectangular_interval_approximation",
      approximate: true,
      windowStartTs: firstConnTs,
      windowEndTs: lastConnTs,
      includedFrames: trafficFramesInWindow,
      estimatedUploadBytes: Math.round(estimatedUploadBytes),
      estimatedDownloadBytes: Math.round(estimatedDownloadBytes),
      differenceVsGlobalPctUp,
      differenceVsGlobalPctDown
    }
  };
}

// CLI 执行
if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const args = process.argv.slice(2);
  const jsonMode = args.includes('--json');
  const sessionDir = args.find(a => !a.startsWith('-'));

  if (!sessionDir) {
    console.error('Usage: node tools/discovery/analyze-accounting.mjs <sessionDir> [--json]');
    process.exit(1);
  }

  analyzeSessionAccounting(path.resolve(sessionDir))
    .then(r => {
      if (jsonMode) {
        console.log(JSON.stringify(r, null, 2));
        return;
      }

      console.log('================================================================');
      console.log(`ProxyLens Global Accounting & Reconciliation: ${r.session}`);
      console.log('================================================================');
      console.log(`Epoch Status                  : ${r.epochIntegrity.hasCounterEpochBreak ? 'COUNTER EPOCH BREAK DETECTED' : 'CLEAN MONOTONIC EPOCH'}`);
      console.log('----------------------------------------------------------------');
      console.log('1. GLOBAL COUNTER DELTAS:');
      console.log(`   Global Upload Delta        : ${r.globalCounters.globalUploadDelta !== null ? r.globalCounters.globalUploadDelta.toLocaleString() + ' Bytes' : 'INVALID (RESET)'}`);
      console.log(`   Global Download Delta      : ${r.globalCounters.globalDownloadDelta !== null ? r.globalCounters.globalDownloadDelta.toLocaleString() + ' Bytes' : 'INVALID (RESET)'}`);
      console.log('----------------------------------------------------------------');
      console.log('2. HIERARCHICAL ATTRIBUTED TRAFFIC:');
      console.log(`   Known Application Upload   : ${r.hierarchicalAttribution.knownApplicationUpload.toLocaleString()} Bytes`);
      console.log(`   Known Application Download : ${r.hierarchicalAttribution.knownApplicationDownload.toLocaleString()} Bytes`);
      console.log(`   Confirmed Relay Duplicate  : Up ${r.hierarchicalAttribution.confirmedRelayDuplicateUpload.toLocaleString()} B, Down ${r.hierarchicalAttribution.confirmedRelayDuplicateDownload.toLocaleString()} B (Deduped)`);
      console.log(`   Unpaired Missing Attr      : Up ${r.hierarchicalAttribution.unpairedMissingAttributionUpload.toLocaleString()} B, Down ${r.hierarchicalAttribution.unpairedMissingAttributionDownload.toLocaleString()} B (Preserved)`);
      console.log(`   Unique Observed Sum        : Up ${r.hierarchicalAttribution.uniqueObservedUpload.toLocaleString()} B, Down ${r.hierarchicalAttribution.uniqueObservedDownload.toLocaleString()} B`);
      console.log('----------------------------------------------------------------');
      console.log('3. RESIDUAL (Global Delta - Unique Observed):');
      console.log(`   Residual Upload            : ${r.residual.residualUpload !== null ? r.residual.residualUpload.toLocaleString() + ' B (' + r.residual.residualUploadPct.toFixed(2) + '%)' : 'N/A'}`);
      console.log(`   Residual Download          : ${r.residual.residualDownload !== null ? r.residual.residualDownload.toLocaleString() + ' B (' + r.residual.residualDownloadPct.toFixed(2) + '%)' : 'N/A'}`);
      console.log('----------------------------------------------------------------');
      console.log('4. APPROXIMATE /traffic RATE INTEGRATION:');
      console.log(`   Estimated Upload (Rate Int): ${r.trafficRateIntegration.estimatedUploadBytes.toLocaleString()} B (Diff vs Global: ${r.trafficRateIntegration.differenceVsGlobalPctUp !== null ? (r.trafficRateIntegration.differenceVsGlobalPctUp > 0 ? '+' : '') + r.trafficRateIntegration.differenceVsGlobalPctUp.toFixed(2) + '%' : 'N/A'})`);
      console.log(`   Estimated Download         : ${r.trafficRateIntegration.estimatedDownloadBytes.toLocaleString()} B (Diff vs Global: ${r.trafficRateIntegration.differenceVsGlobalPctDown !== null ? (r.trafficRateIntegration.differenceVsGlobalPctDown > 0 ? '+' : '') + r.trafficRateIntegration.differenceVsGlobalPctDown.toFixed(2) + '%' : 'N/A'})`);
      console.log('================================================================\n');
    })
    .catch(err => {
      console.error('[FATAL]', err);
      process.exit(1);
    });
}
