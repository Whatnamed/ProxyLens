#!/usr/bin/env node

import { execFileSync, spawn, spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const repoRoot = path.resolve(path.dirname(__filename), '..', '..');
const collectorDir = path.join(repoRoot, 'collector');
const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-quiescence-'));
const dataDir = path.join(tempRoot, 'data');
const configDir = path.join(tempRoot, 'config');
fs.mkdirSync(dataDir, { recursive: true });
fs.mkdirSync(configDir, { recursive: true });

const supervisorExe = path.join(collectorDir, 'proxylens-supervisor.exe');
const runtimeExe = path.join(collectorDir, 'proxylens-runtime.exe');

function runPowerShell(script) {
  const result = spawnSync('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', script], {
    cwd: repoRoot,
    encoding: 'utf8',
    windowsHide: true,
  });
  if (result.status !== 0) {
    throw new Error(`PowerShell failed: ${result.stderr || result.stdout}`);
  }
  return result.stdout.trim();
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function assertMockControllerUrl(controllerUrl) {
  if (!controllerUrl) throw new Error('Mock Controller URL is required');
  const parsed = new URL(controllerUrl);
  if (parsed.protocol !== 'http:' || parsed.hostname !== '127.0.0.1' || !parsed.port) {
    throw new Error(`Quiescence test requires a random 127.0.0.1 mock Controller URL, got ${controllerUrl}`);
  }
  const port = Number(parsed.port);
  if (port === 9090 || port === 7988) {
    throw new Error(`Forbidden controller port: ${port}`);
  }
}

async function stopExactProcess(child) {
  if (!child || child.exitCode !== null) return;
  try { child.kill(); } catch {}
  await new Promise((resolve) => child.once('exit', resolve));
}

async function stopExactPid(pid) {
  if (!pid || pid <= 0) return;
  try { runPowerShell(`Stop-Process -Id ${pid} -Force`); } catch {}
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    try {
      process.kill(pid, 0);
      await sleep(100);
    } catch {
      break;
    }
  }
}

function encodeWebSocketText(payload) {
  const body = Buffer.from(payload, 'utf8');
  if (body.length < 126) return Buffer.concat([Buffer.from([0x81, body.length]), body]);
  const header = Buffer.alloc(4);
  header[0] = 0x81;
  header[1] = 126;
  header.writeUInt16BE(body.length, 2);
  return Buffer.concat([header, body]);
}

async function startMockController() {
  const sockets = new Set();
  const frame = encodeWebSocketText(JSON.stringify({
    uploadTotal: 10,
    downloadTotal: 20,
    connections: [],
  }));

  const server = http.createServer((request, response) => {
    const url = new URL(request.url || '/', 'http://127.0.0.1');
    if (url.pathname === '/version') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ version: 'mock-quiescence', premium: true }));
      return;
    }
    response.writeHead(404);
    response.end();
  });

  server.on('upgrade', (request, socket) => {
    const url = new URL(request.url || '/', 'http://127.0.0.1');
    if (url.pathname !== '/connections' || !request.headers['sec-websocket-key']) {
      socket.destroy();
      return;
    }
    const accept = crypto.createHash('sha1')
      .update(`${request.headers['sec-websocket-key']}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`)
      .digest('base64');
    socket.write(['HTTP/1.1 101 Switching Protocols', 'Upgrade: websocket', 'Connection: Upgrade', `Sec-WebSocket-Accept: ${accept}`, '\r\n'].join('\r\n'));
    sockets.add(socket);
    const interval = setInterval(() => {
      if (!socket.destroyed) socket.write(frame);
    }, 200);
    const cleanup = () => {
      clearInterval(interval);
      sockets.delete(socket);
    };
    socket.on('close', cleanup);
    socket.on('error', cleanup);
  });

  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const port = server.address().port;
  assertMockControllerUrl(`http://127.0.0.1:${port}`);
  return {
    url: `http://127.0.0.1:${port}`,
    close: async () => {
      for (const s of sockets) s.destroy();
      await new Promise((resolve) => server.close(resolve));
    },
  };
}

function runControlStop(dbPath, wait = '5s') {
  const result = spawnSync(supervisorExe, ['control', 'stop', '--db', dbPath, '--wait', wait], {
    cwd: collectorDir,
    encoding: 'utf8',
    windowsHide: true,
  });
  let status = null;
  try {
    status = JSON.parse(result.stdout.trim());
  } catch {}
  return {
    exitCode: result.status,
    stdout: result.stdout.trim(),
    stderr: result.stderr.trim(),
    status,
  };
}

function findProcessPid(name, dbPath) {
  const script = `Get-CimInstance Win32_Process -Filter "Name = '${name}'" | Where-Object { $_.CommandLine -match [regex]::Escape('${dbPath.replace(/'/g, "''")}') } | Select-Object -ExpandProperty ProcessId`;
  const output = runPowerShell(script);
  if (!output) return null;
  const pids = output.split(/\r?\n/).map((s) => parseInt(s.trim(), 10)).filter(Number.isInteger);
  return pids[0] || null;
}

