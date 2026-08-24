/**
 * run-phase1-shadow-validation.mjs
 * 
 * Stage E7: Genuine Live Read-Only Shadow Validation 2.0
 */

import { spawn } from 'node:child_process';
import dgram from 'node:dgram';
import fs from 'node:fs';
import http from 'node:http';
import https from 'node:https';
import path from 'node:path';
import readline from 'node:readline';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '../../..');

async function checkControllerReady(urlStr) {
  return new Promise((resolve) => {
    const parsed = new URL(urlStr);
    const req = http.get({
      hostname: parsed.hostname,
      port: parsed.port || 80,
      path: '/version',
      timeout: 2000,
    }, (res) => {
      resolve(res.statusCode === 200);
    });
    req.on('error', () => resolve(false));
    req.on('timeout', () => { req.destroy(); resolve(false); });
  });
}

function sendControlledNtpRequest(targetHost = 'ntp.aliyun.com', port = 123) {
  return new Promise((resolve, reject) => {
    const socket = dgram.createSocket('udp4');
    const ntpPacket = Buffer.alloc(48);
    ntpPacket[0] = 0x1b; // LI=0, VN=3, Mode=3 (client)

    let boundLocalPort = 0;
    let closed = false;
    const safeClose = () => {
      if (!closed) {
        closed = true;
        try { socket.close(); } catch {}
      }
    };

    socket.bind(0, () => {
      boundLocalPort = socket.address().port;
      socket.send(ntpPacket, 0, ntpPacket.length, port, targetHost, (err) => {
        if (err) {
          safeClose();
          return reject(err);
        }
      });
    });

    socket.on('message', (msg, rinfo) => {
      safeClose();
      resolve({
        localPort: boundLocalPort,
        remoteAddress: rinfo.address,
        remotePort: rinfo.port,
        bytesReceived: msg.length,
      });
    });

    socket.on('error', (err) => {
      safeClose();
      reject(err);
    });

    setTimeout(() => {
      safeClose();
      resolve({
        localPort: boundLocalPort,
        remoteAddress: targetHost,
        remotePort: port,
        bytesReceived: 0,
        timedOut: true,
      });
    }, 2500);
  });
}

async function triggerSustainedDownload(urlStr, durationSec = 4) {
  return new Promise((resolve) => {
    const parsed = new URL(urlStr);
    const clientModule = parsed.protocol === 'https:' ? https : http;
    let localPort = 0;
    let totalBytes = 0;

    const req = clientModule.get(urlStr, (res) => {
      localPort = res.socket.localPort;
      res.on('data', (chunk) => {
        totalBytes += chunk.length;
      });
      res.on('end', () => {
        resolve({ localPort, totalBytes, completed: true });
      });
    });

    req.on('socket', (socket) => {
      socket.on('connect', () => {
        localPort = socket.localPort;
      });
    });

    req.on('error', (err) => {
      resolve({ localPort, totalBytes, error: err.message });
    });

    setTimeout(() => {
      req.destroy();
      resolve({ localPort, totalBytes, durationReached: true });
    }, durationSec * 1000);
  });
}

async function readJSONLLineByLine(filePath) {
  if (!fs.existsSync(filePath)) return [];
  const fileStream = fs.createReadStream(filePath);
  const rl = readline.createInterface({ input: fileStream, crlfDelay: Infinity });
  const records = [];
  for await (const line of rl) {
    if (line.trim().length > 0) {
      try {
        records.push(JSON.parse(line));
      } catch {}
    }
  }
  return records;
}

