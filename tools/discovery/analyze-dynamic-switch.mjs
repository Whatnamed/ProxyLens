/**
 * analyze-dynamic-switch.mjs
 * 
 * 分析动态节点切换实验中连接的 chains 演变与生命周期行为
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

export async function analyzeDynamicSwitchSession(sessionDir) {
  const connPath = path.join(sessionDir, 'connections.ndjson');
  const frames = await parseNdjson(connPath);

  if (frames.length === 0) {
    throw new Error(`No frames found in ${sessionDir}`);
  }

  // 提取与目标 host (githubusercontent.com) 相关的所有连接 ID
  const relevantConns = new Map(); // id -> { id, frames: [ { receivedAt, upload, download, chains, rule } ] }

  frames.forEach((f, frameIdx) => {
    const ts = f.receivedAt || f.timestamp;
    const conns = (f.frame || f.payload)?.connections || [];

    conns.forEach(c => {
      const host = c.metadata?.host || c.metadata?.destinationIP || '';
      const process = c.metadata?.process || '';

      if (host.toLowerCase().includes('githubusercontent') || process.toLowerCase().includes('curl')) {
        if (!relevantConns.has(c.id)) {
          relevantConns.set(c.id, {
            id: c.id,
            process,
            host,
            start: c.start,
            rule: c.rule,
            rulePayload: c.rulePayload,
            snapshots: []
          });
        }
        relevantConns.get(c.id).snapshots.push({
          frameIdx,
          receivedAt: ts,
          upload: c.upload,
          download: c.download,
          chains: c.chains
        });
      }
    });
  });

  const timelineSummary = [];

  for (const [id, info] of relevantConns.entries()) {
    const firstSnap = info.snapshots[0];
    const lastSnap = info.snapshots[info.snapshots.length - 1];
    const distinctChains = Array.from(new Set(info.snapshots.map(s => JSON.stringify(s.chains))));

    timelineSummary.push({
      id,
      process: info.process,
      host: info.host,
      rule: info.rule,
      rulePayload: info.rulePayload,
      start: info.start,
      snapshotsCount: info.snapshots.length,
      firstSeen: firstSnap.receivedAt,
      lastSeen: lastSnap.receivedAt,
      initialChains: firstSnap.chains,
      finalChains: lastSnap.chains,
      isChainsMutatedDuringLifetime: distinctChains.length > 1,
      distinctChains: distinctChains.map(c => JSON.parse(c)),
      totalUpload: lastSnap.upload,
      totalDownload: lastSnap.download
    });
  }

  return {
    sessionDir,
    totalFrames: frames.length,
    trackedConnectionsCount: timelineSummary.length,
    connections: timelineSummary
  };
}

if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const sessionDir = process.argv[2];
  if (!sessionDir) {
    console.error('Usage: node tools/discovery/analyze-dynamic-switch.mjs <sessionDir>');
    process.exit(1);
  }

  analyzeDynamicSwitchSession(path.resolve(sessionDir))
    .then(result => {
      console.log('================================================================');
      console.log('STAGE B1: DYNAMIC SWITCH SESSION ANALYSIS');
      console.log('================================================================');
      console.log(`Session Dir               : ${result.sessionDir}`);
      console.log(`Total Snapshot Frames     : ${result.totalFrames}`);
      console.log(`Tracked Relevant Conns    : ${result.trackedConnectionsCount}`);
      console.log('----------------------------------------------------------------');

      result.connections.forEach((c, idx) => {
        console.log(`[Connection #${idx + 1}] ID: ${c.id.slice(0, 8)}... (${c.process} -> ${c.host})`);
        console.log(`  Rule (Payload)          : ${c.rule} ("${c.rulePayload}")`);
        console.log(`  Start Time              : ${c.start}`);
        console.log(`  Snapshots Observed      : ${c.snapshotsCount} frames (${c.firstSeen.slice(11, 23)} -> ${c.lastSeen.slice(11, 23)})`);
        console.log(`  Chains at Creation      : ${JSON.stringify(c.initialChains)}`);
        console.log(`  Chains at Final Snapshot: ${JSON.stringify(c.finalChains)}`);
        console.log(`  Chains Mutated In-flight: ${c.isChainsMutatedDuringLifetime ? 'YES [MUTATED!]' : 'NO [STABLE THROUGH LIFETIME]'}`);
        console.log(`  Total Transferred       : Up ${c.totalUpload.toLocaleString()} B, Down ${c.totalDownload.toLocaleString()} B`);
        console.log('----------------------------------------------------------------');
      });
    })
    .catch(err => {
      console.error(err);
      process.exit(1);
    });
}
