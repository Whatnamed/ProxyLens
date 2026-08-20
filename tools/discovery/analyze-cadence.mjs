#!/usr/bin/env node

/**
 * ProxyLens Phase 0 — Cadence & Volume Analyzer
 * 
 * 分析指定 session 目录中 /connections 帧的实际到达节奏 (Cadence) 与原始数据量 (Observation Volume)。
 * 纯事实统计，不修改原始数据。
 */

import fs from 'node:fs';
import path from 'node:path';

function printHelp() {
  console.log(`
Usage:
  node tools/discovery/analyze-cadence.mjs <session-directory>

Example:
  node tools/discovery/analyze-cadence.mjs tmp/discovery/0c1-direct-xxx
`);
}

function percentile(sortedArr, p) {
  if (sortedArr.length === 0) return 0;
  const index = (sortedArr.length - 1) * p;
  const lower = Math.floor(index);
  const upper = Math.ceil(index);
  const weight = index - lower;
  if (upper >= sortedArr.length) return sortedArr[lower];
  return sortedArr[lower] * (1 - weight) + sortedArr[upper] * weight;
}

function formatBytes(bytes) {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return (bytes / Math.pow(k, i)).toFixed(2) + ' ' + sizes[i];
}

async function main() {
  const sessionDir = process.argv[2];
  if (!sessionDir || sessionDir === '-h' || sessionDir === '--help') {
    printHelp();
    process.exit(sessionDir ? 0 : 1);
  }

  const absDir = path.resolve(sessionDir);
  if (!fs.existsSync(absDir)) {
    console.error(`[ERROR] 目录不存在: ${absDir}`);
    process.exit(1);
  }

  const manifestPath = path.join(absDir, 'manifest.json');
  const connPath = path.join(absDir, 'connections.ndjson');

  if (!fs.existsSync(manifestPath) || !fs.existsSync(connPath)) {
    console.error(`[ERROR] 缺少核心文件 (manifest.json 或 connections.ndjson) 于 ${absDir}`);
    process.exit(1);
  }

  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
  const connLines = fs.readFileSync(connPath, 'utf8').trim().split('\n').filter(Boolean);
  const connStat = fs.statSync(connPath);

  const frameTimestamps = [];
  const activeConnCounts = [];

  connLines.forEach((line) => {
    try {
      const p = JSON.parse(line);
      if (p.receivedAt) {
        frameTimestamps.push(new Date(p.receivedAt).getTime());
        activeConnCounts.push(p.frame?.connections?.length || 0);
      }
    } catch {}
  });

  const intervals = [];
  for (let i = 1; i < frameTimestamps.length; i++) {
    intervals.push(frameTimestamps[i] - frameTimestamps[i - 1]);
  }
  intervals.sort((a, b) => a - b);

  const sampleCount = intervals.length;
  const min = sampleCount > 0 ? intervals[0] : 0;
  const max = sampleCount > 0 ? intervals[intervals.length - 1] : 0;
  const mean = sampleCount > 0 ? intervals.reduce((a, b) => a + b, 0) / sampleCount : 0;
  const p50 = percentile(intervals, 0.50);
  const p95 = percentile(intervals, 0.95);

  const totalTimeMs = frameTimestamps.length > 1
    ? frameTimestamps[frameTimestamps.length - 1] - frameTimestamps[0]
    : 0;
  const totalSec = totalTimeMs / 1000;
  const effectiveFps = totalSec > 0 ? sampleCount / totalSec : 0;

  const totalActiveConns = activeConnCounts.reduce((a, b) => a + b, 0);
  const meanActiveConns = activeConnCounts.length > 0 ? totalActiveConns / activeConnCounts.length : 0;
  const rawBytesPerSec = manifest.durationSeconds > 0 ? connStat.size / manifest.durationSeconds : 0;

  console.log('===============================================================');
  console.log(`ProxyLens Cadence & Volume Analysis: ${path.basename(absDir)}`);
  console.log('===============================================================');
  console.log(`Requested Interval (Query) : ${manifest.requestedConnectionsIntervalMs ? manifest.requestedConnectionsIntervalMs + ' ms' : 'DEFAULT (not set)'}`);
  console.log(`Session Duration (Manifest): ${manifest.durationSeconds}s`);
  console.log(`Frames Captured            : ${frameTimestamps.length} frames`);
  console.log(`Health Quality             : ${manifest.evidenceQuality?.isHealthySession ? 'HEALTHY (PASS)' : 'UNHEALTHY (FAIL)'}`);
  console.log('---------------------------------------------------------------');
  console.log(`Actual Inter-Frame Cadence (ms, N=${sampleCount}):`);
  console.log(`  Min Cadence              : ${min} ms`);
  console.log(`  P50 Cadence (Median)     : ${p50.toFixed(1)} ms`);
  console.log(`  Mean Cadence             : ${mean.toFixed(1)} ms`);
  console.log(`  P95 Cadence              : ${p95.toFixed(1)} ms`);
  console.log(`  Max Cadence              : ${max} ms`);
  console.log(`  Effective Frames / Sec   : ${effectiveFps.toFixed(2)} fps`);
  console.log('---------------------------------------------------------------');
  console.log(`Raw Observation Volume:`);
  console.log(`  connections.ndjson Size  : ${formatBytes(connStat.size)} (${connStat.size} Bytes)`);
  console.log(`  Raw Stream Throughput    : ${formatBytes(rawBytesPerSec)}/s`);
  console.log(`  Mean Active Conns/Frame  : ${meanActiveConns.toFixed(1)} connections`);
  console.log('===============================================================\n');
}

main().catch((err) => {
  console.error('[FATAL]', err);
  process.exit(1);
});
