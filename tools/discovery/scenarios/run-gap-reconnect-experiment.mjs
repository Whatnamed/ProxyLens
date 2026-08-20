/**
 * run-gap-reconnect-experiment.mjs
 * 
 * Stage B2: Controller Gap & WebSocket Reconnect Semantics Experiment
 * 
 * 流程:
 * 1. 启动持续流量背景负载 (Background continuous HTTP traffic)
 * 2. 启动 Phase 1 采集 (4 秒)
 * 3. 模拟中断: 终止 Phase 1，人为休眠 4 秒 (产生显式 Monitoring Gap)
 * 4. 启动 Phase 2 采集 (6 秒)，模拟 Collector 断线重连
 * 5. 分析两段会话之间的数据衔接、首帧基线、Gap 流量与状态机行为
 */

import { spawn } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '../../..');

function runProcess(cmd, args) {
  return new Promise((resolve, reject) => {
    const p = spawn(cmd, args, { stdio: 'inherit' });
    p.on('error', reject);
    p.on('close', (code) => {
      if (code === 0) resolve();
      else reject(new Error(`${cmd} ${args.join(' ')} failed with code ${code}`));
    });
  });
}

async function main() {
  const baseOutDir = path.join(projectRoot, `tmp/discovery/work-package-b/gap-experiment-${new Date().toISOString().replace(/[:.]/g, '-')}`);
  const phase1Dir = path.join(baseOutDir, 'phase1-before-gap');
  const phase2Dir = path.join(baseOutDir, 'phase2-after-gap');

  fs.mkdirSync(phase1Dir, { recursive: true });
  fs.mkdirSync(phase2Dir, { recursive: true });

  console.log('================================================================');
  console.log('STAGE B2: CONTROLLER GAP & RECONNECT SEMANTICS EXPERIMENT');
  console.log('================================================================');
  console.log(`Base Output Dir   : ${baseOutDir}`);

  // 1. 启动后台持续流量发生器 (每秒发 1 个请求，持续 16 秒)
  console.log('[TRAFFIC] Launching continuous background traffic...');
  const trafficProcess = spawn('node', ['-e', `
    const http = require('https');
    let count = 0;
    const timer = setInterval(() => {
      count++;
      http.get('https://www.baidu.com', (res) => {
        res.resume();
      }).on('error', () => {});
      if (count >= 16) {
        clearInterval(timer);
      }
    }, 1000);
  `], { stdio: 'ignore' });

  // 2. 运行 Phase 1 采集 (4 秒)
  console.log('\n[PHASE 1] Starting Probe Phase 1 (pre-gap observation)...');
  await runProcess('node', [
    'tools/discovery/probe.mjs',
    '--controller', 'http://127.0.0.1:9090',
    '--output', phase1Dir,
    '--duration', '4',
    '--connections-interval', '250'
  ]);
  const phase1EndTs = new Date().toISOString();
  console.log(`[PHASE 1 DONE] Phase 1 completed at: ${phase1EndTs}`);

  // 3. 人为模拟 4 秒 Monitoring Gap (Collector 离线)
  console.log('\n[GAP SIMULATION] Collector disconnected! Simulating 4.0s monitoring gap...');
  await new Promise(r => setTimeout(r, 4000));
  const phase2StartTs = new Date().toISOString();
  console.log(`[GAP END] Reconnecting collector at: ${phase2StartTs}`);

  // 4. 运行 Phase 2 采集 (6 秒，模拟断线重连)
  console.log('\n[PHASE 2] Starting Probe Phase 2 (post-gap reconnect)...');
  await runProcess('node', [
    'tools/discovery/probe.mjs',
    '--controller', 'http://127.0.0.1:9090',
    '--output', phase2Dir,
    '--duration', '6',
    '--connections-interval', '250'
  ]);
  console.log('[PHASE 2 DONE] Phase 2 completed.');

  // 写入 Gap 会话元数据
  const meta = {
    phase1Dir,
    phase2Dir,
    gapStart: phase1EndTs,
    gapEnd: phase2StartTs,
    gapDurationMs: new Date(phase2StartTs).getTime() - new Date(phase1EndTs).getTime()
  };
  fs.writeFileSync(path.join(baseOutDir, 'gap-meta.json'), JSON.stringify(meta, null, 2));

  console.log('================================================================');
  console.log(`GAP EXPERIMENT FINISHED. Gap duration: ${(meta.gapDurationMs / 1000).toFixed(2)}s`);
  console.log(`Results saved to: ${baseOutDir}`);
  console.log('================================================================\n');
}

main().catch(err => {
  console.error('[FATAL]', err);
  process.exit(1);
});
