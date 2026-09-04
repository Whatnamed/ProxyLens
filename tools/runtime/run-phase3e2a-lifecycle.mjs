#!/usr/bin/env node

/**
 * Phase 3E-2A Windows Runtime ownership lifecycle smoke.
 *
 * This test deliberately creates its own HTTP/WebSocket mock Controller on a
 * random loopback port. It never reads or targets a user's Controller port,
 * and every Runtime database is below one temporary directory owned by this
 * invocation.
 */

import { spawn } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const rootDir = path.resolve(__dirname, '..', '..');
const tauriBinary = path.resolve(
  process.env.PROXYLENS_TAURI_EXE || path.join(rootDir, 'ui', 'src-tauri', 'target', 'release', 'proxylens-desktop.exe'),
);
const runtimeBinaryOverride = process.env.PROXYLENS_RUNTIME_EXE;

const forbiddenControllerPorts = new Set([9000 + 90, 7900 + 88]);
const syntheticControllerSecret = 'phase3e2a-synthetic-secret';
const trackedProcesses = new Set();
const trackedRuntimePids = new Set();
const trackedSupervisorPids = new Set();

function requireFile(filePath, label) {
  if (!fs.statSync(filePath, { throwIfNoEntry: false })?.isFile()) {
    throw new Error(`${label} is missing: ${filePath}. Build the current release binary before running this smoke.`);
  }
}

function resolveRuntimeBinary() {
  if (runtimeBinaryOverride) {
    const resolved = path.resolve(runtimeBinaryOverride);
    requireFile(resolved, 'Runtime binary');
    return resolved;
  }
  const binariesDir = path.join(rootDir, 'ui', 'src-tauri', 'binaries');
  const candidates = fs.readdirSync(binariesDir, { withFileTypes: true })
    .filter((entry) => entry.isFile() && /^proxylens-runtime-[^.]+\.exe$/.test(entry.name))
    .map((entry) => path.join(binariesDir, entry.name));
  if (candidates.length !== 1) {
    throw new Error(`Expected exactly one bundled Runtime binary in ${binariesDir}, found ${candidates.length}.`);
  }
  requireFile(candidates[0], 'Bundled Runtime binary');
  return candidates[0];
}

function assertMockControllerUrl(controllerUrl) {
  if (!controllerUrl) {
    throw new Error('Mock Controller URL is required before any process launch.');
  }
  const parsed = new URL(controllerUrl);
  if (parsed.protocol !== 'http:' || parsed.hostname !== '127.0.0.1' || !parsed.port) {
    throw new Error(`Lifecycle smoke requires a random 127.0.0.1 mock Controller URL, got ${controllerUrl}`);
  }
  if (forbiddenControllerPorts.has(Number(parsed.port))) {
    throw new Error(`Refusing forbidden conventional Controller port ${parsed.port}`);
  }
  if (parsed.pathname !== '/' || parsed.search || parsed.hash || parsed.username || parsed.password) {
    throw new Error(`Unexpected mock Controller URL shape: ${controllerUrl}`);
  }
}

function encodeWebSocketText(payload) {
  const body = Buffer.from(payload, 'utf8');
  if (body.length < 126) {
    return Buffer.concat([Buffer.from([0x81, body.length]), body]);
  }
  if (body.length < 65536) {
    const header = Buffer.alloc(4);
    header[0] = 0x81;
    header[1] = 126;
    header.writeUInt16BE(body.length, 2);
    return Buffer.concat([header, body]);
  }
  const header = Buffer.alloc(10);
  header[0] = 0x81;
  header[1] = 127;
  header.writeBigUInt64BE(BigInt(body.length), 2);
  return Buffer.concat([header, body]);
}

