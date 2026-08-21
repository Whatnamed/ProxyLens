#!/usr/bin/env node

/**
 * ProxyLens Phase 0 — Capture Rate & Snapshot Blind Spot Matching Analyzer
 * 
 * 将 Ground Truth 网络事件与 Mihomo /connections 快照进行确定性关联匹配，
 * 量化分析快照采样盲区 (Snapshot Blind Spot) 与捕获率 (Capture Rate)。
 * 
 * 包含严密的 1-to-1 映射、Probe 窗口越界检测与 Raw chains 路由验证 Gate。
 */

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

function printHelp() {
  console.log(`
Usage:
  node tools/discovery/analyze-capture-rate.mjs <session-directory> <ground-truth.ndjson> [--expected-route <direct|proxy>] [--json]

Example:
  node tools/discovery/analyze-capture-rate.mjs tmp/discovery/work-package-a/capture/direct-500-r1 tmp/gt.ndjson --expected-route direct --json
`);
}

function formatPercent(count, total) {
  if (!total) return '0.0%';
  return ((count / total) * 100).toFixed(1) + '%';
}

export function classifyRawRoute(chains) {
  if (!Array.isArray(chains) || chains.length === 0) {
    return 'UNKNOWN';
  }
  if (chains.length === 1 && chains[0] === 'DIRECT') {
    return 'DIRECT';
  }
  if (chains[0] === 'REJECT') {
    return 'REJECT';
  }
  if (chains.length > 0 && chains[0] !== 'DIRECT') {
    return 'PROXY';
  }
  return 'UNKNOWN';
}

