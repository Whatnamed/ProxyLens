#!/usr/bin/env node

/**
 * ProxyLens Phase 0 — Connection Timeline Analyzer
 * 
 * 专门提取指定 session 目录中特定连接 ID (或匹配进程/host) 在每一帧快照中的完整演变时间线。
 * 纯事实统计，不修改原始数据，不进行推测性平滑。
 */

import fs from 'node:fs';
import path from 'node:path';

function printHelp() {
  console.log(`
ProxyLens Phase 0 — Connection Timeline Analyzer

Usage:
  node tools/discovery/trace-connection.mjs <session-directory> [options]

Options:
  --id <connection-id>   精确追踪指定 Connection ID 的演变时间线 (首选)
  --process <name>       按进程名过滤候选 ID (若未指定 --id)
  --host <host>          按目标域名过滤候选 ID (若未指定 --id)
  -h, --help             显示帮助信息

Examples:
  node tools/discovery/trace-connection.mjs tmp/discovery/0c3a-steady-xxx --id 8d3b4047-...
  node tools/discovery/trace-connection.mjs tmp/discovery/0c3a-steady-xxx --process curl
`);
}

function parseArgs() {
  const args = process.argv.slice(2);
  let sessionDir = '';
  const options = {
    id: null,
    process: null,
    host: null,
  };

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === '--id') {
      options.id = args[++i];
    } else if (arg === '--process') {
      options.process = args[++i]?.toLowerCase();
    } else if (arg === '--host') {
      options.host = args[++i]?.toLowerCase();
    } else if (arg === '-h' || arg === '--help') {
      printHelp();
      process.exit(0);
    } else if (!sessionDir && !arg.startsWith('-')) {
      sessionDir = arg;
    }
  }

  return { sessionDir, options };
}

function formatBytes(bytes) {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return (bytes / Math.pow(k, i)).toFixed(2) + ' ' + sizes[i];
}