async function startMockController(expectedSecret) {
  const requests = [];
  const sockets = new Set();
  const frame = encodeWebSocketText(JSON.stringify({
    uploadTotal: 11,
    downloadTotal: 23,
    connections: [{
      id: 'mock-phase3e2a-connection',
      metadata: {
        network: 'tcp',
        destinationIP: '203.0.113.10',
        destinationPort: '443',
        host: 'mock.phase3e2a.test',
        process: 'mock-runtime-test.exe',
        processPath: 'C:\\Mock\\mock-runtime-test.exe',
      },
      upload: 11,
      download: 23,
      start: '2026-09-04T00:00:00Z',
      chains: ['Mock-Node'],
      rule: 'MATCH',
      rulePayload: 'MATCH',
    }],
  }));

  const server = http.createServer((request, response) => {
    const requestPath = new URL(request.url || '/', 'http://127.0.0.1').pathname;
    requests.push({ method: request.method, path: requestPath, authMatches: request.headers.authorization === `Bearer ${expectedSecret}` });
    if (request.method !== 'GET') {
      response.writeHead(405);
      response.end();
      return;
    }
    if (requestPath === '/version') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ meta: true, version: 'mock-phase3e2a' }));
      return;
    }
    response.writeHead(404);
    response.end();
  });

  server.on('upgrade', (request, socket) => {
    const requestPath = new URL(request.url || '/', 'http://127.0.0.1').pathname;
    requests.push({ method: request.method, path: requestPath, authMatches: request.headers.authorization === `Bearer ${expectedSecret}` });
    if (request.method !== 'GET' || requestPath !== '/connections') {
      socket.destroy();
      return;
    }
    const key = request.headers['sec-websocket-key'];
    if (typeof key !== 'string') {
      socket.destroy();
      return;
    }
    const accept = crypto
      .createHash('sha1')
      .update(`${key}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`)
      .digest('base64');
    socket.write([
      'HTTP/1.1 101 Switching Protocols',
      'Upgrade: websocket',
      'Connection: Upgrade',
      `Sec-WebSocket-Accept: ${accept}`,
      '\r\n',
    ].join('\r\n'));
    socket.setTimeout(0);
    sockets.add(socket);
    const timer = setInterval(() => {
      if (!socket.destroyed) socket.write(frame);
    }, 100);
    const remove = () => {
      clearInterval(timer);
      sockets.delete(socket);
    };
    socket.on('close', remove);
    socket.on('error', remove);
  });

  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  if (!address || typeof address === 'string' || !address.port) {
    throw new Error('Mock Controller did not expose a random loopback port.');
  }
  const url = `http://127.0.0.1:${address.port}`;
  assertMockControllerUrl(url);

  return {
    url,
    requests,
    async close() {
      for (const socket of sockets) socket.destroy();
      await new Promise((resolve) => server.close(resolve));
    },
  };
}

class ProcessCapture {
  constructor(child, label) {
    this.child = child;
    this.label = label;
    this.lines = [];
    this.buffer = '';
    this.stderr = [];
    this.stderrBuffer = '';
    this.closed = null;
    this.waiters = [];

    child.stdout.setEncoding('utf8');
    child.stdout.on('data', (chunk) => this.consumeStdout(chunk));
    child.stderr.setEncoding('utf8');
    child.stderr.on('data', (chunk) => {
      this.stderr.push(chunk);
      if (this.stderr.length > 40) this.stderr.shift();
      this.consumeStderr(chunk);
    });
    child.on('error', (error) => this.rejectWaiters(new Error(`${this.label} process error: ${error.message}`)));
    const finish = (code, signal) => {
      if (this.closed) return;
      if (this.buffer) this.emitLine(this.buffer);
      if (this.stderrBuffer) this.emitLine(this.stderrBuffer);
      this.closed = { code, signal };
      for (const waiter of this.waiters.splice(0)) {
        clearTimeout(waiter.timer);
        waiter.reject(new Error(`${this.label} exited before expected output (code=${code}, signal=${signal})`));
      }
    };
    // Windows GUI-subsystem processes can close inherited stdio pipes later
    // than their process handle. Exit is the lifecycle boundary we need;
    // close remains a harmless duplicate fallback.
    child.on('exit', finish);
    child.on('close', finish);
  }