export async function analyzeCaptureSession(sessionDir, gtFile, options = {}) {
  const expectedRoute = options.expectedRoute ? options.expectedRoute.toUpperCase() : null;

  if (!fs.existsSync(sessionDir) || !fs.existsSync(gtFile)) {
    throw new Error(`Session directory or Ground Truth file not found: ${sessionDir}, ${gtFile}`);
  }

  const manifestPath = path.join(sessionDir, 'manifest.json');
  const connPath = path.join(sessionDir, 'connections.ndjson');

  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
  const connLines = fs.readFileSync(connPath, 'utf8').trim().split('\n').filter(Boolean);
  const gtLines = fs.readFileSync(gtFile, 'utf8').trim().split('\n').filter(Boolean);

  // 1. 解析 Ground Truth
  const groundTruths = [];
  gtLines.forEach((line) => {
    try {
      groundTruths.push(JSON.parse(line));
    } catch {}
  });

  // 2. 解析 Mihomo 快照并按 sourcePort 索引
  const mihomoSnapshots = [];
  const connIdObservations = new Map();

  connLines.forEach((line, frameIndex) => {
    let parsed;
    try {
      parsed = JSON.parse(line);
    } catch {
      return;
    }
    const receivedAt = parsed.receivedAt || parsed.timestamp;
    const conns = (parsed.frame || parsed.payload)?.connections || [];

    conns.forEach((c) => {
      mihomoSnapshots.push({ frameIndex, receivedAt, conn: c });
      connIdObservations.set(c.id, (connIdObservations.get(c.id) || 0) + 1);
    });
  });

  const firstConnFrame = JSON.parse(connLines[0]);
  const lastConnFrame = JSON.parse(connLines[connLines.length - 1]);
  const probeStartMs = new Date(firstConnFrame.receivedAt || firstConnFrame.timestamp).getTime();
  const probeEndMs = new Date(lastConnFrame.receivedAt || lastConnFrame.timestamp).getTime();

  // 3. 执行确定性匹配与互斥映射
  const results = [];
  const matchedMihomoIds = new Set();
  const mihomoIdToGtIndex = new Map();

  let eligibleConnectedCount = 0;
  let completedSuccessCount = 0;
  let windowViolations = 0;
  let oneToOneConflicts = 0;

  groundTruths.forEach((gt, gtIdx) => {
    if (gt.eligibleConnected) eligibleConnectedCount++;
    if (gt.success) completedSuccessCount++;

    if (!gt.eligibleConnected) {
      results.push({ gt, status: 'NOT_ELIGIBLE', candidates: [] });
      return;
    }

    const reqTargetHost = new URL(gt.targetUrl).hostname.toLowerCase();
    const reqStartMs = new Date(gt.requestedAt).getTime();
    const reqEndMs = new Date(gt.completedAt || gt.responseAt || gt.requestedAt).getTime();

    // 检查是否超出 Probe 监控有效窗口
    if (reqStartMs < probeStartMs || reqEndMs > probeEndMs) {
      windowViolations++;
    }

    // 匹配候选
    const candidates = mihomoSnapshots.filter((snap) => {
      const c = snap.conn;
      const meta = c.metadata || {};

      // 1. sourcePort 必须完全匹配
      if (String(meta.sourcePort) !== String(gt.localPort)) return false;

      // 2. 进程必须为 node
      if (!meta.process?.toLowerCase()?.includes('node')) return false;

      // 3. 目标 Host 匹配
      if (meta.host && !meta.host.toLowerCase().includes(reqTargetHost) && !reqTargetHost.includes(meta.host.toLowerCase())) {
        return false;
      }

      // 4. 时间窗口合理 (requestedAt - 2s 到 completedAt + 5s)
      const snapTimeMs = new Date(snap.receivedAt).getTime();
      if (snapTimeMs < reqStartMs - 2000 || snapTimeMs > reqEndMs + 5000) {
        return false;
      }

      return true;
    });

    const uniqueIds = Array.from(new Set(candidates.map((cand) => cand.conn.id)));

    let status = 'MISSED';
    let matchedConn = null;
    let actualRoute = null;

    if (uniqueIds.length === 1) {
      const targetId = uniqueIds[0];
      if (mihomoIdToGtIndex.has(targetId) && mihomoIdToGtIndex.get(targetId) !== gtIdx) {
        status = 'AMBIGUOUS';
        oneToOneConflicts++;
      } else {
        status = 'MATCHED';
        matchedMihomoIds.add(targetId);
        mihomoIdToGtIndex.set(targetId, gtIdx);
        matchedConn = candidates[0].conn;
        actualRoute = classifyRawRoute(matchedConn.chains);
      }
    } else if (uniqueIds.length > 1) {
      status = 'AMBIGUOUS';
    }

    results.push({
      gt,
      status,
      uniqueIds,
      candidateObservations: candidates.length,
      matchedConn,
      actualRoute,
      matchedFrames: uniqueIds.length === 1 ? connIdObservations.get(uniqueIds[0]) : 0,
    });
  });

  const matched = results.filter((r) => r.status === 'MATCHED').length;
  const missed = results.filter((r) => r.status === 'MISSED').length;
  const ambiguous = results.filter((r) => r.status === 'AMBIGUOUS').length;

  const captureRateEligible = eligibleConnectedCount > 0 ? matched / eligibleConnectedCount : 0;
  const captureRateCompleted = completedSuccessCount > 0 ? matched / completedSuccessCount : 0;

  // 4. 路由验证统计
  let matchedWithKnownRoute = 0;
  let routeMismatches = 0;
  let routeUnknown = 0;

  results.forEach((r) => {
    if (r.status === 'MATCHED') {
      if (r.actualRoute === 'UNKNOWN' || r.actualRoute === 'REJECT') {
        routeUnknown++;
      } else {
        matchedWithKnownRoute++;
        if (expectedRoute && r.actualRoute !== expectedRoute) {
          routeMismatches++;
        }
      }
    }
  });

  // 5. Snapshot Presence Distribution
  const presenceDist = {
    oneFrame: 0,
    twoFrames: 0,
    threePlusFrames: 0,
  };

  results.forEach((r) => {
    if (r.status === 'MATCHED') {
      const frames = r.matchedFrames;
      if (frames === 1) presenceDist.oneFrame++;
      else if (frames === 2) presenceDist.twoFrames++;
      else if (frames >= 3) presenceDist.threePlusFrames++;
    }
  });

  // 6. Duration Bins
  const durationBins = [
    { label: '<100ms', min: 0, max: 100, n: 0, matched: 0, missed: 0, ambiguous: 0 },
    { label: '100–250ms', min: 100, max: 250, n: 0, matched: 0, missed: 0, ambiguous: 0 },
    { label: '250–500ms', min: 250, max: 500, n: 0, matched: 0, missed: 0, ambiguous: 0 },
    { label: '500–1000ms', min: 500, max: 1000, n: 0, matched: 0, missed: 0, ambiguous: 0 },
    { label: '>=1000ms', min: 1000, max: Infinity, n: 0, matched: 0, missed: 0, ambiguous: 0 },
  ];

  results.forEach((r) => {
    if (r.gt.eligibleConnected) {
      const d = r.gt.durationMs ?? 0;
      const bin = durationBins.find((b) => d >= b.min && d < b.max);
      if (bin) {
        bin.n++;
        if (r.status === 'MATCHED') bin.matched++;
        else if (r.status === 'MISSED') bin.missed++;
        else if (r.status === 'AMBIGUOUS') bin.ambiguous++;
      }
    }
  });

  return {
    session: path.basename(sessionDir),
    requestedIntervalMs: manifest.requestedConnectionsIntervalMs,
    eligibleConnectedCount,
    completedSuccessCount,
    windowViolations,
    matched,
    missed,
    ambiguous,
    oneToOneConflicts,
    routeVerification: {
      expectedRoute,
      matchedWithKnownRoute,
      routeMismatches,
      routeUnknown,
    },
    captureRateEligible,
    captureRateCompleted,
    presenceDist,
    durationBins,
  };
}

