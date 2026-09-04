#!/usr/bin/env node

/**
 * Phase 3E-2B1 Supervisor, secure-config, crash-restart and Tauri lifecycle
 * acceptance smoke.
 *
 * Every Controller connection in this tool goes to the random loopback server
 * created below. All DB/config state is below one invocation-owned temp root.
 * Process cleanup is limited to PIDs recorded from children created here.
 */

import { spawn, spawnSync } from 'node:child_process';
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
const forbiddenControllerPorts = new Set([9000 + 90, 7900 + 88]);
const syntheticControllerSecret = 'phase3e2b1-secure-credential-secret';
const trackedProcesses = new Set();
const trackedSupervisors = new Set();
const trackedPids = new Set();

function requireFile(filePath, label) {
  if (!fs.statSync(filePath, { throwIfNoEntry: false })?.isFile()) {
    throw new Error(`${label} is missing: ${filePath}. Build the current release binary before running this smoke.`);
  }
}

function resolveBundledBinary(name) {
  const binariesDir = path.join(rootDir, 'ui', 'src-tauri', 'binaries');
  const candidates = fs.readdirSync(binariesDir, { withFileTypes: true })
    .filter((entry) => entry.isFile() && entry.name.startsWith(`${name}-`) && entry.name.endsWith('.exe'))
    .map((entry) => path.join(binariesDir, entry.name));
  if (candidates.length !== 1) {
    throw new Error(`Expected exactly one bundled ${name} binary in ${binariesDir}, found ${candidates.length}.`);
  }
  requireFile(candidates[0], `${name} binary`);
  return candidates[0];
}