  consumeStdout(chunk) {
    this.buffer += chunk;
    const parts = this.buffer.split(/\r?\n/);
    this.buffer = parts.pop() || '';
    for (const line of parts) this.emitLine(line);
  }

  consumeStderr(chunk) {
    this.stderrBuffer += chunk;
    const parts = this.stderrBuffer.split(/\r?\n/);
    this.stderrBuffer = parts.pop() || '';
    for (const line of parts) this.emitLine(line);
  }

  emitLine(line) {
    const trimmed = line.trim();
    if (!trimmed) return;
    this.lines.push(trimmed);
    if (this.lines.length > 1000) this.lines.shift();
    for (const waiter of [...this.waiters]) {
      let matched = false;
      try {
        matched = waiter.predicate(trimmed);
      } catch (error) {
        clearTimeout(waiter.timer);
        this.waiters = this.waiters.filter((item) => item !== waiter);
        waiter.reject(error);
        continue;
      }
      if (matched) {
        clearTimeout(waiter.timer);
        this.waiters = this.waiters.filter((item) => item !== waiter);
        waiter.resolve(trimmed);
      }
    }
  }

  rejectWaiters(error) {
    for (const waiter of this.waiters.splice(0)) {
      clearTimeout(waiter.timer);
      waiter.reject(error);
    }
  }

  waitFor(predicate, timeoutMs = 15000) {
    for (const line of this.lines) {
      if (predicate(line)) return Promise.resolve(line);
    }
    if (this.closed) {
      return Promise.reject(new Error(`${this.label} already exited (code=${this.closed.code}, signal=${this.closed.signal})`));
    }
    return new Promise((resolve, reject) => {
      const waiter = { predicate, resolve, reject, timer: null };
      waiter.timer = setTimeout(() => {
        this.waiters = this.waiters.filter((item) => item !== waiter);
        reject(new Error(`Timed out waiting for ${this.label} output; stderr=${this.stderr.join('').slice(-2000)}`));
      }, timeoutMs);
      this.waiters.push(waiter);
    });
  }

  waitForClose(timeoutMs = 15000) {
    if (this.closed) return Promise.resolve(this.closed);
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`Timed out waiting for ${this.label} to exit`)), timeoutMs);
      this.child.once('exit', (code, signal) => {
        clearTimeout(timer);
        resolve({ code, signal });
      });
    });
  }
}

function jsonLineWithType(type) {
  return (line) => {
    try {
      return JSON.parse(line)?.type === type;
    } catch {
      return false;
    }
  };
}

function spawnRuntime(runtimeBinary, dbPath, controllerUrl, label) {
  assertMockControllerUrl(controllerUrl);
  const child = spawn(runtimeBinary, [
    '--db', dbPath,
    '--controller', controllerUrl,
    '--connections-interval', '50',
    '--accounting-interval', '50ms',
    '--queue-capacity', '8',
  ], {
    env: testEnvironment(controllerUrl),
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });
  const capture = new ProcessCapture(child, label);
  trackedProcesses.add(capture);
  trackedRuntimePids.add(child.pid);
  return capture;
}

function testEnvironment(controllerUrl, dataDir = undefined, autoExitMs = undefined) {
  const environment = { ...process.env };
  delete environment.PROXYLENS_DB_PATH;
  environment.MIHOMO_SECRET = syntheticControllerSecret;
  environment.PROXYLENS_CONTROLLER_URL = controllerUrl;
  if (dataDir) environment.PROXYLENS_DATA_DIR = dataDir;
  else delete environment.PROXYLENS_DATA_DIR;
  if (autoExitMs !== undefined) {
    environment.PROXYLENS_E2E_MODE = '1';
    environment.PROXYLENS_E2E_AUTO_EXIT_MS = String(autoExitMs);
  } else {
    delete environment.PROXYLENS_E2E_MODE;
    delete environment.PROXYLENS_E2E_AUTO_EXIT_MS;
  }
  return environment;
}

function spawnTauri(tauriBinaryPath, controllerUrl, dataDir, label) {
  assertMockControllerUrl(controllerUrl);
  const child = spawn(tauriBinaryPath, [], {
    env: testEnvironment(controllerUrl, dataDir, 8000),
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });
  const capture = new ProcessCapture(child, label);
  trackedProcesses.add(capture);
  return capture;
}

