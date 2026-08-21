/**
 * run-ws-reconnect-experiment.mjs
 * 
 * Stage R4: Same-Client Process WebSocket Disconnect & Reconnect Experiment
 * 
 * 流程:
 * 1. 同一 Node.js 进程建立 /connections WebSocket 连接
 * 2. 采集 Phase 1 (Pre-disconnect baseline, 3s)
 * 3. 故障注入: 主动关闭客户端 WebSocket (模拟网络抖动/Collector WS 中断)
 * 4. 模拟 3.0 秒断线 Gap (Monitoring Gap)
 * 5. 自动重连: 重新建立 /connections WebSocket 连接
 * 6. 采集 Phase 2 (Post-reconnect observation, 4s)
 * 7. 分析:
 *    - 跨断线存活连接 ID 保持率
 *    - Coverage Gap 时间戳与物理流量差分
 *    - 验证同一进程下的重连状态机行为
 */

import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '../../..');

async function runWsSession() {
  const wsUrl = 'ws://127.0.0.1:9090/connections';
  const outDir = path.join(projectRoot, 'tmp/discovery/work-package-b1');
  fs.mkdirSync(outDir, { recursive: true });

  console.log('================================================================');
  console.log('STAGE R4: SAME-CLIENT WS DISCONNECT & RECONNECT EXPERIMENT');
  console.log('================================================================');

  const p1Frames = [];
  const p2Frames = [];
  const events = [];

  function logEvent(type, details = {}) {
    const ts = new Date().toISOString();
    events.push({ timestamp: ts, type, details });
    console.log(`[${ts}] [${type}]`, details);
  }

  // --- PHASE 1 ---
  console.log('\n[PHASE 1] Connecting WebSocket for pre-disconnect baseline (3s)...');
  let ws1 = new WebSocket(wsUrl);
  let p1Resolve;
  const p1Promise = new Promise(r => p1Resolve = r);

  ws1.onopen = () => {
    logEvent('ws_open_phase1', { status: 'connected' });
  };

  ws1.onmessage = (event) => {
    try {
      const parsed = JSON.parse(event.data);
      p1Frames.push({ receivedAt: new Date().toISOString(), payload: parsed });
    } catch {}
  };

  // 运行 3 秒后主动切断
  setTimeout(() => {
    logEvent('ws_fault_injection_disconnect', { reason: 'Client simulated disconnect' });
    ws1.close(1000, 'Simulated Client Disconnect');
    p1Resolve();
  }, 3000);

  await p1Promise;
  const lastP1Frame = p1Frames[p1Frames.length - 1];
  const lastP1Ts = lastP1Frame?.receivedAt || new Date().toISOString();
  console.log(`[PHASE 1 DONE] Collected ${p1Frames.length} frames. Last frame at: ${lastP1Ts}`);

  // --- GAP SIMULATION ---
  console.log('\n[GAP] Simulating 3.0s client-side disconnection gap...');
  await new Promise(r => setTimeout(r, 3000));
  logEvent('reconnect_attempt_start', { gapDuration: '3.0s' });

  // --- PHASE 2 ---
  console.log('\n[PHASE 2] Reconnecting WebSocket on the same client process (4s)...');
  let ws2 = new WebSocket(wsUrl);
  let p2Resolve;
  const p2Promise = new Promise(r => p2Resolve = r);

  ws2.onopen = () => {
    logEvent('ws_open_phase2_reconnected', { status: 'reconnected_successfully' });
  };

  ws2.onmessage = (event) => {
    try {
      const parsed = JSON.parse(event.data);
      if (p2Frames.length === 0) {
        logEvent('first_frame_after_reconnect', {
          uploadTotal: parsed.uploadTotal,
          downloadTotal: parsed.downloadTotal,
          connectionsCount: (parsed.connections || []).length
        });
      }
      p2Frames.push({ receivedAt: new Date().toISOString(), payload: parsed });
    } catch {}
  };

  setTimeout(() => {
    ws2.close();
    p2Resolve();
  }, 4000);

  await p2Promise;
  console.log(`[PHASE 2 DONE] Collected ${p2Frames.length} frames.`);

  // --- ANALYSIS ---
  const firstP2Frame = p2Frames[0];
  const firstP2Ts = firstP2Frame?.receivedAt || new Date().toISOString();

  const p1Data = lastP1Frame?.payload || {};
  const p2Data = firstP2Frame?.payload || {};

  const gapUpload = (p2Data.uploadTotal ?? 0) - (p1Data.uploadTotal ?? 0);
  const gapDownload = (p2Data.downloadTotal ?? 0) - (p1Data.downloadTotal ?? 0);

  const p1Conns = new Map((p1Data.connections || []).map(c => [c.id, c]));
  const p2Conns = new Map((p2Data.connections || []).map(c => [c.id, c]));

  let survivedAcrossReconnect = 0;
  for (const id of p2Conns.keys()) {
    if (p1Conns.has(id)) survivedAcrossReconnect++;
  }

  const actualObservationGapMs = new Date(firstP2Ts).getTime() - new Date(lastP1Ts).getTime();

  const report = {
    experiment: 'SAME_PROCESS_WS_RECONNECT',
    coverageGap: {
      lastObservedAt: lastP1Ts,
      firstObservedAfterRecoveryAt: firstP2Ts,
      actualObservationGapMs,
      actualObservationGapSec: (actualObservationGapMs / 1000).toFixed(2)
    },
    gapPhysicalTraffic: {
      gapUploadDelta: gapUpload,
      gapDownloadDelta: gapDownload
    },
    connectionContinuity: {
      preDisconnectConnsCount: p1Conns.size,
      postReconnectConnsCount: p2Conns.size,
      survivedAcrossReconnect,
      survivedRatioPct: p1Conns.size > 0 ? ((survivedAcrossReconnect / p1Conns.size) * 100).toFixed(1) : 0
    },
    events
  };

  const reportPath = path.join(outDir, 'ws-reconnect-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(report, null, 2));

  console.log('----------------------------------------------------------------');
  console.log('WS RECONNECT EXPERIMENT SUMMARY:');
  console.log(`  Coverage Gap Window           : ${report.coverageGap.lastObservedAt} -> ${report.coverageGap.firstObservedAfterRecoveryAt}`);
  console.log(`  Actual Coverage Gap Duration  : ${report.coverageGap.actualObservationGapSec}s (${report.coverageGap.actualObservationGapMs} ms)`);
  console.log(`  Pre-Disconnect Active Conns   : ${report.connectionContinuity.preDisconnectConnsCount}`);
  console.log(`  Post-Reconnect Active Conns   : ${report.connectionContinuity.postReconnectConnsCount}`);
  console.log(`  Survived Connections Across WS: ${report.connectionContinuity.survivedAcrossReconnect} (${report.connectionContinuity.survivedRatioPct}%)`);
  console.log(`  Gap Physical Upload Delta     : ${gapUpload.toLocaleString()} Bytes`);
  console.log(`  Gap Physical Download Delta   : ${gapDownload.toLocaleString()} Bytes`);
  console.log(`  Report written to             : ${reportPath}`);
  console.log('================================================================\n');
  process.exit(0);
}

runWsSession().catch(err => {
  console.error('[FATAL]', err);
  process.exit(1);
});
