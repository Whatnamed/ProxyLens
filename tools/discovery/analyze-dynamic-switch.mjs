/**
 * analyze-dynamic-switch.mjs
 * 
 * 分析动态节点切换实验中连接的 chains 演变与生命周期行为
 * 
 * 严格输出:
 * 1. oldPathConnection (切换前旧路径连接)
 * 2. postSwitchConnection (切换后新路径连接)
 * 3. crossBoundaryEvidence (切换时间锚点与链路不可变性证据)
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

export async function analyzeDynamicSwitchSession(sessionDir, options = {}) {
  const targetHostSubstring = (options.targetHost || 'githubusercontent').toLowerCase();
  const connPath = path.join(sessionDir, 'connections.ndjson');
  const frames = await parseNdjson(connPath);

  if (frames.length === 0) {
    throw new Error(`No frames found in ${sessionDir}`);
  }

  // 提取与目标 host 相关的所有受控连接 ID
  const relevantConns = new Map();

  frames.forEach((f, frameIdx) => {
    const ts = f.receivedAt || f.timestamp;
    const conns = (f.frame || f.payload)?.connections || [];

    conns.forEach(c => {
      const host = c.metadata?.host || c.metadata?.destinationIP || '';
      const process = c.metadata?.process || '';

      if (host.toLowerCase().includes(targetHostSubstring)) {
        if (!relevantConns.has(c.id)) {
          relevantConns.set(c.id, {
            id: c.id,
            process,
            host,
            start: c.start,
            rule: c.rule,
            rulePayload: c.rulePayload,
            firstFrameIdx: frameIdx,
            firstSeenTs: ts,
            lastFrameIdx: frameIdx,
            lastSeenTs: ts,
            snapshots: []
          });
        }
        const item = relevantConns.get(c.id);
        item.lastFrameIdx = frameIdx;
        item.lastSeenTs = ts;
        item.snapshots.push({
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
      firstFrameIdx: info.firstFrameIdx,
      lastFrameIdx: info.lastFrameIdx,
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

  // 按出现时间排序
  timelineSummary.sort((a, b) => new Date(a.firstSeen).getTime() - new Date(b.firstSeen).getTime());

  let oldPathConn = null;
  let postSwitchConn = null;
  let crossBoundaryEvidence = {
    hasOldAndNewConns: false,
    oldConnRetainedChainThroughoutLifetime: false,
    newConnMigratedToNewNode: false,
    oldConnPresentAfterNewConnFirstSeen: false
  };

  if (timelineSummary.length >= 2) {
    oldPathConn = timelineSummary[0];
    postSwitchConn = timelineSummary[timelineSummary.length - 1];

    const isOldStable = !oldPathConn.isChainsMutatedDuringLifetime;
    const isDifferentChains = JSON.stringify(oldPathConn.initialChains) !== JSON.stringify(postSwitchConn.initialChains);
    const isOldActiveAfterNewStart = new Date(oldPathConn.lastSeen).getTime() >= new Date(postSwitchConn.firstSeen).getTime();

    crossBoundaryEvidence = {
      hasOldAndNewConns: true,
      oldConnRetainedChainThroughoutLifetime: isOldStable,
      newConnMigratedToNewNode: isDifferentChains,
      oldConnPresentAfterNewConnFirstSeen: isOldActiveAfterNewStart,
      oldPathChain: oldPathConn.initialChains,
      postSwitchChain: postSwitchConn.initialChains
    };
  }

  return {
    sessionDir,
    totalFrames: frames.length,
    trackedConnectionsCount: timelineSummary.length,
    oldPathConnection: oldPathConn,
    postSwitchConnection: postSwitchConn,
    crossBoundaryEvidence,
    connections: timelineSummary
  };
}

if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const args = process.argv.slice(2);
  const jsonMode = args.includes('--json');
  const sessionDir = args.find(a => !a.startsWith('-'));

  if (!sessionDir) {
    console.error('Usage: node tools/discovery/analyze-dynamic-switch.mjs <sessionDir> [--json]');
    process.exit(1);
  }

  analyzeDynamicSwitchSession(path.resolve(sessionDir))
    .then(result => {
      if (jsonMode) {
        console.log(JSON.stringify(result, null, 2));
        return;
      }

      console.log('================================================================');
      console.log('STAGE R3: DYNAMIC SWITCH SESSION ANALYSIS');
      console.log('================================================================');
      console.log(`Session Dir               : ${result.sessionDir}`);
      console.log(`Total Snapshot Frames     : ${result.totalFrames}`);
      console.log(`Tracked Relevant Conns    : ${result.trackedConnectionsCount}`);
      console.log('----------------------------------------------------------------');
      console.log('CROSS-BOUNDARY EVIDENCE:');
      console.log(`  Has Old & New Conns     : ${result.crossBoundaryEvidence.hasOldAndNewConns}`);
      console.log(`  Old Conn Stable Chains  : ${result.crossBoundaryEvidence.oldConnRetainedChainThroughoutLifetime}`);
      console.log(`  New Conn Migrated Egress: ${result.crossBoundaryEvidence.newConnMigratedToNewNode}`);
      console.log(`  Old Chain Pattern       : ${JSON.stringify(result.crossBoundaryEvidence.oldPathChain || [])}`);
      console.log(`  New Chain Pattern       : ${JSON.stringify(result.crossBoundaryEvidence.postSwitchChain || [])}`);
      console.log('----------------------------------------------------------------');

      result.connections.forEach((c, idx) => {
        console.log(`[Connection #${idx + 1}] ID: ${c.id.slice(0, 8)}... (${c.process} -> ${c.host})`);
        console.log(`  Rule (Payload)          : ${c.rule} ("${c.rulePayload}")`);
        console.log(`  Start Time              : ${c.start}`);
        console.log(`  Frames Observed         : ${c.snapshotsCount} frames (${c.firstSeen.slice(11, 23)} -> ${c.lastSeen.slice(11, 23)})`);
        console.log(`  Chains at Creation      : ${JSON.stringify(c.initialChains)}`);
        console.log(`  Chains at Final Snapshot: ${JSON.stringify(c.finalChains)}`);
        console.log(`  Chains Mutated In-flight: ${c.isChainsMutatedDuringLifetime ? 'YES [MUTATED!]' : 'NO [STABLE]'}`);
        console.log('----------------------------------------------------------------');
      });
      console.log('================================================================\n');
    })
    .catch(err => {
      console.error(err);
      process.exit(1);
    });
}