async function runTauriScenario(tauriBinaryPath, controllerUrl, dataDir, expectedState, label) {
  const capture = spawnTauri(tauriBinaryPath, controllerUrl, dataDir, label);
  const bootstrapLine = await capture.waitFor((line) => line.startsWith('PROXYLENS_SUPERVISOR_BOOTSTRAP '));
  const status = JSON.parse(bootstrapLine.slice('PROXYLENS_SUPERVISOR_BOOTSTRAP '.length));
  if (status.state !== expectedState) {
    throw new Error(`${label} Supervisor state was ${status.state}, expected ${expectedState}`);
  }
  if (status.state === 'Started') {
    if (status.pid) trackedSupervisorPids.add(status.pid);
    if (status.runtimePid) trackedRuntimePids.add(status.runtimePid);
  }
  const queryLine = await capture.waitFor((line) => line.includes('Query sidecar ready at:'));
  const queryMatch = queryLine.match(/Query sidecar ready at:\s*http:\/\/127\.0\.0\.1:(\d+)/);
  if (!queryMatch) throw new Error(`${label} did not report a loopback Query API URL`);
  await capture.waitFor((line) => line.includes('PROXYLENS_WEBVIEW_E2E_READY meta=1 summary=1 connections=1'));
  const closed = await capture.waitForClose(20000);
  if (closed.code !== 0) {
    throw new Error(`${label} exited unexpectedly (code=${closed.code}, signal=${closed.signal})`);
  }
  return { status, queryPort: Number(queryMatch[1]), capture };
}

function isPidAlive(pid) {
  if (!pid || !Number.isInteger(pid) || pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error?.code === 'EPERM';
  }
}

async function assertPortClosed(port) {
  await new Promise((resolve, reject) => {
    const request = http.get({ hostname: '127.0.0.1', port, path: '/healthz', timeout: 700 }, (response) => {
      response.resume();
      response.on('end', () => reject(new Error(`Query API port ${port} still responded after UI close`)));
    });
    request.on('timeout', () => request.destroy());
    request.on('error', () => resolve());
  });
}

async function stopExactProcess(capture) {
  if (!capture || capture.closed) return;
  try {
    capture.child.kill();
  } catch {}
  try {
    await capture.waitForClose(5000);
  } catch {}
}

