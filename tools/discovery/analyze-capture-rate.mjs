#!/usr/bin/env node

/**
 * ProxyLens Phase 0 — Capture Rate & Snapshot Blind Spot Matching Analyzer
 * 
 * 将 Ground Truth 网络事件与 Mihomo /connections 快照进行确定性关联匹配，
 * 量化分析快照采样盲区 (Snapshot Blind Spot) 与捕获率 (Capture Rate)。
 * 纯事实统计，不修改原始数据。
 */

import fs from 'node:fs';
import path from 'node:path';

function printHelp() {
  console.log(`
Usage:
  node tools/discovery/analyze-capture-rate.mjs <session-directory> <ground-truth.ndjson> [--json]

Example:
  node tools/discovery/analyze-capture-rate.mjs tmp/discovery/work-package-a/capture/direct-500-r1 tmp/gt.ndjson
`);
}

function formatPercent(count, total) {
  if (!total) return '0.0%';
  return ((count / total) * 100).toFixed(1) + '%';
}

async function main() {
  const args = process.argv.slice(2);
  const jsonMode = args.includes('--json');
  const positional = args.filter((a) => !a.startsWith('-'));

  if (positional.length < 2) {
    printHelp();
    process.exit(1);
  }

  const sessionDir = path.resolve(positional[0]);
  const gtFile = path.resolve(positional[1]);

  if (!fs.existsSync(sessionDir) || !fs.existsSync(gtFile)) {
    console.error(`[ERROR] 指定的文件或目录不存在`);
    process.exit(1);
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
  const mihomoSnapshots = []; // list of { frameIndex, receivedAt, conn }
  const connIdObservations = new Map(); // id -> count of frames

  connLines.forEach((line, frameIndex) => {
    let parsed;
    try {
      parsed = JSON.parse(line);
    } catch {
      return;
    }
    const receivedAt = parsed.receivedAt;
    const conns = parsed.frame?.connections || [];

    conns.forEach((c) => {
      mihomoSnapshots.push({ frameIndex, receivedAt, conn: c });
      connIdObservations.set(c.id, (connIdObservations.get(c.id) || 0) + 1);
    });
  });

  // 3. 执行确定性匹配
  const results = [];
  const matchedMihomoIds = new Set();

  let eligibleConnectedCount = 0;
  let completedSuccessCount = 0;

  groundTruths.forEach((gt) => {
    if (gt.eligibleConnected) eligibleConnectedCount++;
    if (gt.success) completedSuccessCount++;

    if (!gt.eligibleConnected) {
      results.push({ gt, status: 'NOT_ELIGIBLE', candidates: [] });
      return;
    }

    const reqTargetHost = new URL(gt.targetUrl).hostname.toLowerCase();
    const reqStartMs = new Date(gt.requestedAt).getTime();
    const reqEndMs = new Date(gt.completedAt || gt.responseAt || gt.requestedAt).getTime();

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

    // 去重为独特的 connection ID 列表
    const uniqueIds = Array.from(new Set(candidates.map((cand) => cand.conn.id)));

    let status = 'MISSED';
    if (uniqueIds.length === 1) {
      status = 'MATCHED';
      matchedMihomoIds.add(uniqueIds[0]);
    } else if (uniqueIds.length > 1) {
      status = 'AMBIGUOUS';
    }

    results.push({
      gt,
      status,
      uniqueIds,
      candidateObservations: candidates.length,
      matchedConn: uniqueIds.length === 1 ? candidates[0].conn : null,
      matchedFrames: uniqueIds.length === 1 ? connIdObservations.get(uniqueIds[0]) : 0,
    });
  });

  const matched = results.filter((r) => r.status === 'MATCHED').length;
  const missed = results.filter((r) => r.status === 'MISSED').length;
  const ambiguous = results.filter((r) => r.status === 'AMBIGUOUS').length;

  const captureRateEligible = eligibleConnectedCount > 0 ? matched / eligibleConnectedCount : 0;
  const captureRateCompleted = completedSuccessCount > 0 ? matched / completedSuccessCount : 0;

  // 4. Snapshot Presence Distribution
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

  // 5. Duration Bins
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

  if (jsonMode) {
    console.log(
      JSON.stringify(
        {
          session: path.basename(sessionDir),
          requestedIntervalMs: manifest.requestedConnectionsIntervalMs,
          eligibleConnectedCount,
          completedSuccessCount,
          matched,
          missed,
          ambiguous,
          captureRateEligible,
          captureRateCompleted,
          presenceDist,
          durationBins,
        },
        null,
        2
      )
    );
    return;
  }

  console.log('===============================================================');
  console.log(`ProxyLens Capture Rate Analysis: ${path.basename(sessionDir)}`);
  console.log('===============================================================');
  console.log(`Requested Connections Interval : ${manifest.requestedConnectionsIntervalMs ? manifest.requestedConnectionsIntervalMs + ' ms' : 'DEFAULT (1000ms)'}`);
  console.log(`Ground Truth Total Requests    : ${groundTruths.length}`);
  console.log(`Eligible Connected (Primary N) : ${eligibleConnectedCount}`);
  console.log(`Completed Success (Secondary N): ${completedSuccessCount}`);
  console.log('---------------------------------------------------------------');
  console.log(`Matching Classification Results:`);
  console.log(`  MATCHED                      : ${matched} / ${eligibleConnectedCount} (${formatPercent(matched, eligibleConnectedCount)})`);
  console.log(`  MISSED (Snapshot Blind Spot) : ${missed} / ${eligibleConnectedCount} (${formatPercent(missed, eligibleConnectedCount)})`);
  console.log(`  AMBIGUOUS (Multiple Matches) : ${ambiguous} / ${eligibleConnectedCount} (${formatPercent(ambiguous, eligibleConnectedCount)})`);
  console.log(`  Capture Rate (Eligible)      : ${formatPercent(matched, eligibleConnectedCount)} (Primary)`);
  console.log(`  Capture Rate (Completed)     : ${formatPercent(matched, completedSuccessCount)} (Secondary)`);
  console.log('---------------------------------------------------------------');
  console.log(`Snapshot Presence Distribution (For MATCHED Connections):`);
  console.log(`  1 Frame Only (Transient)     : ${presenceDist.oneFrame} (${formatPercent(presenceDist.oneFrame, matched)})`);
  console.log(`  2 Frames                     : ${presenceDist.twoFrames} (${formatPercent(presenceDist.twoFrames, matched)})`);
  console.log(`  3+ Frames (Sustained)        : ${presenceDist.threePlusFrames} (${formatPercent(presenceDist.threePlusFrames, matched)})`);
  console.log('---------------------------------------------------------------');
  console.log(`Capture Rate by Application Request Duration:`);
  durationBins.forEach((b) => {
    const rateStr = b.n > 0 ? formatPercent(b.matched, b.n) : 'N/A';
    console.log(`  ${b.label.padEnd(12)}: N=${String(b.n).padStart(3)} | Matched=${String(b.matched).padStart(3)} | Missed=${String(b.missed).padStart(3)} | Capture Rate: ${rateStr}`);
  });
  console.log('===============================================================\n');
}

main().catch((err) => {
  console.error('[FATAL]', err);
  process.exit(1);
});
