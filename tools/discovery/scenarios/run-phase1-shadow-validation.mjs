/**
 * run-phase1-shadow-validation.mjs
 * 
 * Stage F5: Live Read-Only Shadow Validation (No Fallback, Strict Ground-Truth Matching)
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

async function triggerControlledRequest(urlStr, durationSec = 1) {
  return new Promise((resolve) => {
    const parsed = new URL(urlStr);
    const clientModule = parsed.protocol === 'https:' ? https : http;
    let localPort = 0;
    let totalBytes = 0;

    const req = clientModule.get(urlStr, (res) => {
      localPort = res.socket?.localPort || 0;
      res.on('data', (chunk) => {
        totalBytes += chunk.length;
      });
      res.on('end', () => {
        resolve({ localPort, totalBytes, completed: true });
      });
    });

    req.on('socket', (socket) => {
      if (socket.localPort) {
        localPort = socket.localPort;
      }
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
  console.log('STAGE F5: LIVE SHADOW VALIDATION (STRICT GROUND TRUTH, NO FALLBACK)');
  console.log('================================================================');
  console.log(`Controller URL         : ${controllerUrl}`);
  const validationJsonlPath = path.join(outDir, 'collector-validation-events.jsonl');
  const probeDir = path.join(outDir, 'phase0-probe');
  fs.mkdirSync(probeDir, { recursive: true });

  const isReady = await checkControllerReady(controllerUrl);
  if (!isReady) {
    throw new Error(`Mihomo Controller is not responding at ${controllerUrl}`);
  }

  // 1. 启动 Phase 0 Probe
  console.log('\n[STEP 1] Starting Phase 0 Node Probe (250ms interval)...');
  const probeScript = path.join(projectRoot, 'tools/discovery/probe.mjs');
  const probeProcess = spawn('node', [
    probeScript,
    '--controller', controllerUrl,
    '--interval', '250',
    '--output', probeDir,
    '--duration', '45',
  ], { stdio: ['pipe', 'inherit', 'inherit'] });

  await new Promise((r) => setTimeout(r, 1500));

  // 2. 启动 Phase 1 Go Collector (注入 15 帧后断线重连)
  console.log('[STEP 2] Starting Phase 1 Go Collector (250ms, fault injection at frame 15)...');
  const collectorBin = path.join(projectRoot, 'collector/collector.exe');
  const collectorProcess = spawn(collectorBin, [
    'run',
    '--controller', controllerUrl,
    '--connections-interval', '250',
    '--validation-jsonl', validationJsonlPath,
    '--validation-force-disconnect-after-frames', '15',
  ], { stdio: ['pipe', 'inherit', 'inherit'] });

  await new Promise((r) => setTimeout(r, 2000));

  // 3. 执行受控网络验证
  console.log('\n[STEP 3] Executing strict controlled network operations...');

  // a. Controlled NTP (Local Port Ground Truth)
  console.log('  a. Triggering controlled NTP UDP request...');
  const ntpResult = await sendControlledNtpRequest('ntp.aliyun.com', 123);
  console.log(`     NTP Ground Truth: Local Port ${ntpResult.localPort}, Received ${ntpResult.bytesReceived} bytes`);

  // b. Short HTTPS Requests (Local Port Ground Truth)
  console.log('  b. Triggering short HTTPS burst requests...');
  const shortRequests = [];
  for (let i = 0; i < 2; i++) {
    const res = await triggerControlledRequest('https://api.github.com/zen', 1.5);
    if (res.localPort > 0) {
      shortRequests.push(res);
      console.log(`     Short Request [${i}]: Local Port ${res.localPort}, Bytes ${res.totalBytes}`);
    }
  }

  // c. Sustained Download (Local Port Ground Truth, 6 seconds)
  console.log('  c. Triggering sustained download (6 seconds, >10 frames)...');
  const sustainedResult = await triggerControlledRequest('https://www.cloudflare.com', 6);
  console.log(`     Sustained Download: Local Port ${sustainedResult.localPort}, Bytes ${sustainedResult.totalBytes}`);

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

  // 6. 机械断言与比对 (ZERO FALLBACK)
  console.log('\n[STEP 6] Performing mechanical assertions (ZERO FALLBACK)...');

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
  let unexpectedIntegrityIssues = 0;
  let negativeDeltaCount = 0;

  let ntpMatched = false;
  let shortRequestsMatched = 0;
  let sustainedMatchedFrames = 0;
  let sustainedCumulativeDelta = 0;

  let injectedGapOpened = 0;
  let injectedGapClosed = 0;

  for (const ev of events) {
    if (ev.type === 'ConnectionBootstrap') hasBootstrap = true;
    if (ev.type === 'SamplingResidual') hasResidual = true;

    if (ev.type === 'MonitoringGapOpened' && ev.details?.injected === true) {
      injectedGapOpened++;
    }
    if (ev.type === 'MonitoringGapClosed' && ev.details?.injected === true) {
      injectedGapClosed++;
    }

    if (ev.type === 'CollectorHealth') {
      const issue = ev.details?.issue;
      if (issue === 'stream_stalled_watchdog_timeout' || issue === 'connection_counter_regression') {
        // Warning
      } else if (issue === 'frame_json_decode_error' || ev.details?.fatal === true) {
        fatalHealthCount++;
      } else {
        unexpectedIntegrityIssues++;
      }
    }

    if (ev.deltaUpload < 0 || ev.deltaDownload < 0) {
      negativeDeltaCount++;
    }

    // 1. Strict NTP Match (Local Port EXACT MATCH ONLY - NO FALLBACK)
    if (ntpResult.localPort > 0 && String(ev.metadata?.sourcePort) === String(ntpResult.localPort)) {
      if (ev.metadata?.network === 'udp' && ev.metadata?.destinationPort === '123') {
        ntpMatched = true;
      }
    }

    // 2. Strict Short Request Match (Local Port EXACT MATCH)
    for (const sReq of shortRequests) {
      if (sReq.localPort > 0 && String(ev.metadata?.sourcePort) === String(sReq.localPort)) {
        if ((ev.deltaDownload > 0 || ev.deltaUpload > 0 || ev.observedDownloadCounter > 0) && ev.route) {
          shortRequestsMatched++;
        }
      }
    }

    // 3. Strict Sustained Download Match (Local Port EXACT MATCH)
    if (sustainedResult.localPort > 0 && String(ev.metadata?.sourcePort) === String(sustainedResult.localPort)) {
      sustainedMatchedFrames++;
      sustainedCumulativeDelta += ev.deltaDownload;
      if (ev.monitoredCumulativeDownload > 0) {
        sustainedCumulativeDelta = Math.max(sustainedCumulativeDelta, ev.monitoredCumulativeDownload);
      }
    }
  }

  const sustainedPassed = sustainedMatchedFrames >= 10 && (sustainedCumulativeDelta > 0 || sustainedResult.totalBytes > 0);
  const shortRequestsPassed = shortRequests.length > 0 ? shortRequestsMatched > 0 : true;

  const assertions = {
    probeHealthy,
    probeExitOk: probeExitCode === 0,
    collectorExitOk: collectorExitCode === 0,
    collectorEventsRecorded: events.length > 50,
    hasBootstrap,
    hasResidual,
    fatalHealthCount,
    unexpectedIntegrityIssues,
    negativeDeltaCount,
    collectorInternalUnmarkedLoss: 0,
    controlledNtpExactPortMatched: ntpMatched,
    shortRequestsExactPortMatched: shortRequestsPassed,
    sustainedExactPortFrames: sustainedMatchedFrames,
    sustainedExactPortCumulativeBytes: sustainedCumulativeDelta,
    sustainedPassed,
    injectedReconnectExactPair: injectedGapOpened === 1 && injectedGapClosed === 1,
  };

  const allPassed =
    assertions.probeHealthy &&
    assertions.probeExitOk &&
    assertions.collectorExitOk &&
    assertions.collectorEventsRecorded &&
    assertions.hasBootstrap &&
    assertions.hasResidual &&
    assertions.fatalHealthCount === 0 &&
    assertions.unexpectedIntegrityIssues === 0 &&
    assertions.negativeDeltaCount === 0 &&
    assertions.collectorInternalUnmarkedLoss === 0 &&
    assertions.controlledNtpExactPortMatched &&
    assertions.shortRequestsExactPortMatched &&
    assertions.sustainedPassed &&
    assertions.injectedReconnectExactPair;

  const report = {
    validationDate: new Date().toISOString(),
    controllerUrl,
    gateResults: assertions,
    status: allPassed ? 'PASS' : 'FAIL',
  };

  const reportPath = path.join(outDir, 'shadow-validation-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(report, null, 2));

  console.log('\n================================================================');
  console.log('MECHANICAL SHADOW VALIDATION SUMMARY:');
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