async function stopExactPid(pid) {
  if (!isPidAlive(pid)) return;
  try {
    process.kill(pid);
  } catch {}
  const deadline = Date.now() + 5000;
  while (Date.now() < deadline && isPidAlive(pid)) {
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  if (isPidAlive(pid)) throw new Error(`Test Runtime PID ${pid} did not stop after exact-PID cleanup`);
}

async function main() {
  requireFile(tauriBinary, 'Tauri desktop binary');
  const runtimeBinary = resolveRuntimeBinary();
  const controller = await startMockController(syntheticControllerSecret);
  assertMockControllerUrl(controller.url);

  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-3e2a-'));
  const dataDir = path.join(tempRoot, 'first-data');
  const otherDataDir = path.join(tempRoot, 'other-data');
  fs.mkdirSync(dataDir, { recursive: true });
  fs.mkdirSync(otherDataDir, { recursive: true });
  const firstDbPath = path.join(dataDir, 'proxylens.db');
  const otherDbPath = path.join(otherDataDir, 'proxylens.db');
  let firstRuntimePid = null;
  let otherRuntime = null;

  try {
    console.log(`[mock] Controller=${controller.url}`);
    console.log('[A] first Tauri launch starts Supervisor/Runtime and Query API');
    const first = await runTauriScenario(tauriBinary, controller.url, dataDir, 'Started', 'Tauri-A');
    firstRuntimePid = first.status.runtimePid;
    if (!isPidAlive(firstRuntimePid)) throw new Error(`Runtime PID ${firstRuntimePid} was not alive after Tauri-A exited`);
    if (!fs.statSync(firstDbPath, { throwIfNoEntry: false })?.isFile()) {
      throw new Error(`First-run Runtime did not create its authority DB: ${firstDbPath}`);
    }
    await assertPortClosed(first.queryPort);
    console.log(`    PASS: UI closed, Query API port ${first.queryPort} closed, Runtime PID ${firstRuntimePid} remains alive`);

    console.log('[B] duplicate Runtime candidate receives ALREADY_RUNNING');
    const duplicate = spawnRuntime(runtimeBinary, firstDbPath, controller.url, 'Runtime-duplicate');
    const duplicateLine = await duplicate.waitFor(jsonLineWithType('proxylens-runtime-already-running'), 10000);
    const duplicateSignal = JSON.parse(duplicateLine);
    if (duplicateSignal.runtimeVersion !== '0.7.0-phase3e2a') {
      throw new Error(`Unexpected duplicate Runtime version: ${duplicateSignal.runtimeVersion}`);
    }
    const duplicateExit = await duplicate.waitForClose(10000);
    if (duplicateExit.code !== 0 || duplicate.lines.some(jsonLineWithType('proxylens-runtime-ready'))) {
      throw new Error(`Duplicate Runtime did not exit cleanly without READY: ${JSON.stringify(duplicateExit)}`);
    }
    if (!isPidAlive(firstRuntimePid)) throw new Error('Duplicate candidate affected the existing Runtime PID');
    console.log('    PASS: duplicate candidate exited cleanly without a second READY');

    console.log('[C] reopening Tauri reuses the existing Supervisor/Runtime');
    const second = await runTauriScenario(tauriBinary, controller.url, dataDir, 'AlreadyRunning', 'Tauri-B');
    if (!isPidAlive(firstRuntimePid)) throw new Error('Reopening Tauri affected the existing Runtime PID');
    await assertPortClosed(second.queryPort);
    console.log(`    PASS: Tauri-B reported AlreadyRunning and Query API port ${second.queryPort} closed on UI close`);

    console.log('[D] a different authority DB can run concurrently');
    otherRuntime = spawnRuntime(runtimeBinary, otherDbPath, controller.url, 'Runtime-other-db');
    const otherReadyLine = await otherRuntime.waitFor(jsonLineWithType('proxylens-runtime-ready'), 10000);
    const otherReady = JSON.parse(otherReadyLine);
    if (!isPidAlive(firstRuntimePid) || !isPidAlive(otherReady.pid)) {
      throw new Error('Different-DB Runtime concurrency did not keep both exact PIDs alive');
    }
    console.log(`    PASS: Runtime PID ${otherReady.pid} owns a different temp DB alongside ${firstRuntimePid}`);
    await stopExactProcess(otherRuntime);
    otherRuntime = null;

    for (const request of controller.requests) {
      if (request.method !== 'GET' || !['/version', '/connections'].includes(request.path)) {
        throw new Error(`Mock Controller observed unexpected request: ${JSON.stringify(request)}`);
      }
      if (!request.authMatches) throw new Error(`Mock Controller observed a request without the inherited synthetic secret: ${request.path}`);
    }
    if (!controller.requests.some((request) => request.path === '/version') || !controller.requests.some((request) => request.path === '/connections')) {
      throw new Error('Mock Controller did not observe the expected read-only endpoints');
    }
    console.log(`[mock] ${controller.requests.length} Controller requests were GET-only and mock-scoped`);
    console.log('Phase 3E-2A lifecycle smoke PASS');
  } finally {
    await stopExactProcess(otherRuntime);
    for (const pid of trackedSupervisorPids) await stopExactPid(pid);
    for (const pid of trackedRuntimePids) await stopExactPid(pid);
    for (const processCapture of trackedProcesses) await stopExactProcess(processCapture);
    await controller.close();
    try {
      fs.rmSync(tempRoot, { recursive: true, force: true });
    } catch {}
  }
}

main().catch((error) => {
  console.error(`[FATAL] ${error.message}`);
  process.exitCode = 1;
});
