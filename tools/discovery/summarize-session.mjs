#!/usr/bin/env node

/**
 * ProxyLens Phase 0 — Discovery Session Summarizer
 * 
 * 离线分析指定 discovery session 目录的原始数据，输出纯事实统计。
 * 严格只读，不修改样本，不硬编码业务推断。
 */

import fs from 'node:fs';
import path from 'node:path';

function printHelp() {
  console.log(`
ProxyLens Phase 0 — Session Summarizer

Usage:
  node tools/discovery/summarize-session.mjs <session-directory> [options]

Options:
  --network <tcp|udp>    仅统计特定传输层协议连接
  --port <port>          仅统计特定目标端口连接
  --process <name>       仅统计特定进程名连接 (包含子串匹配)
  -h, --help             显示帮助信息

Examples:
  node tools/discovery/summarize-session.mjs tmp/discovery/0c0-live-gate-2026-08-20T15-26-37-119Z
  node tools/discovery/summarize-session.mjs tmp/discovery/0c2-idle-udp-xxx --network udp
  node tools/discovery/summarize-session.mjs tmp/discovery/0c2-idle-udp-xxx --port 123
`);
}

function parseArgs() {
  const args = process.argv.slice(2);
  let sessionDir = '';
  const filters = {
    network: null,
    port: null,
    process: null,
  };

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === '--network') {
      filters.network = args[++i]?.toLowerCase();
    } else if (arg === '--port') {
      filters.port = args[++i];
    } else if (arg === '--process') {
      filters.process = args[++i]?.toLowerCase();
    } else if (arg === '-h' || arg === '--help') {
      printHelp();
      process.exit(0);
    } else if (!sessionDir && !arg.startsWith('-')) {
      sessionDir = arg;
    }
  }

  return { sessionDir, filters };
}

function formatPercent(count, total) {
  if (!total) return '0.0%';
  return ((count / total) * 100).toFixed(1) + '%';
}

