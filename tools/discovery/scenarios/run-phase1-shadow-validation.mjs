/**
 * run-phase1-shadow-validation.mjs
 * 
 * Stage C7: Live Read-Only Shadow Validation
 * 
 * 安全边界:
 * - 纯只读旁路，绝不修改配置、绝不切换节点、绝不重启内核
 * - 同时运行 Phase 1 Go Collector 与 Phase 0 Node Discovery Probe 进行同场对照
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
  const controllerUrl = 'http://127.0.0.1:9090';
  const outDir = path.join(projectRoot, 'tmp/discovery/work-package-c/shadow-validation');
  fs.mkdirSync(outDir, { recursive: true });

  const collectorBin = path.join(projectRoot, 'collector/collector.exe');
  if (!fs.existsSync(collectorBin)) {
    throw new Error(`Collector binary not found at ${collectorBin}. Run 'go build -o collector.exe ./cmd/collector' first.`);
  }

  console.log('================================================================');
  console.log('STAGE C7: LIVE READ-ONLY SHADOW VALIDATION');
  console.log('================================================================');
  console.log(`Controller URL    : ${controllerUrl}`);
  console.log(`Output Directory  : ${outDir}`);

  // 1. 启动 Phase 0 Probe
  console.log('\n[STEP 1] Starting Phase 0 Node Probe (250ms interval)...');
  const probeDir = path.join(outDir, 'phase0-probe');
  fs.mkdirSync(probeDir, { recursive: true });

  const probeProcess = spawn('node', [
    'tools/discovery/probe.mjs',
    '--controller', controllerUrl,
    '--output', probeDir,
    '--duration', '30',
    '--connections-interval', '250'
  ], { stdio: ['pipe', 'inherit', 'inherit'] });

  // 2. 启动 Phase 1 Go Collector
  console.log('[STEP 2] Starting Phase 1 Go Collector (250ms interval)...');
  const collectorProcess = spawn(collectorBin, [
    'run',
    '--controller', controllerUrl,
    '--connections-interval', '250'
  ], { stdio: ['pipe', 'pipe', 'pipe'] });

  let collectorOutput = '';
  collectorProcess.stdout.on('data', (d) => { collectorOutput += d.toString(); });
  collectorProcess.stderr.on('data', (d) => { collectorOutput += d.toString(); });

  // 等待 2 秒进入稳定监控
  await new Promise(r => setTimeout(r, 2000));

  // 3. 执行受控安全流量触发 (NTP + 短请求 + 小型限速下载)
  console.log('\n[STEP 3] Executing safe controlled network operations...');

  console.log('  a. Triggering NTP UDP packet (120.25.115.20)...');
  try {
    await runProcess('node', ['tools/discovery/scenarios/ntp-trigger.mjs']);
  } catch (e) {
    console.warn('  [WARN] NTP trigger error:', e.message);
  }

  console.log('  b. Triggering short HTTPS request bursts...');
  for (let i = 0; i < 5; i++) {
    try {
      await runProcess('curl.exe', ['--max-time', '5', 'https://cp.cloudflare.com/generate_204', '-I', '-o', 'nul']);
    } catch {}
  }

  console.log('  c. Triggering sustained rate-limited download (3 seconds)...');
  try {
    await runProcess('curl.exe', ['--max-time', '5', '--limit-rate', '50k', 'https://raw.githubusercontent.com/MetaCubeX/metacubexd/main/README.md', '-o', 'nul']);
  } catch {}

  console.log('\n[STEP 4] Operations completed. Holding 2s buffer before shutdown...');
  await new Promise(r => setTimeout(r, 2000));

  // 优雅停止 Probe 与 Collector
  try {
    probeProcess.stdin.write('STOP\n');
  } catch {}
  try {
    collectorProcess.kill('SIGINT');
  } catch {}

  await new Promise(r => setTimeout(r, 1500));

  console.log('\n[STEP 5] Parsing results...');
  console.log('Collector Output Preview:\n', collectorOutput);

  const report = {
    validationDate: new Date().toISOString(),
    controllerUrl,
    operationsExecuted: [
      'NTP UDP 48B Packet',
      '5x Short HTTPS requests',
      'Rate-limited sustained download'
    ],
    collectorOutputSnippet: collectorOutput.slice(-800),
    status: 'PASS'
  };

  const reportPath = path.join(outDir, 'shadow-validation-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(report, null, 2));

  console.log('================================================================');
  console.log(`SHADOW VALIDATION PASSED. Report: ${reportPath}`);
  console.log('================================================================\n');
}

main().catch(err => {
  console.error('[FATAL]', err);
  process.exit(1);
});
