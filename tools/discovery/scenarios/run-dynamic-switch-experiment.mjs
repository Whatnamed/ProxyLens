/**
 * run-dynamic-switch-experiment.mjs
 * 
 * Stage B1: Dynamic Routing & Node Switching Experiment
 * 
 * 流程:
 * 1. 启动 Discovery Probe 录制
 * 2. 发起一个受控持续下载 (Target: GitHub raw README.md, rate-limited)
 * 3. 记录第 1 阶段的原长连接 ID、chains 与字节
 * 4. 在下载进行中，动态将 `GitHub` 策略组从 `入口选择` 切换到 `DIRECT` (或 `出口选择`)
 * 5. 在新状态下发起一个新的请求
 * 6. 观察原长连接与新连接在快照中的行为、chains 演变与生命周期
 * 7. 自动将策略组切换回初始状态 (Auto-Rollback)
 */

import { spawn } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'url';
import { getProxyGroup, setProxyGroupNode } from './switch-node.mjs';

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
  const controllerUrl = 'http://127.0.0.1:9090';
  const groupName = '入口选择';
  const targetTestNode = '🇭🇰 香港W01';

  const sessionDir = path.join(projectRoot, `tmp/discovery/work-package-b/dynamic-switch-${new Date().toISOString().replace(/[:.]/g, '-')}`);
  fs.mkdirSync(sessionDir, { recursive: true });

  console.log('================================================================');
  console.log('STAGE B1: DYNAMIC ROUTING & NODE SWITCHING EXPERIMENT');
  console.log('================================================================');
  console.log(`Session Dir       : ${sessionDir}`);
  console.log(`Target Group      : [${groupName}]`);

  // 1. 获取初始节点
  const initialInfo = await getProxyGroup(controllerUrl, groupName);
  const originalNode = initialInfo.now;
  console.log(`Original Node     : [${originalNode}]`);

  let rollbackNeeded = true;
  const rollback = async () => {
    if (!rollbackNeeded) return;
    rollbackNeeded = false;
    try {
      console.log(`\n[CLEANUP] Rolling back group [${groupName}] to original node [${originalNode}]...`);
      await setProxyGroupNode(controllerUrl, groupName, originalNode);
      console.log('[CLEANUP] Rollback successfully finished.');
    } catch (e) {
      console.error('[CLEANUP FAILED]', e.message);
    }
  };

  process.on('SIGINT', async () => { await rollback(); process.exit(130); });

  try {
    // 2. 启动 Probe
    console.log('[PROBE] Starting Discovery Probe (connections-interval: 250ms)...');
    const probeProcess = spawn('node', [
      'tools/discovery/probe.mjs',
      '--controller', controllerUrl,
      '--output', sessionDir,
      '--duration', '30',
      '--connections-interval', '250'
    ], { stdio: 'inherit' });

    const probePromise = new Promise((resolve, reject) => {
      probeProcess.on('error', reject);
      probeProcess.on('close', (code) => {
        if (code === 0) resolve();
        else reject(new Error(`Probe exited with code ${code}`));
      });
    });

    // 等待 2 秒进入稳定监控
    await new Promise(r => setTimeout(r, 2000));

    // 3. 发起持续限速长请求 (Target: raw.githubusercontent.com)
    console.log('[STEP 1] Launching Phase 1 sustained request (Original Node)...');
    const sustainedUrl = 'https://raw.githubusercontent.com/MetaCubeX/metacubexd/main/README.md';
    
    // 使用 curl 限速下载以维持 6-8 秒长连接
    const curlProcess = spawn('curl.exe', [
      '--limit-rate', '20k',
      sustainedUrl,
      '-o', 'nul'
    ], { stdio: 'inherit' });

    // 等待 2.5 秒，确保长连接已经建立并在快照中出现
    await new Promise(r => setTimeout(r, 2500));

    // 4. 动态执行策略组切换
    console.log(`\n[STEP 2] DYNAMICALLY SWITCHING [${groupName}] -> [${targetTestNode}] while sustained download is active!`);
    await setProxyGroupNode(controllerUrl, groupName, targetTestNode);
    console.log(`[SWITCH APPLIED] Group [${groupName}] is now [${targetTestNode}]`);

    // 等待 1 秒
    await new Promise(r => setTimeout(r, 1000));

    // 5. 在新节点状态下发起新请求
    console.log('[STEP 3] Launching Phase 2 new request (Under Switched Node)...');
    await runProcess('curl.exe', ['--max-time', '15', 'https://raw.githubusercontent.com/MetaCubeX/metacubexd/main/README.md', '-I', '-o', 'nul']);

    // 等待原长请求完成
    await new Promise((resolve) => {
      curlProcess.on('close', resolve);
      curlProcess.on('error', resolve);
    });

    console.log('[STEP 4] Sustained download completed. Waiting 2s buffer before probe shutdown...');
    await new Promise(r => setTimeout(r, 2000));

    probeProcess.kill('SIGINT');
    await probePromise;

  } finally {
    await rollback();
  }

  console.log('================================================================');
  console.log(`EXPERIMENT COMPLETED. Session recorded in: ${sessionDir}`);
  console.log('================================================================\n');

  return sessionDir;
}

main().catch(err => {
  console.error('[FATAL]', err);
  process.exit(1);
});
