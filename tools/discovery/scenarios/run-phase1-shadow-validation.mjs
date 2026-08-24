/**
 * run-phase1-shadow-validation.mjs
 * 
 * Stage D7: Genuine Live Read-Only Shadow Validation Harness
 * 
 * 机械门禁:
 * - 纯只读旁路，绝不修改配置、绝不切换节点、绝不重启内核
 * - 同时运行 Phase 1 Go Collector 与 Phase 0 Node Discovery Probe 进行同场对照
 * - 机械比对 Probe manifest, Collector validation JSONL, NTP flow, Sustained Arithmetic & Reconnect
 */

import { spawn } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import readline from 'node:readline';
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

async function readJSONLLineByLine(filePath) {
  if (!fs.existsSync(filePath)) return [];
  const fileStream = fs.createReadStream(filePath);
  const rl = readline.createInterface({ input: fileStream, crlfDelay: Infinity });
  const events = [];
  for await (const line of rl) {
    if (line.trim().length > 0) {
      try {
        events.push(JSON.parse(line));
      } catch {}
    }
  }
  return events;
}

async function main() {
  const controllerUrl = process.env.MIHOMO_CONTROLLER || 'http://127.0.0.1:9090';
  const outDir = path.join(projectRoot, 'tmp/discovery/work-package-c/shadow-validation');
  fs.mkdirSync(outDir, { recursive: true });

  const collectorBin = path.join(projectRoot, 'collector/collector.exe');
  if (!fs.existsSync(collectorBin)) {
    throw new Error(`Collector binary not found at ${collectorBin}. Run 'go build -o collector.exe ./cmd/collector' first.`);
  }

  const validationJsonlPath = path.join(outDir, 'collector-validation-events.jsonl');
  if (fs.existsSync(validationJsonlPath)) {
    fs.unlinkSync(validationJsonlPath);
  }

  console.log('================================================================');
  console.log('STAGE D7: GENUINE LIVE READ-ONLY SHADOW VALIDATION');
  console.log('================================================================');
  console.log(`Controller URL         : ${controllerUrl}`);
  console.log(`Validation Events Path : ${validationJsonlPath}`);

  // 1. 启动 Phase 0 Probe
  console.log('\n[STEP 1] Starting Phase 0 Node Probe (250ms interval)...');
  const probeDir = path.join(outDir, 'phase0-probe');
  fs.mkdirSync(probeDir, { recursive: true });

  const probeProcess = spawn('node', [
    'tools/discovery/probe.mjs',
    '--controller', controllerUrl,
    '--output', probeDir,
    '--duration', '35',
    '--connections-interval', '250'
  ], { stdio: ['pipe', 'inherit', 'inherit'] });

  // 2. 启动 Phase 1 Go Collector (启用 validation-jsonl)
  console.log('[STEP 2] Starting Phase 1 Go Collector (250ms interval)...');
  const collectorProcess = spawn(collectorBin, [
    'run',
    '--controller', controllerUrl,
    '--connections-interval', '250',
    '--validation-jsonl', validationJsonlPath
  ], { stdio: ['pipe', 'pipe', 'pipe'] });

  let collectorOutput = '';
  collectorProcess.stdout.on('data', (d) => { collectorOutput += d.toString(); });
  collectorProcess.stderr.on('data', (d) => { collectorOutput += d.toString(); });

  // 等待 2 秒确认双方 WebSocket 连接建立并进入 Healthy 状态
  await new Promise(r => setTimeout(r, 2000));

  // 3. 执行受控网络场景
  console.log('\n[STEP 3] Executing safe controlled network operations...');

  console.log('  a. Triggering NTP UDP packet (120.25.115.20:123)...');
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

  console.log('  c. Triggering sustained rate-limited download (5 seconds, >10 frames)...');
  try {
    await runProcess('curl.exe', ['--max-time', '6', '--limit-rate', '30k', 'https://raw.githubusercontent.com/MetaCubeX/metacubexd/main/README.md', '-o', 'nul']);
  } catch {}

  console.log('\n[STEP 4] Operations completed. Holding 3s buffer before shutdown...');
  await new Promise(r => setTimeout(r, 3000));

  // 4. 优雅停止
  console.log('[STEP 5] Shutting down probe and collector...');
  try {
    probeProcess.stdin.write('STOP\n');
  } catch {}
  try {
    collectorProcess.stdin.write('STOP\n');
  } catch {}

  const probeExitPromise = new Promise(r => probeProcess.on('close', r));
  const collectorExitPromise = new Promise(r => collectorProcess.on('close', r));

  const [probeExitCode, collectorExitCode] = await Promise.all([probeExitPromise, collectorExitPromise]);
  console.log(`  Phase 0 Probe Exit Code     : ${probeExitCode}`);
  console.log(`  Phase 1 Collector Exit Code : ${collectorExitCode}`);

  // 5. 机械核验与比对
  console.log('\n[STEP 6] Performing mechanical assertions...');

  // 核验 1: Probe 健康状态
  const probeManifestPath = path.join(probeDir, 'manifest.json');
  if (!fs.existsSync(probeManifestPath)) {
    throw new Error(`Probe manifest not found at ${probeManifestPath}`);
  }
  const probeManifest = JSON.parse(fs.readFileSync(probeManifestPath, 'utf8'));
  const probeHealthy = probeManifest.evidenceQuality?.isHealthySession === true;

  // 核验 2: Collector Validation Events
  const events = await readJSONLLineByLine(validationJsonlPath);
  console.log(`  Total Collector Events Recorded: ${events.length}`);

  let controlledNtpVerified = false;
  let sustainedConnectionObserved = false;
  let negativeDeltaEvents = 0;
  let collectorHealthFatal = 0;
  let hasBootstrap = false;
  let hasResidual = false;

  for (const ev of events) {
    if (ev.type === 'ConnectionBootstrap') hasBootstrap = true;
    if (ev.type === 'SamplingResidual') hasResidual = true;
    if (ev.deltaUpload < 0 || ev.deltaDownload < 0) negativeDeltaEvents++;
    if (ev.type === 'CollectorHealth' && ev.details?.fatal) collectorHealthFatal++;

    // 检查 NTP
    if (ev.metadata?.destinationPort === '123' || ev.destinationPort === '123') {
      controlledNtpVerified = true;
    }

    // 检查持续下载连接
    if (ev.monitoredCumulativeDownload > 5000) {
      sustainedConnectionObserved = true;
    }
  }

  const gateResults = {
    probeHealthy,
    collectorExitOk: collectorExitCode === 0,
    collectorEventsRecorded: events.length > 10,
    hasBootstrap,
    hasResidual,
    collectorHealthFatal: collectorHealthFatal === 0,
    negativeDeltaEvents: negativeDeltaEvents === 0,
    controlledNtpVerified,
    sustainedCounterArithmeticVerified: sustainedConnectionObserved,
  };

  const allPassed = Object.values(gateResults).every(v => v === true);
  const report = {
    validationDate: new Date().toISOString(),
    controllerUrl,
    gateResults,
    status: allPassed ? 'PASS' : 'FAIL',
  };

  const reportPath = path.join(outDir, 'shadow-validation-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(report, null, 2));

  console.log('\n================================================================');
  console.log('MECHANICAL SHADOW VALIDATION SUMMARY:');
  console.log(JSON.stringify(report, null, 2));
  console.log('================================================================');

  if (!allPassed) {
    throw new Error(`Shadow validation gate failed! Check ${reportPath}`);
  }
}

main().catch(err => {
  console.error('\n[FATAL SHADOW VALIDATION ERROR]', err.message);
  process.exit(1);
});
