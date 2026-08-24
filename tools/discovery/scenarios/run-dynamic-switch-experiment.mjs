/**
 * run-dynamic-switch-experiment.mjs
 * 
 * Stage B1: Dynamic Routing & Node Switching Experiment
 * 
 * 安全机制:
 * 1. 默认仅进行 Preflight / Dry-run，绝不直接发起任何 PUT 修改 live 节点
 * 2. 必须显式传入 --allow-live-mutation 选项才允许发起实际网络变更
 * 3. 必须通过受保护的 runControlledSwitch 进行切换，强制二次 GET 验证与 Best-effort 回滚验证
 * 4. 若回滚失败，强制以非零状态码退出 (fail closed)
 * 5. Probe 进程通过 stdin STOP 指令实现优雅 flush 与退出
 * 
 * 用法:
 *   node tools/discovery/scenarios/run-dynamic-switch-experiment.mjs [--controller <url>] [--group <name>] [--target-node <node>] [--target-url <url>] [--allow-live-mutation]
 */

import { spawn } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'url';
import { runControlledSwitch, getProxyGroup } from './switch-node.mjs';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '../../..');

function parseArgs() {
  const args = process.argv.slice(2);
  const options = {
    controllerUrl: 'http://127.0.0.1:9090',
    groupName: '入口选择',
    targetNode: '🇭🇰 香港W01',
    targetUrl: 'https://raw.githubusercontent.com/MetaCubeX/metacubexd/main/README.md',
    allowLiveMutation: false
  };

  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--controller' && args[i + 1]) options.controllerUrl = args[++i];
    if (args[i] === '--group' && args[i + 1]) options.groupName = args[++i];
    if (args[i] === '--target-node' && args[i + 1]) options.targetNode = args[++i];
    if (args[i] === '--target-url' && args[i + 1]) options.targetUrl = args[++i];
    if (args[i] === '--allow-live-mutation') options.allowLiveMutation = true;
  }

  return options;
}

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
  const { controllerUrl, groupName, targetNode, targetUrl, allowLiveMutation } = parseArgs();

  console.log('================================================================');
  console.log('STAGE B1: DYNAMIC ROUTING & NODE SWITCHING EXPERIMENT');
  console.log('================================================================');
  console.log(`Controller URL    : ${controllerUrl}`);
  console.log(`Target Group      : [${groupName}]`);
  console.log(`Target Switch Node: [${targetNode}]`);
  console.log(`Target URL        : ${targetUrl}`);
  console.log(`Live Mutation     : ${allowLiveMutation ? 'ENABLED (LIVE MUTATION ALLOWED)' : 'DISABLED (SAFE DRY-RUN ONLY)'}`);

  // 1. Preflight: 检查当前状态
  const initialInfo = await getProxyGroup(controllerUrl, groupName);
  const originalNode = initialInfo.now;
  console.log(`Current Active    : [${originalNode}]`);

  if (!allowLiveMutation) {
    console.log('\n[DRY-RUN / PREFLIGHT NOTICE]');
    console.log(`  --allow-live-mutation was NOT specified.`);
    console.log(`  Dry-run preflight check passed: group [${groupName}] is currently on [${originalNode}].`);
    console.log(`  Skipping live PUT mutation. Exiting safely.\n`);
    return;
  }

  const sessionDir = path.join(projectRoot, `tmp/discovery/work-package-b/dynamic-switch-${new Date().toISOString().replace(/[:.]/g, '-')}`);
  fs.mkdirSync(sessionDir, { recursive: true });

  console.log(`Session Output Dir: ${sessionDir}`);

  // 2. 启动 Probe 后台执行
  console.log('[PROBE] Starting Discovery Probe (connections-interval: 250ms)...');
  const probeProcess = spawn('node', [
    'tools/discovery/probe.mjs',
    '--controller', controllerUrl,
    '--output', sessionDir,
    '--duration', '60',
    '--connections-interval', '250'
  ], { stdio: ['pipe', 'inherit', 'inherit'] });

  const probePromise = new Promise((resolve, reject) => {
    probeProcess.on('error', reject);
    probeProcess.on('close', (code) => {
      if (code === 0) resolve();
      else reject(new Error(`Probe exited with code ${code}`));
    });
  });

  // 等待 2 秒进入稳定监控
  await new Promise(r => setTimeout(r, 2000));

  let switchMeta = {};

  try {
    // 3. 发起持续限速长请求
    console.log('[STEP 1] Launching Phase 1 sustained request (Original Node)...');
    const curlProcess = spawn('curl.exe', [
      '--limit-rate', '20k',
      targetUrl,
      '-o', 'nul'
    ], { stdio: 'inherit' });

    // 等待 2.5 秒，确保长连接已经建立并在快照中出现
    await new Promise(r => setTimeout(r, 2500));

    // 4. 执行受控切换与验证
    switchMeta = await runControlledSwitch({
      controllerUrl,
      groupName,
      targetNode,
      allowLiveMutation: true,
      onSwitched: async () => {
        // 5. 在新节点状态下发起新请求
        console.log('[STEP 2] Launching Phase 2 new request (Under Switched Node)...');
        await runProcess('curl.exe', ['--max-time', '15', targetUrl, '-I', '-o', 'nul']);

        // 等待原长请求完成
        console.log('[STEP 3] Waiting for original sustained request to complete...');
        await new Promise((resolve) => {
          curlProcess.on('close', resolve);
          curlProcess.on('error', resolve);
        });
      }
    });

    // 6. 额外留出 2 秒采样缓冲，然后通过 stdin 发送 STOP 通知 Probe 优雅退出
    console.log('[PROBE] Experiment completed, waiting 2s buffer before graceful probe flush...');
    await new Promise(r => setTimeout(r, 2000));
    probeProcess.stdin.write('STOP\n');
    await probePromise;

  } catch (err) {
    console.error('[EXPERIMENT ERROR]', err.message);
    try {
      probeProcess.stdin.write('STOP\n');
      await probePromise;
    } catch {}
    throw err;
  }

  // 写出会话 metadata
  const metaPath = path.join(sessionDir, 'switch-meta.json');
  fs.writeFileSync(metaPath, JSON.stringify({
    groupName,
    originalNode,
    targetNode,
    switchMeta,
    completedAt: new Date().toISOString()
  }, null, 2));

  console.log('================================================================');
  console.log(`EXPERIMENT COMPLETED SUCCESSFULLY. Session: ${sessionDir}`);
  console.log('================================================================\n');
}

main().catch(err => {
  console.error('[FATAL]', err);
  process.exit(1);
});
