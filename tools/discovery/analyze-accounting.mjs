/**
 * analyze-accounting.mjs
 * 
 * ProxyLens Global Accounting & /traffic Reconciliation Tool
 * 
 * 分析与量化：
 * 1. Global Delta: uploadTotal / downloadTotal 的起止差值
 * 2. Attributed Delta: 全量连接的 per-connection delta 求和
 * 3. Residual: Global Delta - Attributed Delta (以及短连接盲区等对残差的影响)
 * 4. Traffic Rate Integration: /traffic 速率积分与 Global Delta 的对比
 * 
 * 用法:
 *   node tools/discovery/analyze-accounting.mjs <sessionDir> [--json]
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
      } catch (err) {
        // ignore malformed line
      }
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

  // 1. Global Delta 计算与 Counter Reset 检测
  const firstConn = connFrames[0];
  const lastConn = connFrames[connFrames.length - 1];

  const firstConnData = firstConn.frame || firstConn.payload || {};
  const lastConnData = lastConn.frame || lastConn.payload || {};

  const firstUploadTotal = firstConnData.uploadTotal ?? 0;
  const lastUploadTotal = lastConnData.uploadTotal ?? 0;
  const firstDownloadTotal = firstConnData.downloadTotal ?? 0;
  const lastDownloadTotal = lastConnData.downloadTotal ?? 0;

  const isCounterReset = (lastUploadTotal < firstUploadTotal || lastDownloadTotal < firstDownloadTotal);
  const globalUploadDelta = isCounterReset ? null : (lastUploadTotal - firstUploadTotal);
  const globalDownloadDelta = isCounterReset ? null : (lastDownloadTotal - firstDownloadTotal);

  const firstConnTs = new Date(firstConn.receivedAt || firstConn.timestamp).getTime();
  const lastConnTs = new Date(lastConn.receivedAt || lastConn.timestamp).getTime();

  // 2. Per-connection Attributed Delta 计算
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
          minUp: up,
          maxUp: up,
          minDown: down,
          maxDown: down,
          snapshotsCount: 1,
          isPreExisting: (frameIdx === 0),
          isRelayCandidate,
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
        item.maxUp = Math.max(item.maxUp, up);
        item.maxDown = Math.max(item.maxDown, down);
        item.minUp = Math.min(item.minUp, up);
        item.minDown = Math.min(item.minDown, down);
        item.snapshotsCount += 1;
      }
    }
  }

  // 3. Relay 配对与验证引擎 (Pairing & Validation Engine)
  const appConns = [];
  const relayCandidates = [];

  for (const conn of connectionsMap.values()) {
    if (conn.isRelayCandidate) {
      relayCandidates.push(conn);
    } else if (conn.metadata?.process && conn.rule) {
      appConns.push(conn);
    }
  }

  // 尝试为每个 relayCandidate 寻找确凿的应用连接配对
  for (const rc of relayCandidates) {
    const rcUpDelta = rc.isPreExisting ? (rc.finalUp - rc.initialUp) : rc.finalUp;
    const rcDownDelta = rc.isPreExisting ? (rc.finalDown - rc.initialDown) : rc.finalDown;

    for (const ac of appConns) {
      const acUpDelta = ac.isPreExisting ? (ac.finalUp - ac.initialUp) : ac.finalUp;
      const acDownDelta = ac.isPreExisting ? (ac.finalDown - ac.initialDown) : ac.finalDown;

      // 配对条件:
      // a. 时间窗口重叠
      const timeOverlap = (rc.firstSeen <= ac.lastSeen + 2000 && rc.lastSeen >= ac.firstSeen - 2000);
      // b. 流量高度吻合 (差值 < 2000B 或相对差 < 5%)
      const upMatch = Math.abs(rcUpDelta - acUpDelta) < 2000 || (acUpDelta > 0 && Math.abs(rcUpDelta - acUpDelta) / acUpDelta < 0.05);
      const downMatch = Math.abs(rcDownDelta - acDownDelta) < 2000 || (acDownDelta > 0 && Math.abs(rcDownDelta - acDownDelta) / acDownDelta < 0.05);

      if (timeOverlap && (upMatch || downMatch) && (rcUpDelta > 500 || rcDownDelta > 500)) {
        rc.isConfirmedRelay = true;
        rc.pairedAppConnId = ac.id;
        break;
      }
    }
  }

  // 4. 流量汇总与归因
  let attributedUpload = 0;
  let attributedDownload = 0;
  let appUpload = 0;
  let appDownload = 0;
  let confirmedRelayUpload = 0;
  let confirmedRelayDownload = 0;
  let unpairedMissingAttributionUpload = 0;
  let unpairedMissingAttributionDownload = 0;

  for (const conn of connectionsMap.values()) {
    let connUp = 0;
    let connDown = 0;

    if (conn.isPreExisting) {
      connUp = conn.finalUp - conn.initialUp;
      connDown = conn.finalDown - conn.initialDown;
    } else {
      connUp = conn.finalUp;
      connDown = conn.finalDown;
    }

    attributedUpload += connUp;
    attributedDownload += connDown;

    if (conn.isConfirmedRelay) {
      confirmedRelayUpload += connUp;
      confirmedRelayDownload += connDown;
    } else if (conn.isRelayCandidate && !conn.isConfirmedRelay) {
      // 未配对成功的缺归因连接：保留，不作为 Relay 剔除
      unpairedMissingAttributionUpload += connUp;
      unpairedMissingAttributionDownload += connDown;
      appUpload += connUp;
      appDownload += connDown;
    } else {
      appUpload += connUp;
      appDownload += connDown;
    }
  }

  // 5. 残差 (Residual) 计算
  let appResidualUpload = null;
  let appResidualDownload = null;
  let appResidualUploadPct = null;
  let appResidualDownloadPct = null;

  if (!isCounterReset && globalUploadDelta !== null && globalDownloadDelta !== null) {
    appResidualUpload = globalUploadDelta - appUpload;
    appResidualDownload = globalDownloadDelta - appDownload;
    appResidualUploadPct = globalUploadDelta > 0 ? (appResidualUpload / globalUploadDelta) * 100 : 0;
    appResidualDownloadPct = globalDownloadDelta > 0 ? (appResidualDownload / globalDownloadDelta) * 100 : 0;
  }

  // 6. /traffic 严格时间窗口对齐积分 (Strictly Window-Aligned Integration)
  let integratedUpload = 0;
  let integratedDownload = 0;
  let trafficFramesInWindow = 0;

  // 过滤并排序窗口内的 traffic frames
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

    integratedUpload += curr.up * dtSec;
    integratedDownload += curr.down * dtSec;
  }

  let trafficDiscrepancyUp = null;
  let trafficDiscrepancyDown = null;
  if (!isCounterReset && globalUploadDelta !== null && globalDownloadDelta !== null) {
    trafficDiscrepancyUp = globalUploadDelta > 0 ? ((integratedUpload - globalUploadDelta) / globalUploadDelta) * 100 : 0;
    trafficDiscrepancyDown = globalDownloadDelta > 0 ? ((integratedDownload - globalDownloadDelta) / globalDownloadDelta) * 100 : 0;
  }

  return {
    sessionDir,
    durationMs: lastConnTs - firstConnTs,
    isCounterReset,
    connectionFramesCount: connFrames.length,
    trafficFramesCount: trafficFrames.length,
    trafficFramesInWindow,
    totalObservedConnections: connectionsMap.size,
    confirmedRelayCount: relayCandidates.filter(r => r.isConfirmedRelay).length,
    unpairedMissingAttributionCount: relayCandidates.filter(r => !r.isConfirmedRelay).length,
    global: {
      firstUploadTotal,
      lastUploadTotal,
      firstDownloadTotal,
      lastDownloadTotal,
      uploadDelta: globalUploadDelta,
      downloadDelta: globalDownloadDelta
    },
    attributed: {
      totalUpload: attributedUpload,
      totalDownload: attributedDownload,
      appUpload,
      appDownload,
      confirmedRelayUpload,
      confirmedRelayDownload,
      unpairedMissingAttributionUpload,
      unpairedMissingAttributionDownload
    },
    residual: {
      appResidualUpload,
      appResidualDownload,
      appResidualUploadPct,
      appResidualDownloadPct
    },
    trafficIntegration: {
      integratedUpload: Math.round(integratedUpload),
      integratedDownload: Math.round(integratedDownload),
      discrepancyUpPct: trafficDiscrepancyUp,
      discrepancyDownPct: trafficDiscrepancyDown
    }
  };
}

// CLI 执行
if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const sessionDir = process.argv[2];
  const isJson = process.argv.includes('--json');

  if (!sessionDir) {
    console.error('Usage: node tools/discovery/analyze-accounting.mjs <sessionDir> [--json]');
    process.exit(1);
  }

  analyzeSessionAccounting(path.resolve(sessionDir))
    .then(result => {
      if (isJson) {
        console.log(JSON.stringify(result, null, 2));
      } else {
        console.log('================================================================');
        console.log('PROXYLENS GLOBAL ACCOUNTING & TRAFFIC RECONCILIATION REPORT');
        console.log('================================================================');
        console.log(`Session Dir               : ${result.sessionDir}`);
        console.log(`Duration                  : ${(result.durationMs / 1000).toFixed(2)}s`);
        console.log(`Frames Analyzed           : ${result.connectionFramesCount} Connections, ${result.trafficFramesCount} Traffic`);
        console.log(`Total Connections Tracked : ${result.totalObservedConnections}`);
        console.log('----------------------------------------------------------------');
        console.log('1. GLOBAL COUNTERS (uploadTotal / downloadTotal):');
        if (result.isCounterReset) {
          console.log('   [WARN: COUNTER RESET DETECTED - GLOBAL DELTA INVALIDATED]');
        } else {
          console.log(`   Global Upload Delta   : ${result.global.uploadDelta.toLocaleString()} Bytes`);
          console.log(`   Global Download Delta : ${result.global.downloadDelta.toLocaleString()} Bytes`);
        }
        console.log('----------------------------------------------------------------');
        console.log('2. ATTRIBUTED TRAFFIC & RELAY PAIRING:');
        console.log(`   Application Upload    : ${result.attributed.appUpload.toLocaleString()} Bytes (Includes ${result.attributed.unpairedMissingAttributionUpload}B unpaired)`);
        console.log(`   Application Download  : ${result.attributed.appDownload.toLocaleString()} Bytes (Includes ${result.attributed.unpairedMissingAttributionDownload}B unpaired)`);
        console.log(`   Confirmed Relay Dedup : Up ${result.attributed.confirmedRelayUpload.toLocaleString()} Bytes, Down ${result.attributed.confirmedRelayDownload.toLocaleString()} Bytes (${result.confirmedRelayCount} paired conns)`);
        console.log(`   Unpaired Missing Attrib: Up ${result.attributed.unpairedMissingAttributionUpload.toLocaleString()} Bytes, Down ${result.attributed.unpairedMissingAttributionDownload.toLocaleString()} Bytes (${result.unpairedMissingAttributionCount} conns)`);
        console.log(`   Total (Raw Sum)       : Up ${result.attributed.totalUpload.toLocaleString()} Bytes, Down ${result.attributed.totalDownload.toLocaleString()} Bytes`);
        console.log('----------------------------------------------------------------');
        console.log('3. RESIDUAL & UNATTRIBUTED GAP (Global - Application):');
        if (result.isCounterReset) {
          console.log('   Residual unavailable due to counter reset');
        } else {
          console.log(`   App Upload Residual   : ${result.residual.appResidualUpload.toLocaleString()} Bytes (${result.residual.appResidualUploadPct.toFixed(2)}%)`);
          console.log(`   App Download Residual : ${result.residual.appResidualDownload.toLocaleString()} Bytes (${result.residual.appResidualDownloadPct.toFixed(2)}%)`);
        }
        console.log('----------------------------------------------------------------');
        console.log('4. /traffic STRICT WINDOW RECONCILIATION:');
        console.log(`   Integrated Upload     : ${result.trafficIntegration.integratedUpload.toLocaleString()} Bytes ${result.trafficIntegration.discrepancyUpPct !== null ? '(Diff: ' + result.trafficIntegration.discrepancyUpPct.toFixed(2) + '%)' : ''}`);
        console.log(`   Integrated Download   : ${result.trafficIntegration.integratedDownload.toLocaleString()} Bytes ${result.trafficIntegration.discrepancyDownPct !== null ? '(Diff: ' + result.trafficIntegration.discrepancyDownPct.toFixed(2) + '%)' : ''}`);
        console.log('================================================================');
      }
    })
    .catch(err => {
      console.error('[ERROR]', err.message);
      process.exit(1);
    });
}
