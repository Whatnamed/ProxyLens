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

  // 1. Global Delta 计算
  const firstConn = connFrames[0];
  const lastConn = connFrames[connFrames.length - 1];

  const firstConnData = firstConn.frame || firstConn.payload || {};
  const lastConnData = lastConn.frame || lastConn.payload || {};

  const firstUploadTotal = firstConnData.uploadTotal ?? 0;
  const lastUploadTotal = lastConnData.uploadTotal ?? 0;
  const firstDownloadTotal = firstConnData.downloadTotal ?? 0;
  const lastDownloadTotal = lastConnData.downloadTotal ?? 0;

  const globalUploadDelta = Math.max(0, lastUploadTotal - firstUploadTotal);
  const globalDownloadDelta = Math.max(0, lastDownloadTotal - firstDownloadTotal);

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
      const isRelayHop = (!c.metadata?.process && !c.rule && c.chains && c.chains.length > 0);

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
          isRelayHop,
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

  let attributedUpload = 0;
  let attributedDownload = 0;
  let appUpload = 0;
  let appDownload = 0;
  let relayUpload = 0;
  let relayDownload = 0;
  let preExistingUpload = 0;
  let preExistingDownload = 0;
  let newConnUpload = 0;
  let newConnDownload = 0;

  for (const conn of connectionsMap.values()) {
    let connUp = 0;
    let connDown = 0;

    if (conn.isPreExisting) {
      connUp = conn.finalUp - conn.initialUp;
      connDown = conn.finalDown - conn.initialDown;
      preExistingUpload += connUp;
      preExistingDownload += connDown;
    } else {
      connUp = conn.finalUp;
      connDown = conn.finalDown;
      newConnUpload += connUp;
      newConnDownload += connDown;
    }

    attributedUpload += connUp;
    attributedDownload += connDown;

    if (conn.isRelayHop) {
      relayUpload += connUp;
      relayDownload += connDown;
    } else {
      appUpload += connUp;
      appDownload += connDown;
    }
  }

  // 3. 残差 (Residual) 计算
  // 原始残差 (vs all connections) 与 应用层残差 (vs app connections)
  const residualUpload = globalUploadDelta - attributedUpload;
  const residualDownload = globalDownloadDelta - attributedDownload;
  const appResidualUpload = globalUploadDelta - appUpload;
  const appResidualDownload = globalDownloadDelta - appDownload;

  const residualUploadPct = globalUploadDelta > 0 ? (residualUpload / globalUploadDelta) * 100 : 0;
  const residualDownloadPct = globalDownloadDelta > 0 ? (residualDownload / globalDownloadDelta) * 100 : 0;
  const appResidualUploadPct = globalUploadDelta > 0 ? (appResidualUpload / globalUploadDelta) * 100 : 0;
  const appResidualDownloadPct = globalDownloadDelta > 0 ? (appResidualDownload / globalDownloadDelta) * 100 : 0;

  // 4. /traffic 速率积分计算与总计数器比对
  let integratedUpload = 0;
  let integratedDownload = 0;

  for (let i = 0; i < trafficFrames.length; i++) {
    const rawFrame = trafficFrames[i];
    const frameData = rawFrame.frame || rawFrame.payload || {};
    const upRate = frameData.up ?? 0;
    const downRate = frameData.down ?? 0;

    let dtSec = 1.0;
    if (i > 0) {
      const prevTs = new Date(trafficFrames[i - 1].receivedAt || trafficFrames[i - 1].timestamp).getTime();
      const currTs = new Date(rawFrame.receivedAt || rawFrame.timestamp).getTime();
      const diffMs = currTs - prevTs;
      if (diffMs > 0 && diffMs < 5000) {
        dtSec = diffMs / 1000.0;
      }
    }

    integratedUpload += upRate * dtSec;
    integratedDownload += downRate * dtSec;
  }

  const trafficDiscrepancyUp = globalUploadDelta > 0 ? ((integratedUpload - globalUploadDelta) / globalUploadDelta) * 100 : 0;
  const trafficDiscrepancyDown = globalDownloadDelta > 0 ? ((integratedDownload - globalDownloadDelta) / globalDownloadDelta) * 100 : 0;

  const firstConnTs = new Date(firstConn.receivedAt || firstConn.timestamp).getTime();
  const lastConnTs = new Date(lastConn.receivedAt || lastConn.timestamp).getTime();

  return {
    sessionDir,
    durationMs: lastConnTs - firstConnTs,
    connectionFramesCount: connFrames.length,
    trafficFramesCount: trafficFrames.length,
    totalObservedConnections: connectionsMap.size,
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
      relayUpload,
      relayDownload,
      preExistingUpload,
      preExistingDownload,
      newConnUpload,
      newConnDownload
    },
    residual: {
      uploadResidual: residualUpload,
      downloadResidual: residualDownload,
      uploadResidualPct: residualUploadPct,
      downloadResidualPct: residualDownloadPct,
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
        console.log(`   Global Upload Delta   : ${result.global.uploadDelta.toLocaleString()} Bytes`);
        console.log(`   Global Download Delta : ${result.global.downloadDelta.toLocaleString()} Bytes`);
        console.log('----------------------------------------------------------------');
        console.log('2. ATTRIBUTED TRAFFIC:');
        console.log(`   Application Upload    : ${result.attributed.appUpload.toLocaleString()} Bytes`);
        console.log(`   Application Download  : ${result.attributed.appDownload.toLocaleString()} Bytes`);
        console.log(`   Relay Hop Upload      : ${result.attributed.relayUpload.toLocaleString()} Bytes (Multi-hop duplicate)`);
        console.log(`   Relay Hop Download    : ${result.attributed.relayDownload.toLocaleString()} Bytes (Multi-hop duplicate)`);
        console.log(`   Total (Raw Sum)       : Up ${result.attributed.totalUpload.toLocaleString()} Bytes, Down ${result.attributed.totalDownload.toLocaleString()} Bytes`);
        console.log('----------------------------------------------------------------');
        console.log('3. RESIDUAL & UNATTRIBUTED GAP (Global - Application):');
        console.log(`   App Upload Residual   : ${result.residual.appResidualUpload.toLocaleString()} Bytes (${result.residual.appResidualUploadPct.toFixed(2)}%)`);
        console.log(`   App Download Residual : ${result.residual.appResidualDownload.toLocaleString()} Bytes (${result.residual.appResidualDownloadPct.toFixed(2)}%)`);
        console.log('----------------------------------------------------------------');
        console.log('4. /traffic RATE INTEGRATION RECONCILIATION:');
        console.log(`   Integrated Upload     : ${result.trafficIntegration.integratedUpload.toLocaleString()} Bytes (Diff vs Global: ${result.trafficIntegration.discrepancyUpPct.toFixed(2)}%)`);
        console.log(`   Integrated Download   : ${result.trafficIntegration.integratedDownload.toLocaleString()} Bytes (Diff vs Global: ${result.trafficIntegration.discrepancyDownPct.toFixed(2)}%)`);
        console.log('================================================================');
      }
    })
    .catch(err => {
      console.error('[ERROR]', err.message);
      process.exit(1);
    });
}