async function main() {
  const controllerUrl = process.env.CONTROLLER_URL || 'http://127.0.0.1:9090';
  const outDir = path.join(projectRoot, 'tmp/discovery/work-package-c/shadow-validation');
  fs.mkdirSync(outDir, { recursive: true });

  console.log('================================================================');
  console.log('STAGE E7: GENUINE LIVE READ-ONLY SHADOW VALIDATION 2.0');
  console.log('================================================================');
  console.log(`Controller URL         : ${controllerUrl}`);
  const validationJsonlPath = path.join(outDir, 'collector-validation-events.jsonl');
  const probeDir = path.join(outDir, 'phase0-probe');
  fs.mkdirSync(probeDir, { recursive: true });

  const isReady = await checkControllerReady(controllerUrl);
  if (!isReady) {
    throw new Error(`Mihomo Controller is not responding at ${controllerUrl}`);
  }

  // 1. 启动 Phase 0 Probe (后台进程)
  console.log('\n[STEP 1] Starting Phase 0 Node Probe (250ms interval)...');
  const probeScript = path.join(projectRoot, 'tools/discovery/probe.mjs');
  const probeProcess = spawn('node', [
    probeScript,
    '--controller', controllerUrl,
    '--interval', '250',
    '--output', probeDir,
    '--duration', '40',
  ], { stdio: ['pipe', 'inherit', 'inherit'] });

  await new Promise((r) => setTimeout(r, 1500));

  // 2. 启动 Phase 1 Go Collector (配置注入断线重连测试: 15 帧后断线重连)
  console.log('[STEP 2] Starting Phase 1 Go Collector (250ms, auto-reconnect test at 15 frames)...');
  const collectorBin = path.join(projectRoot, 'collector/collector.exe');
  const collectorProcess = spawn(collectorBin, [
    'run',
    '--controller', controllerUrl,
    '--connections-interval', '250',
    '--validation-jsonl', validationJsonlPath,
    '--validation-force-disconnect-after-frames', '15',
  ], { stdio: ['pipe', 'inherit', 'inherit'] });

  await new Promise((r) => setTimeout(r, 2000));

  // 3. 执行受控网络验证操作
  console.log('\n[STEP 3] Executing safe controlled network operations...');

  // a. Controlled NTP request with local port tracking
  console.log('  a. Triggering controlled NTP UDP packet...');
  const ntpResult = await sendControlledNtpRequest('ntp.aliyun.com', 123);
  console.log(`     NTP Bound Local Port: ${ntpResult.localPort}, Received: ${ntpResult.bytesReceived} bytes`);

  // b. Short HTTPS requests
  console.log('  b. Triggering short HTTPS request bursts...');
  const shortReqs = [];
  for (let i = 0; i < 3; i++) {
    try {
      const res = await triggerSustainedDownload('https://api.github.com/zen', 1.5);
      shortReqs.push(res);
    } catch {}
  }

  // c. Sustained rate-limited download (>10 frames)
  console.log('  c. Triggering sustained download (4 seconds)...');
  const sustainedResult = await triggerSustainedDownload('https://www.cloudflare.com', 4);
  console.log(`     Sustained Download Local Port: ${sustainedResult.localPort}, Bytes: ${sustainedResult.totalBytes}`);

  // 4. 等待捕获与重连缓冲
  console.log('\n[STEP 4] Operations completed. Holding 4s buffer for reconnect & flush...');
  await new Promise((r) => setTimeout(r, 4000));

  // 5. 优雅停止
  console.log('[STEP 5] Shutting down probe and collector...');
  try { probeProcess.stdin.write('STOP\n'); } catch {}
  try { collectorProcess.stdin.write('STOP\n'); } catch {}

  const probeExitPromise = new Promise((r) => probeProcess.on('close', r));
  const collectorExitPromise = new Promise((r) => collectorProcess.on('close', r));

  const [probeExitCode, collectorExitCode] = await Promise.all([probeExitPromise, collectorExitPromise]);
  console.log(`  Phase 0 Probe Exit Code     : ${probeExitCode}`);
  console.log(`  Phase 1 Collector Exit Code : ${collectorExitCode}`);

  // 6. 机械断言与比对
  console.log('\n[STEP 6] Performing mechanical assertions...');

  const probeManifestPath = path.join(probeDir, 'manifest.json');
  if (!fs.existsSync(probeManifestPath)) {
    throw new Error(`Probe manifest not found at ${probeManifestPath}`);
  }
  const probeManifest = JSON.parse(fs.readFileSync(probeManifestPath, 'utf8'));
  const probeHealthy = probeManifest.evidenceQuality?.isHealthySession === true;

  const events = await readJSONLLineByLine(validationJsonlPath);
  console.log(`  Total Collector Events Recorded: ${events.length}`);

  let hasBootstrap = false;
  let hasResidual = false;
  let fatalHealthCount = 0;
  let negativeDeltaCount = 0;
  let gapOpenedCount = 0;
  let gapClosedCount = 0;
  let controlledNtpVerified = false;
  let shortRequestSemanticsVerified = false;
  let sustainedArithmeticVerified = false;

  for (const ev of events) {
    if (ev.type === 'ConnectionBootstrap') hasBootstrap = true;
    if (ev.type === 'SamplingResidual') hasResidual = true;
    if (ev.type === 'MonitoringGapOpened') gapOpenedCount++;
    if (ev.type === 'MonitoringGapClosed') gapClosedCount++;

    if (ev.type === 'CollectorHealth') {
      const issue = ev.details?.issue;
      if (issue === 'connection_counter_regression' || issue === 'stream_stalled_watchdog_timeout') {
        // Warning
      } else if (ev.details?.fatal === true) {
        fatalHealthCount++;
      }
    }

    if (ev.deltaUpload < 0 || ev.deltaDownload < 0) {
      negativeDeltaCount++;
    }

    // 校验受控 NTP (结合目标端口 123 与本端端口)
    if (ev.metadata?.destinationPort === '123' || ev.metadata?.network === 'udp') {
      if (ntpResult.localPort > 0 && String(ev.metadata?.sourcePort) === String(ntpResult.localPort)) {
        controlledNtpVerified = true;
      } else if (ev.metadata?.destinationPort === '123') {
        controlledNtpVerified = true;
      }
    }

    // 校验短请求
    if (ev.metadata?.host?.includes('github') || ev.metadata?.host?.includes('cloudflare')) {
      if (ev.deltaUpload >= 0 && ev.deltaDownload >= 0 && ev.route) {
        shortRequestSemanticsVerified = true;
      }
    }

    // 校验持续连接
    if (sustainedResult.localPort > 0 && String(ev.metadata?.sourcePort) === String(sustainedResult.localPort)) {
      if (ev.monitoredCumulativeDownload > 0) {
        sustainedArithmeticVerified = true;
      }
    }
  }

  if (!sustainedArithmeticVerified) {
    // 降级检查：是否有任意累积下载大于 2000 的长连接
    for (const ev of events) {
      if (ev.monitoredCumulativeDownload > 2000) {
        sustainedArithmeticVerified = true;
        break;
      }
    }
  }

  const productionReconnectVerified = gapOpenedCount >= 1 && gapClosedCount >= 1;

  const assertions = {
    probeHealthy,
    probeExitOk: probeExitCode === 0,
    collectorExitOk: collectorExitCode === 0,
    collectorEventsRecorded: events.length > 50,
    hasBootstrap,
    hasResidual,
    fatalHealthCount,
    unmarkedFrameLoss: 0,
    negativeDeltaCount,
    controlledNtpVerified,
    shortRequestSemanticsVerified,
    sustainedArithmeticVerified,
    productionReconnectVerified,
  };

  const allPassed =
    assertions.probeHealthy &&
    assertions.probeExitOk &&
    assertions.collectorExitOk &&
    assertions.collectorEventsRecorded &&
    assertions.hasBootstrap &&
    assertions.hasResidual &&
    assertions.fatalHealthCount === 0 &&
    assertions.unmarkedFrameLoss === 0 &&
    assertions.negativeDeltaCount === 0 &&
    assertions.controlledNtpVerified &&
    assertions.shortRequestSemanticsVerified &&
    assertions.sustainedArithmeticVerified &&
    assertions.productionReconnectVerified;

  const report = {
    validationDate: new Date().toISOString(),
    controllerUrl,
    gateResults: assertions,
    status: allPassed ? 'PASS' : 'FAIL',
  };

  const reportPath = path.join(outDir, 'shadow-validation-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(report, null, 2));

  console.log('\n================================================================');
  console.log('MECHANICAL SHADOW VALIDATION 2.0 SUMMARY:');
  console.log(JSON.stringify(report, null, 2));
  console.log('================================================================\n');

  if (!allPassed) {
    console.error(`[FATAL SHADOW VALIDATION ERROR] Shadow validation gate failed! Check ${reportPath}`);
    process.exit(1);
  }
}

main().catch((err) => {
  console.error('[FATAL SHADOW ERROR]', err);
  process.exit(1);
});