async function main() {
  const { sessionDir, filters } = parseArgs();
  if (!sessionDir) {
    printHelp();
    process.exit(1);
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

  console.log('===============================================================');
  console.log(`ProxyLens Session Summary: ${path.basename(absDir)}`);
  if (filters.network || filters.port || filters.process) {
    console.log(`Filters Applied : ${JSON.stringify(filters)}`);
  }
  console.log('===============================================================');
  console.log(`Probe Version   : ${manifest.probeVersion}`);
  console.log(`Mihomo Version  : ${JSON.stringify(manifest.preflight?.mihomoVersion || manifest.mihomoVersion)}`);
  console.log(`Session Status  : ${manifest.status}`);
  console.log(`Duration (sec)  : ${manifest.durationSeconds}s`);
  console.log(`Health Quality  : ${manifest.evidenceQuality?.isHealthySession ? 'HEALTHY (PASS)' : 'UNHEALTHY (FAIL)'}`);
  if (manifest.evidenceQuality?.issues?.length > 0) {
    console.log(`Issues Encountered:`);
    manifest.evidenceQuality.issues.forEach((iss) => console.log(`  - ${iss}`));
  }
  console.log(`Connections Frames: ${manifest.channels?.connections?.framesReceived ?? manifest.stats?.connectionsFrames}`);
  console.log(`Traffic Frames    : ${manifest.channels?.traffic?.framesReceived ?? manifest.stats?.trafficFrames}`);

  // 解析 connections.ndjson
  const connLines = fs.readFileSync(connPath, 'utf8').trim().split('\n').filter(Boolean);
  
  const connectionsMap = new Map(); // id -> tracker
  let allRawObservations = 0;
  let matchedRawObservations = 0;

  connLines.forEach((line) => {
    let parsed;
    try {
      parsed = JSON.parse(line);
    } catch {
      return;
    }
    const receivedAt = parsed.receivedAt;
    const conns = parsed.frame?.connections || [];

    conns.forEach((c) => {
      allRawObservations++;
      const id = c.id;
      const meta = c.metadata || {};

      // 过滤器应用
      if (filters.network && meta.network?.toLowerCase() !== filters.network) return;
      if (filters.port && String(meta.destinationPort) !== String(filters.port)) return;
      if (filters.process && !meta.process?.toLowerCase()?.includes(filters.process)) return;

      matchedRawObservations++;

      if (!connectionsMap.has(id)) {
        connectionsMap.set(id, {
          id,
          firstSeen: receivedAt,
          lastSeen: receivedAt,
          firstUpload: c.upload ?? 0,
          firstDownload: c.download ?? 0,
          lastUpload: c.upload ?? 0,
          lastDownload: c.download ?? 0,
          decreasedUpload: false,
          decreasedDownload: false,
          sample: c,
        });
      } else {
        const item = connectionsMap.get(id);
        item.lastSeen = receivedAt;
        if ((c.upload ?? 0) < item.lastUpload) item.decreasedUpload = true;
        if ((c.download ?? 0) < item.lastDownload) item.decreasedDownload = true;
        item.lastUpload = c.upload ?? 0;
        item.lastDownload = c.download ?? 0;
      }
    });
  });

  const uniqueCount = connectionsMap.size;
  console.log(`\n--- Connection Statistics ---`);
  console.log(`Unique Connection IDs Observed : ${uniqueCount}`);
  if (filters.network || filters.port || filters.process) {
    console.log(`Matched Connection Snapshots   : ${matchedRawObservations} (out of ${allRawObservations} total)`);
  } else {
    console.log(`Total Connection Snapshots     : ${allRawObservations}`);
  }

  if (uniqueCount === 0) {
    console.log('\n(No matching connection records observed in this session)');
    return;
  }

  // 统计各字段非空覆盖率 (基于 unique connection)
  const coverage = {
    process: 0,
    processPath: 0,
    host: 0,
    sniffHost: 0,
    destinationIP: 0,
    remoteDestination: 0,
    destinationPort: 0,
    network: 0,
    rule: 0,
    rulePayload: 0,
    chains: 0,
    hasUploadOrDownload: 0,
  };

  const networks = new Map();
  const rules = new Map();
  const chainsForms = new Map();
  const processes = new Map();
  const ports = new Map();
  const hosts = new Map();
  let counterDecreasedCount = 0;

  connectionsMap.forEach((item) => {
    const c = item.sample;
    const meta = c.metadata || {};

    if (meta.process && meta.process.trim() !== '') coverage.process++;
    if (meta.processPath && meta.processPath.trim() !== '') coverage.processPath++;
    if (meta.host && meta.host.trim() !== '') coverage.host++;
    if (meta.sniffHost && meta.sniffHost.trim() !== '') coverage.sniffHost++;
    if (meta.destinationIP && meta.destinationIP.trim() !== '') coverage.destinationIP++;
    if (meta.remoteDestination && meta.remoteDestination.trim() !== '') coverage.remoteDestination++;
    if (meta.destinationPort) coverage.destinationPort++;
    if (meta.network) coverage.network++;
    if (c.rule && c.rule.trim() !== '') coverage.rule++;
    if (c.rulePayload && c.rulePayload.trim() !== '') coverage.rulePayload++;
    if (Array.isArray(c.chains) && c.chains.length > 0) coverage.chains++;
    if (item.lastUpload > 0 || item.lastDownload > 0) coverage.hasUploadOrDownload++;

    if (item.decreasedUpload || item.decreasedDownload) counterDecreasedCount++;

    // 分布计数
    const netKey = meta.network || '(empty)';
    networks.set(netKey, (networks.get(netKey) || 0) + 1);

    const ruleKey = c.rule || '(empty)';
    rules.set(ruleKey, (rules.get(ruleKey) || 0) + 1);

    const chainKey = Array.isArray(c.chains) ? JSON.stringify(c.chains) : '(not-array)';
    chainsForms.set(chainKey, (chainsForms.get(chainKey) || 0) + 1);

    const procKey = meta.process || '(empty-process)';
    processes.set(procKey, (processes.get(procKey) || 0) + 1);

    const portKey = String(meta.destinationPort || '(empty-port)');
    ports.set(portKey, (ports.get(portKey) || 0) + 1);

    const targetKey = meta.host || meta.destinationIP || meta.remoteDestination || '(empty-target)';
    hosts.set(targetKey, (hosts.get(targetKey) || 0) + 1);
  });

  console.log(`\n--- Raw Field Coverage (Unique Connections: ${uniqueCount}) ---`);
  console.log(`metadata.process           : ${coverage.process} / ${uniqueCount} (${formatPercent(coverage.process, uniqueCount)})`);
  console.log(`metadata.processPath       : ${coverage.processPath} / ${uniqueCount} (${formatPercent(coverage.processPath, uniqueCount)})`);
  console.log(`metadata.host              : ${coverage.host} / ${uniqueCount} (${formatPercent(coverage.host, uniqueCount)})`);
  console.log(`metadata.sniffHost         : ${coverage.sniffHost} / ${uniqueCount} (${formatPercent(coverage.sniffHost, uniqueCount)})`);
  console.log(`metadata.destinationIP     : ${coverage.destinationIP} / ${uniqueCount} (${formatPercent(coverage.destinationIP, uniqueCount)})`);
  console.log(`metadata.remoteDestination : ${coverage.remoteDestination} / ${uniqueCount} (${formatPercent(coverage.remoteDestination, uniqueCount)})`);
  console.log(`metadata.destinationPort   : ${coverage.destinationPort} / ${uniqueCount} (${formatPercent(coverage.destinationPort, uniqueCount)})`);
  console.log(`metadata.network           : ${coverage.network} / ${uniqueCount} (${formatPercent(coverage.network, uniqueCount)})`);
  console.log(`rule                       : ${coverage.rule} / ${uniqueCount} (${formatPercent(coverage.rule, uniqueCount)})`);
  console.log(`rulePayload                : ${coverage.rulePayload} / ${uniqueCount} (${formatPercent(coverage.rulePayload, uniqueCount)})`);
  console.log(`chains (non-empty array)   : ${coverage.chains} / ${uniqueCount} (${formatPercent(coverage.chains, uniqueCount)})`);
  console.log(`has traffic (>0 bytes)     : ${coverage.hasUploadOrDownload} / ${uniqueCount} (${formatPercent(coverage.hasUploadOrDownload, uniqueCount)})`);
  console.log(`counter decreased cases    : ${counterDecreasedCount} / ${uniqueCount}`);

  console.log(`\n--- Network Distribution ---`);
  for (const [k, v] of networks.entries()) {
    console.log(`  ${k.padEnd(12)}: ${v} (${formatPercent(v, uniqueCount)})`);
  }

  console.log(`\n--- Top Destination Ports ---`);
  const sortedPorts = Array.from(ports.entries()).sort((a, b) => b[1] - a[1]).slice(0, 10);
  sortedPorts.forEach(([k, v]) => {
    console.log(`  Port ${k.padEnd(8)}: ${v} (${formatPercent(v, uniqueCount)})`);
  });

  console.log(`\n--- Rule Distribution ---`);
  for (const [k, v] of rules.entries()) {
    console.log(`  ${k.padEnd(20)}: ${v} (${formatPercent(v, uniqueCount)})`);
  }

  console.log(`\n--- Top Chains Forms Observed ---`);
  const sortedChains = Array.from(chainsForms.entries()).sort((a, b) => b[1] - a[1]).slice(0, 10);
  sortedChains.forEach(([k, v]) => {
    console.log(`  ${String(v).padStart(3)} times: ${k}`);
  });

  console.log(`\n--- Top Processes Observed ---`);
  const sortedProcs = Array.from(processes.entries()).sort((a, b) => b[1] - a[1]).slice(0, 10);
  sortedProcs.forEach(([k, v]) => {
    console.log(`  ${String(v).padStart(3)} times: ${k}`);
  });

  console.log(`\n--- Top Target Hosts / IPs Observed ---`);
  const sortedHosts = Array.from(hosts.entries()).sort((a, b) => b[1] - a[1]).slice(0, 10);
  sortedHosts.forEach(([k, v]) => {
    console.log(`  ${String(v).padStart(3)} times: ${k}`);
  });
  console.log('===============================================================\n');
}

main().catch((err) => {
  console.error('[FATAL]', err);
  process.exit(1);
});