async function main() {
  const { sessionDir, options } = parseArgs();
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
  const connLines = fs.readFileSync(connPath, 'utf8').trim().split('\n').filter(Boolean);

  // 1. 如果没有指定 --id，先扫描匹配的候选 ID 列表
  if (!options.id) {
    const candidates = new Map();
    connLines.forEach((line) => {
      let parsed;
      try {
        parsed = JSON.parse(line);
      } catch {
        return;
      }
      const conns = parsed.frame?.connections || [];
      conns.forEach((c) => {
        const meta = c.metadata || {};
        let match = true;
        if (options.process && !meta.process?.toLowerCase()?.includes(options.process)) match = false;
        if (options.host && !meta.host?.toLowerCase()?.includes(options.host)) match = false;
        if (match) {
          if (!candidates.has(c.id)) {
            candidates.set(c.id, {
              id: c.id,
              process: meta.process,
              host: meta.host || meta.destinationIP,
              start: c.start,
              frames: 1,
              maxDownload: c.download ?? 0,
            });
          } else {
            const item = candidates.get(c.id);
            item.frames++;
            if ((c.download ?? 0) > item.maxDownload) item.maxDownload = c.download ?? 0;
          }
        }
      });
    });

    if (candidates.size === 0) {
      console.log(`[INFO] 未找到匹配的连接候选。已指定过滤器: ${JSON.stringify(options)}`);
      return;
    }

    if (candidates.size > 1) {
      console.log(`\n找到 ${candidates.size} 个候选连接 ID，请指定 --id 进行完整时间线追踪:\n`);
      candidates.forEach((cand) => {
        console.log(`  ID: ${cand.id}`);
        console.log(`    Process: ${cand.process} | Target: ${cand.host} | Frames: ${cand.frames} | Max Download: ${formatBytes(cand.maxDownload)} | Start: ${cand.start}`);
      });
      console.log(`\n使用命令: node tools/discovery/trace-connection.mjs ${sessionDir} --id <chosen-id>\n`);
      return;
    }

    // 只有一个候选，直接使用该 ID
    options.id = candidates.keys().next().value;
    console.log(`[INFO] 自动选定唯一匹配连接 ID: ${options.id}`);
  }

  // 2. 针对指定 options.id 提取完整时间线
  const timeline = [];
  let totalFramesScanned = connLines.length;

  connLines.forEach((line, frameIndex) => {
    let parsed;
    try {
      parsed = JSON.parse(line);
    } catch {
      return;
    }
    const receivedAt = parsed.receivedAt;
    const conns = parsed.frame?.connections || [];
    const found = conns.find((c) => c.id === options.id);

    if (found) {
      timeline.push({
        frameIndex,
        receivedAt,
        conn: found,
      });
    }
  });

  console.log('===============================================================');
  console.log(`Connection Timeline Trace: ${options.id}`);
  console.log(`Session Dir  : ${path.basename(absDir)}`);
  console.log(`Session Start: ${manifest.startTime}`);
  console.log(`Session End  : ${manifest.endTime} (Duration: ${manifest.durationSeconds}s)`);
  console.log(`Total Frames : ${totalFramesScanned} frames scanned`);
  console.log('===============================================================\n');

  if (timeline.length === 0) {
    console.log(`[WARNING] 在该会话中未找到 Connection ID: ${options.id}`);
    return;
  }

  // 3. 计算帧间 delta 与一致性验证
  let prevUpload = null;
  let prevDownload = null;
  let prevFrameIndex = null;
  let sumUploadDelta = 0;
  let sumDownloadDelta = 0;
  let hasCounterDecrease = false;
  let hasStartChange = false;
  let hasMetadataChange = false;
  let hasPresenceGap = false;

  const initialStart = timeline[0].conn.start;
  const initialMetaJson = JSON.stringify(timeline[0].conn.metadata);

  console.log('--- Step-by-Step Observation Timeline ---');
  console.log('Idx | Frame | Received At (UTC)        | Up (Bytes) | Up Delta  | Down (Bytes) | Down Delta  | Chains Form');
  console.log('----+-------+--------------------------+------------+-----------+--------------+-------------+-----------------------------');

  timeline.forEach((item, obsIdx) => {
    const c = item.conn;
    const up = c.upload ?? 0;
    const down = c.download ?? 0;

    let upDelta = 0;
    let downDelta = 0;

    if (prevUpload !== null) {
      upDelta = up - prevUpload;
      downDelta = down - prevDownload;
      sumUploadDelta += upDelta;
      sumDownloadDelta += downDelta;
      if (upDelta < 0 || downDelta < 0) hasCounterDecrease = true;
    }

    if (prevFrameIndex !== null && item.frameIndex > prevFrameIndex + 1) {
      hasPresenceGap = true;
    }

    if (c.start !== initialStart) hasStartChange = true;
    if (JSON.stringify(c.metadata) !== initialMetaJson) hasMetadataChange = true;

    const chainStr = JSON.stringify(c.chains);
    const upDeltaStr = prevUpload === null ? '(first)' : (upDelta >= 0 ? `+${upDelta}` : `${upDelta}`);
    const downDeltaStr = prevDownload === null ? '(first)' : (downDelta >= 0 ? `+${downDelta}` : `${downDelta}`);

    console.log(
      `${String(obsIdx).padStart(3)} | ` +
      `${String(item.frameIndex).padStart(5)} | ` +
      `${item.receivedAt} | ` +
      `${String(up).padStart(10)} | ` +
      `${upDeltaStr.padStart(9)} | ` +
      `${String(down).padStart(12)} | ` +
      `${downDeltaStr.padStart(11)} | ` +
      `${chainStr}`
    );

    prevUpload = up;
    prevDownload = down;
    prevFrameIndex = item.frameIndex;
  });

  const firstObs = timeline[0];
  const lastObs = timeline[timeline.length - 1];
  const observedUpDelta = (lastObs.conn.upload ?? 0) - (firstObs.conn.upload ?? 0);
  const observedDownDelta = (lastObs.conn.download ?? 0) - (firstObs.conn.download ?? 0);

  const isUpConsistent = sumUploadDelta === observedUpDelta;
  const isDownConsistent = sumDownloadDelta === observedDownDelta;

  console.log('\n--- Timeline Summary & Verification ---');
  console.log(`Connection ID               : ${options.id}`);
  console.log(`Process                     : ${firstObs.conn.metadata?.process} (${firstObs.conn.metadata?.processPath})`);
  console.log(`Target Host / Remote Dest   : ${firstObs.conn.metadata?.host || '(none)'} / ${firstObs.conn.metadata?.remoteDestination || firstObs.conn.metadata?.destinationIP}`);
  console.log(`Destination Port / Protocol : ${firstObs.conn.metadata?.destinationPort} / ${firstObs.conn.metadata?.network}`);
  console.log(`Rule / Rule Payload         : ${firstObs.conn.rule} / ${firstObs.conn.rulePayload}`);
  console.log(`Start Timestamp             : ${firstObs.conn.start}`);
  console.log(`First Observed Snapshot     : Frame #${firstObs.frameIndex} at ${firstObs.receivedAt}`);
  console.log(`Last Observed Snapshot      : Frame #${lastObs.frameIndex} at ${lastObs.receivedAt}`);
  console.log(`Total Snapshots Present     : ${timeline.length} (out of ${totalFramesScanned} total session frames)`);
  console.log(`Presence Gaps (Disappeared?): ${hasPresenceGap ? 'YES (Disappeared and reappeared)' : 'NO (Continuous observation)'}`);
  console.log(`First Counter (Up / Down)   : ${firstObs.conn.upload} B / ${firstObs.conn.download} B`);
  console.log(`Last Counter (Up / Down)    : ${lastObs.conn.upload} B / ${lastObs.conn.download} B`);
  console.log(`Observed Delta (Last-First) : Upload: +${observedUpDelta} B | Download: +${observedDownDelta} B (${formatBytes(observedDownDelta)})`);
  console.log(`Sum of Adjacent Deltas      : Upload: +${sumUploadDelta} B | Download: +${sumDownloadDelta} B`);
  console.log(`Arithmetic Consistency      : ${isUpConsistent && isDownConsistent ? 'EXACT MATCH (PASS)' : 'MISMATCH (FAIL)'}`);
  console.log(`Counter Decreased Detected  : ${hasCounterDecrease ? 'YES (Anomaly)' : 'NO (Monotonic Non-Decreasing)'}`);
  console.log(`Start Mutated Across Frames : ${hasStartChange ? 'YES (Anomaly)' : 'NO (Stable Start Timestamp)'}`);
  console.log(`Metadata Mutated in Session : ${hasMetadataChange ? 'YES (Metadata Changed)' : 'NO (Stable Metadata)'}`);
  console.log('===============================================================\n');
}

main().catch((err) => {
  console.error('[FATAL]', err);
  process.exit(1);
});