// CLI 执行
if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const args = process.argv.slice(2);
  const jsonMode = args.includes('--json');
  let expectedRoute = null;

  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--expected-route' && args[i + 1]) {
      expectedRoute = args[++i];
    }
  }

  const positional = args.filter((a, idx) => {
    if (a.startsWith('-')) return false;
    if (idx > 0 && args[idx - 1] === '--expected-route') return false;
    return true;
  });

  if (positional.length < 2) {
    printHelp();
    process.exit(1);
  }

  const sessionDir = path.resolve(positional[0]);
  const gtFile = path.resolve(positional[1]);

  analyzeCaptureSession(sessionDir, gtFile, { expectedRoute })
    .then((res) => {
      if (jsonMode) {
        console.log(JSON.stringify(res, null, 2));
        return;
      }

      console.log('===============================================================');
      console.log(`ProxyLens Capture Rate Analysis: ${res.session}`);
      console.log('===============================================================');
      console.log(`Requested Connections Interval : ${res.requestedIntervalMs ? res.requestedIntervalMs + ' ms' : 'DEFAULT (1000ms)'}`);
      console.log(`Eligible Connected (Primary N) : ${res.eligibleConnectedCount}`);
      console.log(`Completed Success (Secondary N): ${res.completedSuccessCount}`);
      console.log(`Window Out-of-Bound Violations : ${res.windowViolations} ${res.windowViolations > 0 ? '[WARN]' : '[VALID]'}`);
      console.log(`1-to-1 Mapping Conflicts       : ${res.oneToOneConflicts} ${res.oneToOneConflicts > 0 ? '[CONFLICT]' : '[VALID]'}`);
      console.log('---------------------------------------------------------------');
      console.log(`Matching Classification Results:`);
      console.log(`  MATCHED                      : ${res.matched} / ${res.eligibleConnectedCount} (${formatPercent(res.matched, res.eligibleConnectedCount)})`);
      console.log(`  MISSED (Snapshot Blind Spot) : ${res.missed} / ${res.eligibleConnectedCount} (${formatPercent(res.missed, res.eligibleConnectedCount)})`);
      console.log(`  AMBIGUOUS (Multiple Matches) : ${res.ambiguous} / ${res.eligibleConnectedCount} (${formatPercent(res.ambiguous, res.eligibleConnectedCount)})`);
      console.log(`  Capture Rate (Eligible)      : ${formatPercent(res.matched, res.eligibleConnectedCount)} (Primary)`);
      console.log('---------------------------------------------------------------');
      console.log(`Route Verification (Raw chains evidence):`);
      console.log(`  Expected Route               : ${res.routeVerification.expectedRoute || 'N/A (Not specified)'}`);
      console.log(`  Matched with Known Route     : ${res.routeVerification.matchedWithKnownRoute}`);
      console.log(`  Route Mismatches             : ${res.routeVerification.routeMismatches} ${res.routeVerification.routeMismatches > 0 ? '[MISMATCH GATE FAILED]' : '[VALID]'}`);
      console.log(`  Route Unknown / Reject       : ${res.routeVerification.routeUnknown}`);
      console.log('---------------------------------------------------------------');
      console.log(`Snapshot Presence Distribution (For MATCHED Connections):`);
      console.log(`  1 Frame Only (Transient)     : ${res.presenceDist.oneFrame} (${formatPercent(res.presenceDist.oneFrame, res.matched)})`);
      console.log(`  2 Frames                     : ${res.presenceDist.twoFrames} (${formatPercent(res.presenceDist.twoFrames, res.matched)})`);
      console.log(`  3+ Frames (Sustained)        : ${res.presenceDist.threePlusFrames} (${formatPercent(res.presenceDist.threePlusFrames, res.matched)})`);
      console.log('===============================================================\n');
    })
    .catch((err) => {
      console.error('[FATAL]', err);
      process.exit(1);
    });
}