function assertMockControllerUrl(controllerUrl) {
  if (!controllerUrl) throw new Error('Mock Controller URL is required before any process launch.');
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
  if (body.length < 126) return Buffer.concat([Buffer.from([0x81, body.length]), body]);
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
    uploadTotal: 17,
    downloadTotal: 29,
    connections: [{
      id: 'mock-phase3e2b1-connection',
      metadata: {
        network: 'tcp',
        destinationIP: '203.0.113.10',
        destinationPort: '443',
        host: 'mock.phase3e2b1.test',
        process: 'mock-runtime-test.exe',
        processPath: 'C:\\Mock\\mock-runtime-test.exe',
      },
      upload: 17,
      download: 29,
      start: '2026-09-04T00:00:00Z',
      chains: ['Mock-Node'],
      rule: 'MATCH',
      rulePayload: 'MATCH',
    }],
  }));
  const server = http.createServer((request, response) => {
    const requestPath = new URL(request.url || '/', 'http://127.0.0.1').pathname;
    requests.push({
      method: request.method,
      path: requestPath,
      authMatches: request.headers.authorization === `Bearer ${expectedSecret}`,
    });
    if (request.method !== 'GET') {
      response.writeHead(405);
      response.end();
      return;
    }
    if (requestPath === '/version') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ meta: true, version: 'mock-phase3e2b1' }));
      return;
    }
    response.writeHead(404);
    response.end();
  });
  server.on('upgrade', (request, socket) => {
    const requestPath = new URL(request.url || '/', 'http://127.0.0.1').pathname;
    requests.push({
      method: request.method,
      path: requestPath,
      authMatches: request.headers.authorization === `Bearer ${expectedSecret}`,
    });
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
    if (child.pid) trackedPids.add(child.pid);
    child.stdout?.setEncoding('utf8');
    child.stdout?.on('data', (chunk) => this.consume(chunk, false));
    child.stderr?.setEncoding('utf8');
    child.stderr?.on('data', (chunk) => {
      this.stderr.push(chunk);
      if (this.stderr.length > 50) this.stderr.shift();
      this.consume(chunk, true);
    });
    child.on('error', (error) => this.rejectWaiters(new Error(`${this.label} process error: ${error.message}`)));
    const finish = (code, signal) => {
      if (this.closed) return;
      if (this.buffer) this.emitLine(this.buffer);
      if (this.stderrBuffer) this.emitLine(this.stderrBuffer);
      this.closed = { code, signal };
      for (const waiter of this.waiters.splice(0)) {
        clearTimeout(waiter.timer);
        waiter.reject(new Error(
          `${this.label} exited before expected output (code=${code}, signal=${signal}); lines=${this.lines.join(' | ')}; stderr=${this.stderr.join('').slice(-3000)}`,
        ));
      }
    };
    child.on('exit', finish);
    child.on('close', finish);
  }

  consume(chunk, stderr) {
    const property = stderr ? 'stderrBuffer' : 'buffer';
    this[property] += chunk;
    const parts = this[property].split(/\r?\n/);
    this[property] = parts.pop() || '';
    for (const line of parts) this.emitLine(line);
  }

  emitLine(line) {
    const trimmed = line.trim();
    if (!trimmed) return;
    this.lines.push(trimmed);
    if (this.lines.length > 1500) this.lines.shift();
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
      return Promise.reject(new Error(
        `${this.label} already exited (code=${this.closed.code}, signal=${this.closed.signal}); lines=${this.lines.join(' | ')}; stderr=${this.stderr.join('').slice(-3000)}`,
      ));
    }
    return new Promise((resolve, reject) => {
      const waiter = { predicate, resolve, reject, timer: null };
      waiter.timer = setTimeout(() => {
        this.waiters = this.waiters.filter((item) => item !== waiter);
        reject(new Error(`Timed out waiting for ${this.label}; stderr=${this.stderr.join('').slice(-3000)}`));
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

function testEnvironment(controllerUrl, dataDir, configDir, credentialTarget, statusFile) {
  assertMockControllerUrl(controllerUrl);
  const environment = { ...process.env };
  delete environment.PROXYLENS_DB_PATH;
  delete environment.MIHOMO_SECRET;
  delete environment.PROXYLENS_RUNTIME_EXE;
  environment.PROXYLENS_CONTROLLER_URL = controllerUrl;
  environment.PROXYLENS_DATA_DIR = dataDir;
  environment.PROXYLENS_CONFIG_DIR = configDir;
  environment.PROXYLENS_E2E_MODE = '1';
  environment.PROXYLENS_E2E_CREDENTIAL_TARGET = credentialTarget;
  environment.PROXYLENS_E2E_STATUS_FILE = statusFile;
  return environment;
}

function runConfig(supervisorBinary, args, environment, input = undefined) {
  const result = spawnSync(supervisorBinary, ['config', ...args], {
    env: environment,
    input,
    encoding: 'utf8',
    windowsHide: true,
  });
  if (result.error) throw result.error;
  const stdout = result.stdout || '';
  const stderr = result.stderr || '';
  if (stdout.includes(syntheticControllerSecret) || stderr.includes(syntheticControllerSecret)) {
    throw new Error(`Config command ${args[0]} exposed the synthetic secret in output.`);
  }
  if (result.status !== 0) {
    throw new Error(`Config command ${args.join(' ')} failed with ${result.status}: ${stderr}`);
  }
  return { stdout, stderr };
}

function spawnSupervisor(supervisorBinary, dbPath, environment, label) {
  const child = spawn(supervisorBinary, ['--db', dbPath], {
    env: environment,
    stdio: ['pipe', 'pipe', 'pipe'],
    windowsHide: true,
  });
  const capture = new ProcessCapture(child, label);
  trackedProcesses.add(capture);
  trackedSupervisors.add(capture);
  return capture;
}

function spawnRuntime(runtimeBinary, dbPath, controllerUrl, environment, label) {
  assertMockControllerUrl(controllerUrl);
  const child = spawn(runtimeBinary, [
    '--db', dbPath,
    '--controller', controllerUrl,
    '--connections-interval', '50',
    '--accounting-interval', '50ms',
    '--queue-capacity', '8',
  ], {
    env: environment,
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });
  const capture = new ProcessCapture(child, label);
  trackedProcesses.add(capture);
  return capture;
}

function spawnTauri(tauriBinaryPath, environment, label) {
  const child = spawn(tauriBinaryPath, [], {
    env: { ...environment, PROXYLENS_E2E_AUTO_EXIT_MS: '8000' },
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });
  const capture = new ProcessCapture(child, label);
  trackedProcesses.add(capture);
  return capture;
}

async function runTauriScenario(tauriBinaryPath, environment, expectedState, label) {
  const capture = spawnTauri(tauriBinaryPath, environment, label);
  const bootstrapLine = await capture.waitFor((line) => line.startsWith('PROXYLENS_SUPERVISOR_BOOTSTRAP '));
  const status = JSON.parse(bootstrapLine.slice('PROXYLENS_SUPERVISOR_BOOTSTRAP '.length));
  if (status.state !== expectedState) {
    throw new Error(`${label} Supervisor state was ${status.state}, expected ${expectedState}`);
  }
  if (status.state === 'Started') {
    if (!status.pid || !status.runtimePid || status.runtimeState !== 'started') {
      throw new Error(`${label} Started status lacked Supervisor/Runtime identity: ${bootstrapLine}`);
    }
    trackedPids.add(status.pid);
    trackedPids.add(status.runtimePid);
  }
  const queryLine = await capture.waitFor((line) => line.includes('Query sidecar ready at:'));
  const queryMatch = queryLine.match(/Query sidecar ready at:\s*http:\/\/127\.0\.0\.1:(\d+)/);
  if (!queryMatch) throw new Error(`${label} did not report a loopback Query API URL`);
  await capture.waitFor((line) => line.includes('PROXYLENS_WEBVIEW_E2E_READY meta=1 summary=1 connections=1'));
  const closed = await capture.waitForClose(25000);
  if (closed.code !== 0) throw new Error(`${label} exited unexpectedly: ${JSON.stringify(closed)}`);
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

async function waitForPidState(pid, wantAlive, timeoutMs = 10000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (isPidAlive(pid) === wantAlive) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`PID ${pid} did not become ${wantAlive ? 'alive' : 'absent'} in time`);
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

async function stopSupervisorGracefully(capture) {
  if (!capture || capture.closed) return;
  if (capture.child.stdin) {
    try { capture.child.stdin.write('STOP\n'); } catch {}
  }
  await capture.waitForClose(10000);
}

async function stopExactPid(pid) {
  if (!isPidAlive(pid)) return;
  try {
    process.kill(pid);
  } catch {}
  await waitForPidState(pid, false, 10000);
}

async function waitForStatusLine(statusFile, predicate, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const text = fs.readFileSync(statusFile, { encoding: 'utf8', flag: 'a+' });
    for (const line of text.split(/\r?\n/)) {
      if (!line.trim()) continue;
      try {
        const value = JSON.parse(line);
        if (predicate(value)) return value;
      } catch {}
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`Timed out waiting for safe Supervisor status in ${statusFile}`);
}

function assertMockRequests(requests, minimumCount) {
  if (requests.length < minimumCount) {
    throw new Error(`Mock Controller observed ${requests.length} requests, expected at least ${minimumCount}.`);
  }
  for (const request of requests) {
    if (request.method !== 'GET' || !['/version', '/connections'].includes(request.path)) {
      throw new Error(`Mock Controller observed unexpected request: ${JSON.stringify(request)}`);
    }
    if (!request.authMatches) throw new Error(`Mock Controller observed a request without the synthetic credential: ${request.path}`);
  }
}

async function main() {
  requireFile(tauriBinary, 'Tauri desktop binary');
  const supervisorBinary = resolveBundledBinary('proxylens-supervisor');
  const runtimeBinary = resolveBundledBinary('proxylens-runtime');
  const controller = await startMockController(syntheticControllerSecret);
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-3e2b1-'));
  const dataDir = path.join(tempRoot, 'data');
  const otherDataDir = path.join(tempRoot, 'other-data');
  const observedDataDir = path.join(tempRoot, 'observed-data');
  const configDir = path.join(tempRoot, 'config');
  fs.mkdirSync(dataDir, { recursive: true });
  fs.mkdirSync(otherDataDir, { recursive: true });
  fs.mkdirSync(observedDataDir, { recursive: true });
  fs.mkdirSync(configDir, { recursive: true });
  const dbPath = path.join(dataDir, 'proxylens.db');
  const otherDbPath = path.join(otherDataDir, 'proxylens.db');
  const observedDbPath = path.join(observedDataDir, 'proxylens.db');
  const statusFile = path.join(tempRoot, 'supervisor-status.jsonl');
  const credentialTarget = `ProxyLens/Test/${crypto.randomUUID()}`;
  const environment = testEnvironment(controller.url, dataDir, configDir, credentialTarget, statusFile);
  let supervisorPid = null;
  let runtimePidA = null;
  let runtimePidB = null;
  let otherSupervisor = null;
  let otherRuntimePid = null;
  let observedSupervisor = null;
  let observedRuntime = null;
  let observedRuntimePid = null;
  let observedRuntimeRestartPid = null;
  let credentialCleared = false;

  try {
    console.log(`[mock] Controller=${controller.url}`);
    console.log('[A0] secure config writes non-secret Controller URL and random test Credential Manager target');
    runConfig(supervisorBinary, ['set-controller', '--controller', controller.url], environment);
    const configPath = path.join(configDir, 'runtime.json');
    const configText = fs.readFileSync(configPath, 'utf8');
    if (configText.includes(syntheticControllerSecret) || configText.includes('secret')) {
      throw new Error(`runtime.json contains sensitive material: ${configPath}`);
    }
    const beforeSecret = JSON.parse(runConfig(supervisorBinary, ['status'], environment).stdout);
    if (beforeSecret.secretPresent || beforeSecret.controllerUrl !== controller.url) {
      throw new Error(`unexpected pre-secret config status: ${JSON.stringify(beforeSecret)}`);
    }
    runConfig(supervisorBinary, ['set-secret'], environment, `${syntheticControllerSecret}\n`);
    const afterSecret = JSON.parse(runConfig(supervisorBinary, ['status'], environment).stdout);
    if (!afterSecret.secretPresent || afterSecret.controllerUrl !== controller.url) {
      throw new Error(`unexpected secure config status: ${JSON.stringify(afterSecret)}`);
    }
    console.log('    PASS: config URL persisted, secret stored out-of-band, status exposed presence only');

    console.log('[A] Tauri starts Supervisor, Supervisor starts Runtime, Query/frontend become ready');
    const first = await runTauriScenario(tauriBinary, environment, 'Started', 'Tauri-A');
    supervisorPid = first.status.pid;
    runtimePidA = first.status.runtimePid;
    await waitForPidState(supervisorPid, true);
    await waitForPidState(runtimePidA, true);
    if (!fs.statSync(dbPath, { throwIfNoEntry: false })?.isFile()) {
      throw new Error(`Runtime did not create authority DB: ${dbPath}`);
    }
    await assertPortClosed(first.queryPort);
    console.log(`    PASS: Query port ${first.queryPort} closed while Supervisor ${supervisorPid} and Runtime ${runtimePidA} survived UI close`);

    console.log('[B] exact Runtime crash while UI is closed is recovered by the same Supervisor');
    const requestCountBeforeCrash = controller.requests.length;
    await stopExactPid(runtimePidA);
    const restarted = await waitForStatusLine(
      statusFile,
      (value) => value.type === 'proxylens-supervisor-runtime-restarted',
    );
    runtimePidB = restarted.runtimePid;
    if (!runtimePidB || runtimePidB === runtimePidA || restarted.pid !== supervisorPid) {
      throw new Error(`Restart signal did not preserve Supervisor identity or create a new Runtime PID: ${JSON.stringify(restarted)}`);
    }
    await waitForPidState(supervisorPid, true);
    await waitForPidState(runtimePidB, true);
    const requestDeadline = Date.now() + 10000;
    while (controller.requests.length <= requestCountBeforeCrash && Date.now() < requestDeadline) {
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    if (controller.requests.length <= requestCountBeforeCrash) {
      throw new Error('Restarted Runtime did not resume mock Controller observations');
    }
    console.log(`    PASS: Runtime ${runtimePidA} crashed; Supervisor ${supervisorPid} restarted Runtime ${runtimePidB} with the same temp DB`);

    console.log('[C] reopening Tauri reuses the existing Supervisor and Runtime');
    const second = await runTauriScenario(tauriBinary, environment, 'AlreadyRunning', 'Tauri-B');
    if (!isPidAlive(supervisorPid) || !isPidAlive(runtimePidB)) {
      throw new Error('Reopening Tauri affected the existing Supervisor or Runtime');
    }
    await assertPortClosed(second.queryPort);
    console.log(`    PASS: Tauri-B reported Supervisor AlreadyRunning; Query port ${second.queryPort} closed again`);

    console.log('[D] duplicate Supervisor same DB exits without affecting the owner');
    const duplicate = spawnSupervisor(supervisorBinary, dbPath, environment, 'Supervisor-duplicate');
    const duplicateLine = await duplicate.waitFor(jsonLineWithType('proxylens-supervisor-already-running'), 10000);
    const duplicateExit = await duplicate.waitForClose(10000);
    if (duplicateExit.code !== 0 || duplicate.lines.some(jsonLineWithType('proxylens-supervisor-ready'))) {
      throw new Error(`Duplicate Supervisor did not exit cleanly: ${JSON.stringify(duplicateExit)}`);
    }
    if (!isPidAlive(supervisorPid) || !isPidAlive(runtimePidB)) {
      throw new Error(`Duplicate Supervisor affected owner: ${duplicateLine}`);
    }
    console.log('    PASS: duplicate Supervisor emitted ALREADY_RUNNING and preserved the original process pair');

    console.log('[E] different authority DB can run a second Supervisor/Runtime pair');
    otherSupervisor = spawnSupervisor(supervisorBinary, otherDbPath, environment, 'Supervisor-other-db');
    const otherReadyLine = await otherSupervisor.waitFor(jsonLineWithType('proxylens-supervisor-ready'), 10000);
    const otherReady = JSON.parse(otherReadyLine);
    otherRuntimePid = otherReady.runtimePid;
    trackedPids.add(otherRuntimePid);
    if (otherReady.runtimeState !== 'started' || !otherReady.runtimePid || !isPidAlive(otherSupervisor.child.pid) || !isPidAlive(otherReady.runtimePid)) {
      throw new Error(`Different-DB Supervisor did not start an independent Runtime: ${otherReadyLine}`);
    }
    console.log(`    PASS: Supervisor ${otherSupervisor.child.pid} and Runtime ${otherReady.runtimePid} owned an independent temp DB`);

    assertMockRequests(controller.requests, 4);
    console.log(`[mock] ${controller.requests.length} Controller observations were GET-only, authenticated, and random-port mock-scoped`);

    console.log('[E2] Supervisor observes an external Runtime and takes over after its exact exit');
    observedRuntime = spawnRuntime(runtimeBinary, observedDbPath, controller.url, environment, 'Runtime-external');
    const externalReadyLine = await observedRuntime.waitFor(jsonLineWithType('proxylens-runtime-ready'), 10000);
    const externalReady = JSON.parse(externalReadyLine);
    observedRuntimePid = externalReady.pid;
    trackedPids.add(observedRuntimePid);
    if (!observedRuntimePid || !isPidAlive(observedRuntimePid)) {
      throw new Error(`External Runtime did not become alive: ${externalReadyLine}`);
    }
    observedSupervisor = spawnSupervisor(supervisorBinary, observedDbPath, environment, 'Supervisor-observer');
    const observerReadyLine = await observedSupervisor.waitFor(jsonLineWithType('proxylens-supervisor-ready'), 10000);
    const observerReady = JSON.parse(observerReadyLine);
    trackedPids.add(observerReady.pid);
    if (observerReady.runtimeState !== 'already-running' || observerReady.runtimePid || observerReady.pid !== observedSupervisor.child.pid) {
      throw new Error(`Supervisor did not enter external Runtime observation mode: ${observerReadyLine}`);
    }
    await waitForPidState(observerReady.pid, true);
    await stopExactProcess(observedRuntime);
    observedRuntime = null;
    const takeover = await waitForStatusLine(
      statusFile,
      (value) => value.type === 'proxylens-supervisor-runtime-restarted' && value.pid === observerReady.pid,
    );
    observedRuntimeRestartPid = takeover.runtimePid;
    trackedPids.add(observedRuntimeRestartPid);
    if (!observedRuntimeRestartPid || observedRuntimeRestartPid === observedRuntimePid || !isPidAlive(observedRuntimeRestartPid)) {
      throw new Error(`Supervisor did not take over with a new Runtime PID: ${JSON.stringify(takeover)}`);
    }
    console.log(`    PASS: Supervisor ${observerReady.pid} observed external Runtime ${observedRuntimePid} and took over with Runtime ${observedRuntimeRestartPid}`);

    console.log('[F] exact STOP cleanup removes a Supervisor-owned Runtime and final Tauri-owned pair');
    await stopSupervisorGracefully(otherSupervisor);
    await waitForPidState(otherRuntimePid, false);
    otherSupervisor = null;
    otherRuntimePid = null;
    await stopSupervisorGracefully(observedSupervisor);
    await waitForPidState(observedRuntimeRestartPid, false);
    observedSupervisor = null;
    observedRuntimeRestartPid = null;
    // The Tauri-owned Supervisor stdin is intentionally private to the
    // desktop process. Tauri has already exited here, so clean up the exact
    // recorded Supervisor PID first, then its exact recorded Runtime child.
    await stopExactPid(supervisorPid);
    await stopExactPid(runtimePidB);
    runConfig(supervisorBinary, ['clear-secret'], environment);
    credentialCleared = true;
    const cleared = JSON.parse(runConfig(supervisorBinary, ['status'], environment).stdout);
    if (cleared.secretPresent) throw new Error('Credential Manager test target remained present after clear-secret');
    console.log('    PASS: exact Supervisor STOP stopped only its direct-owned Runtime; Tauri pair was then cleaned by exact recorded PIDs; test credential cleared');
    console.log('Phase 3E-2B1 Supervisor lifecycle smoke PASS');
  } finally {
    if (!credentialCleared) {
      try {
        runConfig(supervisorBinary, ['clear-secret'], environment);
      } catch (error) {
        console.error(`[cleanup] failed to clear the random test credential: ${error.message}`);
      }
    }
    if (supervisorPid && isPidAlive(supervisorPid)) {
      try { await stopExactPid(supervisorPid); } catch (error) { console.error(`[cleanup] ${error.message}`); }
    }
    for (const supervisor of trackedSupervisors) {
      if (!supervisor.closed) {
        try { await stopSupervisorGracefully(supervisor); } catch (error) { console.error(`[cleanup] ${error.message}`); }
      }
    }
    for (const pid of [...trackedPids]) {
      if (isPidAlive(pid)) {
        try { await stopExactPid(pid); } catch (error) { console.error(`[cleanup] ${error.message}`); }
      }
    }
    for (const capture of trackedProcesses) await stopExactProcess(capture);
    await controller.close();
    try { fs.rmSync(tempRoot, { recursive: true, force: true }); } catch (error) { console.error(`[cleanup] ${error.message}`); }
  }
}

main().catch((error) => {
  console.error(`[FATAL] ${error.message}`);
  process.exitCode = 1;
});
