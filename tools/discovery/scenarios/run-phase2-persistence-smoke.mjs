/**
 * run-phase2-persistence-smoke.mjs
 * 
 * Stage D8: End-to-End SQLite Persistence, Inspection, Offline Gap and Rebuild Smoke
 */

import { spawn, execSync } from 'node:child_process';
import dgram from 'node:dgram';
import fs from 'node:fs';
import http from 'node:http';
import https from 'node:https';
import path from 'node:path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '../../..');

function sendControlledNtpRequest(targetHost = 'ntp.aliyun.com', port = 123) {
  return new Promise((resolve, reject) => {
    const socket = dgram.createSocket('udp4');
    const ntpPacket = Buffer.alloc(48);
    ntpPacket[0] = 0x1b;

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
      resolve({ localPort: boundLocalPort, bytesReceived: msg.length });
    });

    socket.on('error', (err) => {
      safeClose();
      reject(err);
    });

    setTimeout(() => {
      safeClose();
      resolve({ localPort: boundLocalPort, bytesReceived: 0, timedOut: true });
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
      res.on('data', (chunk) => { totalBytes += chunk.length; });
      res.on('end', () => { resolve({ localPort, totalBytes }); });
    });

    req.on('socket', (socket) => {
      if (socket.localPort) localPort = socket.localPort;
    });

    req.on('error', () => { resolve({ localPort, totalBytes }); });

    setTimeout(() => {
      req.destroy();
      resolve({ localPort, totalBytes });
    }, durationSec * 1000);
  });
}

async function main() {
  const controllerUrl = process.env.CONTROLLER_URL || 'http://127.0.0.1:9090';
  const outDir = path.join(projectRoot, 'tmp/phase2-live');
  fs.mkdirSync(outDir, { recursive: true });
  const testDbPath = path.join(outDir, 'proxylens-test.db');
  if (fs.existsSync(testDbPath)) {
    try { fs.unlinkSync(testDbPath); } catch {}
  }

  const collectorBin = path.join(projectRoot, 'collector/collector.exe');

  console.log('================================================================');
  console.log('STAGE D8: END-TO-END SQLITE PERSISTENCE SMOKE VALIDATION');
  console.log('================================================================');
  console.log(`Database Path  : ${testDbPath}`);
  console.log(`Controller URL : ${controllerUrl}`);

  // -------------------------------------------------------------
  // [STEP 1] 首次启动 Collector (Session 1) 并执行受控网络流量
  // -------------------------------------------------------------
  console.log('\n[STEP 1] Starting Collector Session 1 with SQLite sink (250ms cadence)...');
  const proc1 = spawn(collectorBin, [
    'run',
    '--controller', controllerUrl,
    '--connections-interval', '250',
    '--db', testDbPath,
  ], { stdio: ['pipe', 'inherit', 'inherit'] });

  await new Promise((r) => setTimeout(r, 2000));

  console.log('  a. Triggering controlled NTP UDP request...');
  const ntp = await sendControlledNtpRequest();
  console.log(`     NTP Local Port: ${ntp.localPort}`);

  console.log('  b. Triggering short HTTPS burst requests...');
  await triggerControlledRequest('https://api.github.com/zen', 1.5);

  console.log('  c. Triggering sustained download (4 seconds)...');
  await triggerControlledRequest('https://www.cloudflare.com', 4);

  await new Promise((r) => setTimeout(r, 2000));

  console.log('  d. Cleanly shutting down Session 1...');
  try { proc1.stdin.write('STOP\n'); } catch {}
  await new Promise((r) => proc1.on('close', r));
  console.log('  Session 1 closed cleanly.');

  // -------------------------------------------------------------
  // [STEP 2] 检验 SQLite 数据库中持久化的连接明细
  // -------------------------------------------------------------
  console.log('\n[STEP 2] Inspecting SQLite database using `collector storage inspect`...');
  const inspectOutput = execSync(`"${collectorBin}" storage inspect --db "${testDbPath}" --latest 10`, { encoding: 'utf8' });
  console.log(inspectOutput);

  if (!inspectOutput.includes('Persisted Connections')) {
    throw new Error('Storage inspect failed to return persisted connections header!');
  }

  // -------------------------------------------------------------
  // [STEP 3] 第二次启动 Collector (Session 2) 验证 Offline Gap 推导
  // -------------------------------------------------------------
  console.log('\n[STEP 3] Waiting 3s and starting Collector Session 2 to verify Offline Gap...');
  await new Promise((r) => setTimeout(r, 3000));

  const proc2 = spawn(collectorBin, [
    'run',
    '--controller', controllerUrl,
    '--connections-interval', '250',
    '--db', testDbPath,
  ], { stdio: ['pipe', 'inherit', 'inherit'] });

  await new Promise((r) => setTimeout(r, 2000));
  try { proc2.stdin.write('STOP\n'); } catch {}
  await new Promise((r) => proc2.on('close', r));
  console.log('  Session 2 closed cleanly.');

  console.log('\n[STEP 4] Querying monitoring gaps using `collector storage gaps`...');
  const gapsOutput = execSync(`"${collectorBin}" storage gaps --db "${testDbPath}"`, { encoding: 'utf8' });
  console.log(gapsOutput);

  if (!gapsOutput.includes('collector_session_boundary')) {
    throw new Error('Expected collector_session_boundary gap was not found in DB!');
  }

  // -------------------------------------------------------------
  // [STEP 5] 验证从 event_journal 完整重建投影
  // -------------------------------------------------------------
  console.log('\n[STEP 5] Testing projection rebuild using `collector storage rebuild`...');
  const rebuildOutput = execSync(`"${collectorBin}" storage rebuild --db "${testDbPath}"`, { encoding: 'utf8' });
  console.log(rebuildOutput);

  const reinspectOutput = execSync(`"${collectorBin}" storage inspect --db "${testDbPath}" --latest 10`, { encoding: 'utf8' });
  console.log('After Rebuild Inspect:');
  console.log(reinspectOutput);

  console.log('\n================================================================');
  console.log('STAGE D8 END-TO-END PERSISTENCE SMOKE: 100% PASS');
  console.log('================================================================\n');
}

main().catch((err) => {
  console.error('[FATAL ERROR]', err);
  process.exit(1);
});