async function main() {
  console.log('=== Quiescence Fail-Closed Acceptance Verification ===');
  console.log(`Temp dir: ${tempRoot}`);

  const mock = await startMockController();
  console.log(`Mock Controller running at ${mock.url}`);

  const trackedPids = new Set();

  try {
    // --- TEST A: No Owner / No Runtime -> control stop exits 0 ---
    console.log('\n--- Test A: No Owner / No Runtime ---');
    const dbA = path.join(dataDir, 'test-a.db');
    const resA = runControlStop(dbA);
    console.log(`Test A result: exitCode=${resA.exitCode}, status=${JSON.stringify(resA.status)}`);
    if (resA.exitCode !== 0) {
      throw new Error(`Test A failed: expected exit 0, got ${resA.exitCode}`);
    }
    if (!resA.status || resA.status.supervisorRunning !== false || resA.status.runtimeRunning !== false) {
      throw new Error(`Test A status failed: expected both false, got ${JSON.stringify(resA.status)}`);
    }
    console.log('PASS Test A: Clean exit 0 when nothing is running.');

    // --- TEST B: Supervisor Owns Runtime -> control stop exits 0 ---
    console.log('\n--- Test B: Supervisor Owns Runtime ---');
    const dbB = path.join(dataDir, 'test-b.db');
    const supProcessB = spawn(supervisorExe, [
      '--db', dbB,
      '--runtime-exe', runtimeExe,
      '--controller', mock.url,
    ], {
      cwd: collectorDir,
      env: {
        ...process.env,
        PROXYLENS_CONTROLLER_URL: mock.url,
      },
      stdio: 'ignore',
      windowsHide: true,
    });
    trackedPids.add(supProcessB.pid);

    // Wait for supervisor and runtime to initialize
    let supPidB = null;
    let runPidB = null;
    for (let i = 0; i < 30; i++) {
      await sleep(500);
      supPidB = findProcessPid('proxylens-supervisor.exe', dbB);
      runPidB = findProcessPid('proxylens-runtime.exe', dbB);
      if (supPidB && runPidB) break;
    }
    console.log(`Test B processes: Supervisor PID=${supPidB}, Runtime PID=${runPidB}`);
    if (!supPidB || !runPidB) {
      throw new Error('Test B failed to launch Supervisor and owned Runtime');
    }
    trackedPids.add(runPidB);

    // Run control stop on dbB
    console.log('Running control stop on dbB...');
    const resB = runControlStop(dbB, '10s');
    console.log(`Test B stop result: exitCode=${resB.exitCode}, status=${JSON.stringify(resB.status)}`);
    if (resB.exitCode !== 0) {
      throw new Error(`Test B failed: expected exit 0, got ${resB.exitCode} (stderr: ${resB.stderr})`);
    }
    if (!resB.status || resB.status.supervisorRunning !== false || resB.status.runtimeRunning !== false) {
      throw new Error(`Test B status failed: expected both false, got ${JSON.stringify(resB.status)}`);
    }

    // Verify both processes are dead
    await sleep(1000);
    const supCheckB = findProcessPid('proxylens-supervisor.exe', dbB);
    const runCheckB = findProcessPid('proxylens-runtime.exe', dbB);
    if (supCheckB || runCheckB) {
      throw new Error(`Test B: processes did not terminate cleanly: sup=${supCheckB}, run=${runCheckB}`);
    }
    console.log('PASS Test B: Owned runtime and supervisor both terminated cleanly, exit 0.');

    // --- TEST C: Orphan / External Runtime Without Supervisor -> control stop fails closed ---
    console.log('\n--- Test C: Orphan Runtime Fails Closed ---');
    const dbC = path.join(dataDir, 'test-c.db');
    // Start Runtime directly without Supervisor
    const runProcessC = spawn(runtimeExe, [
      '--db', dbC,
      '--controller', mock.url,
    ], {
      cwd: collectorDir,
      env: {
        ...process.env,
        PROXYLENS_CONTROLLER_URL: mock.url,
      },
      stdio: 'ignore',
      windowsHide: true,
    });
    trackedPids.add(runProcessC.pid);

    let runPidC = null;
    for (let i = 0; i < 30; i++) {
      await sleep(500);
      runPidC = findProcessPid('proxylens-runtime.exe', dbC);
      if (runPidC) break;
    }
    console.log(`Test C orphan Runtime running: PID=${runPidC}`);
    if (!runPidC) {
      throw new Error('Test C failed to launch direct Runtime');
    }

    // Run control stop on dbC: must fail-closed (non-zero exit)
    console.log('Running control stop against orphan runtime...');
    const resC = runControlStop(dbC, '5s');
    console.log(`Test C stop result: exitCode=${resC.exitCode}, stdout=${resC.stdout}, stderr=${resC.stderr}`);
    if (resC.exitCode === 0) {
      throw new Error('Test C FAILED: control stop unexpectedly succeeded with exit 0 when runtime is active!');
    }
    if (resC.exitCode !== 1) {
      throw new Error(`Test C FAILED: expected exit code 1, got ${resC.exitCode}`);
    }
    if (!resC.status || resC.status.supervisorRunning !== false || resC.status.runtimeRunning !== true) {
      throw new Error(`Test C status failed: expected sup=false/run=true, got ${JSON.stringify(resC.status)}`);
    }
    if (!resC.stderr.includes('not quiesced')) {
      throw new Error(`Test C stderr did not contain 'not quiesced': ${resC.stderr}`);
    }

    // Verify Runtime PID is STILL RUNNING (control stop must NOT have killed it)
    await sleep(500);
    const runCheckC = findProcessPid('proxylens-runtime.exe', dbC);
    console.log(`Test C Runtime PID after control stop: ${runCheckC}`);
    if (runCheckC !== runPidC) {
      throw new Error(`Test C FAILED: Runtime was killed or terminated! Got ${runCheckC}, want ${runPidC}`);
    }
    console.log('PASS Test C: control stop failed closed (exit 1), reported runtimeRunning=true, and did not kill Runtime.');

    // Now clean up orphan runtime safely by exact recorded PID
    await stopExactPid(runPidC);
    console.log('Cleaned up orphan Runtime PID.');

    // --- TEST D: NSIS PREINSTALL Gate Contract Simulation ---
    console.log('\n--- Test D: NSIS PREINSTALL Gate Contract ---');
    // Simulate NSIS hooks:
    // ExecWait '"$INSTDIR\proxylens-supervisor.exe" control stop --wait 15s' $0
    // IntCmp $0 0 preinstall_end preinstall_stop_fail preinstall_stop_fail
    // preinstall_stop_fail:
    //   Abort "ProxyLens could not stop the previous background owner safely."

    // Scenario 1: Clean state -> NSIS proceeds
    const dbD1 = path.join(dataDir, 'test-d1.db');
    const hookCheckD1 = runControlStop(dbD1);
    const nsisResultD1 = hookCheckD1.exitCode === 0 ? 'PROCEED' : 'ABORT';
    console.log(`Scenario 1 (Clean): ExitCode=${hookCheckD1.exitCode} -> NSIS will ${nsisResultD1}`);
    if (nsisResultD1 !== 'PROCEED') {
      throw new Error('Test D Scenario 1 failed: clean state should PROCEED');
    }

    // Scenario 2: Orphan runtime active -> NSIS aborts
    const dbD2 = path.join(dataDir, 'test-d2.db');
    const runProcessD2 = spawn(runtimeExe, [
      '--db', dbD2,
      '--controller', mock.url,
    ], {
      cwd: collectorDir,
      env: {
        ...process.env,
        PROXYLENS_CONTROLLER_URL: mock.url,
      },
      stdio: 'ignore',
      windowsHide: true,
    });
    let runPidD2 = null;
    for (let i = 0; i < 30; i++) {
      await sleep(500);
      runPidD2 = findProcessPid('proxylens-runtime.exe', dbD2);
      if (runPidD2) break;
    }
    trackedPids.add(runPidD2);
    console.log(`Scenario 2 active orphan PID: ${runPidD2}`);

    const hookCheckD2 = runControlStop(dbD2, '5s');
    const nsisResultD2 = hookCheckD2.exitCode === 0 ? 'PROCEED' : 'ABORT';
    console.log(`Scenario 2 (Orphan active): ExitCode=${hookCheckD2.exitCode} -> NSIS will ${nsisResultD2}`);
    if (nsisResultD2 !== 'ABORT') {
      throw new Error('Test D Scenario 2 failed: active orphan runtime MUST cause NSIS to ABORT!');
    }

    // Clean up D2
    await stopExactPid(runPidD2);
    console.log('PASS Test D: NSIS PREINSTALL contract verified: orphan runtime triggers ABORT, protecting files from corruption.');

    console.log('\n======================================================');
    console.log('=== ALL TESTS A, B, C, D PASSED SUCCESSFULLY ===');
    console.log('======================================================');

  } finally {
    console.log('\nCleaning up test processes and files...');
    for (const pid of trackedPids) {
      if (pid) await stopExactPid(pid);
    }
    await mock.close();
    try { fs.rmSync(tempRoot, { recursive: true, force: true }); } catch {}
    console.log('Cleanup complete.');
  }
}

main().catch((err) => {
  console.error('Test failed:', err);
  process.exit(1);
});
