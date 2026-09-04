#!/usr/bin/env node

/**
 * Isolated Phase 3E-2B2A installed lifecycle acceptance.
 *
 * This harness builds two temporary Tauri product identities, installs them
 * into a temporary directory, and uses only a random loopback mock Controller,
 * temporary DB/config roots, a random WinCred target, and one random
 * \ProxyLens-Test\<UUID> task. It never targets or enumerates production
 * ProxyLens tasks and never launches a Mihomo/FLClash process.
 */

import { execFileSync, spawn, spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const rootDir = path.resolve(path.dirname(__filename), '..', '..');
const uiDir = path.join(rootDir, 'ui');
const collectorDir = path.join(rootDir, 'collector');
const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-2b2a-installed-'));
const dataDir = path.join(tempRoot, 'data');
const configDir = path.join(tempRoot, 'config');
const installDir = path.join(tempRoot, 'installed');
const statusFile = path.join(tempRoot, 'supervisor-status.jsonl');
const controlBinary = path.join(tempRoot, 'proxylens-supervisor-control.exe');
const taskName = `\\ProxyLens-Test\\${crypto.randomUUID()}`;
const taskPath = '\\ProxyLens-Test\\';
const taskLeaf = taskName.slice(taskPath.length);
const credentialTarget = `ProxyLens/Test/${crypto.randomUUID()}`;
const syntheticSecret = `isolated-${crypto.randomUUID()}`;
const dbPath = path.join(dataDir, 'proxylens.db');
const trackedCaptures = new Set();
const trackedPids = new Set();
let controller;
let packageA;
let packageB;
let installedSupervisor;
let installedRuntime;
let installedDesktop;
let uninstaller;

const baseEnvironment = {
  ...process.env,
  PROXYLENS_E2E_MODE: '1',
  PROXYLENS_DATA_DIR: dataDir,
  PROXYLENS_CONFIG_DIR: configDir,
  PROXYLENS_CONTROLLER_URL: '',
  PROXYLENS_E2E_CREDENTIAL_TARGET: credentialTarget,
  PROXYLENS_E2E_TASK_NAME: taskName,
  PROXYLENS_E2E_STATUS_FILE: statusFile,
  PROXYLENS_E2E_DIRECT_OWNER: '1',
};
delete baseEnvironment.PROXYLENS_DB_PATH;
delete baseEnvironment.PROXYLENS_E2E_TASK_EXE;
delete baseEnvironment.MIHOMO_SECRET;

function requireFile(filePath, label) {
  if (!fs.statSync(filePath, { throwIfNoEntry: false })?.isFile()) {
    throw new Error(`${label} is missing`);
  }
}

function assertMockControllerUrl(controllerUrl) {
  const parsed = new URL(controllerUrl);
  if (parsed.protocol !== 'http:' || parsed.hostname !== '127.0.0.1' || !parsed.port || parsed.pathname !== '/' || parsed.search || parsed.hash || parsed.username || parsed.password) {
    throw new Error('installed lifecycle requires a random 127.0.0.1 mock Controller URL');
  }
  if (Number(parsed.port) === 9090 || Number(parsed.port) === 7988) {
    throw new Error('installed lifecycle refused a conventional Controller port');
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
    uploadTotal: 31,
    downloadTotal: 47,
    connections: [{
      id: 'mock-phase3e2b2a-connection',
      metadata: {
        network: 'tcp',
        destinationIP: '203.0.113.20',
        destinationPort: '443',
        host: 'mock.phase3e2b2a.test',
        process: 'mock-installed-fixture.exe',
        processPath: 'C:\\Mock\\mock-installed-fixture.exe',
      },
      upload: 31,
      download: 47,
      start: '2026-09-04T00:00:00Z',
      chains: ['Mock-Installed-Node'],
      rule: 'MATCH',
      rulePayload: 'MATCH',
    }],
  }));
  const server = http.createServer((request, response) => {
    const requestPath = new URL(request.url || '/', 'http://127.0.0.1').pathname;
    requests.push({ path: requestPath, authMatches: request.headers.authorization === `Bearer ${expectedSecret}` });
    if (request.method !== 'GET') {
      response.writeHead(405);
      response.end();
      return;
    }
    if (requestPath === '/version') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ meta: true, version: 'mock-phase3e2b2a' }));
      return;
    }
    response.writeHead(404);
    response.end();
  });
  server.on('upgrade', (request, socket) => {
    const requestPath = new URL(request.url || '/', 'http://127.0.0.1').pathname;
    requests.push({ path: requestPath, authMatches: request.headers.authorization === `Bearer ${expectedSecret}` });
    if (request.method !== 'GET' || requestPath !== '/connections' || typeof request.headers['sec-websocket-key'] !== 'string') {
      socket.destroy();
      return;
    }
    const accept = crypto.createHash('sha1')
      .update(`${request.headers['sec-websocket-key']}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`)
      .digest('base64');
    socket.write([
      'HTTP/1.1 101 Switching Protocols',
      'Upgrade: websocket',
      'Connection: Upgrade',
      `Sec-WebSocket-Accept: ${accept}`,
      '\r\n',
    ].join('\r\n'));
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
  if (!address || typeof address === 'string' || !address.port) throw new Error('mock Controller did not bind');
  const url = `http://127.0.0.1:${address.port}`;
  assertMockControllerUrl(url);
  return {
    url,
    requests,
    close: async () => {
      for (const socket of sockets) socket.destroy();
      await new Promise((resolve) => server.close(() => resolve()));
    },
  };
}

class ProcessCapture {
  constructor(child, label) {
    this.child = child;
    this.label = label;
    this.lines = [];
    this.stderr = [];
    this.waiters = [];
    this.closed = null;
    this.attach(child.stdout, false);
    this.attach(child.stderr, true);
    child.once('exit', (code, signal) => {
      this.closed = { code, signal };
      for (const waiter of this.waiters.splice(0)) {
        clearTimeout(waiter.timer);
        waiter.reject(new Error(`${this.label} exited before expected evidence`));
      }
    });
  }

  attach(stream, isStderr) {
    if (!stream) return;
    let pending = '';
    stream.setEncoding('utf8');
    stream.on('data', (chunk) => {
      pending += chunk;
      const parts = pending.split(/\r?\n/);
      pending = parts.pop() || '';
      for (const line of parts) {
        if (isStderr) this.stderr.push(line);
        this.lines.push(line);
        this.flush(line);
      }
    });
    stream.on('end', () => {
      if (pending) {
        if (isStderr) this.stderr.push(pending);
        this.lines.push(pending);
        this.flush(pending);
      }
    });
  }

  flush(line) {
    for (const waiter of [...this.waiters]) {
      let matched = false;
      try { matched = waiter.predicate(line); } catch (error) {
        clearTimeout(waiter.timer);
        this.waiters = this.waiters.filter((item) => item !== waiter);
        waiter.reject(error);
        continue;
      }
      if (matched) {
        clearTimeout(waiter.timer);
        this.waiters = this.waiters.filter((item) => item !== waiter);
        waiter.resolve(line);
      }
    }
  }

  waitFor(predicate, timeoutMs = 20000) {
    for (const line of this.lines) if (predicate(line)) return Promise.resolve(line);
    if (this.closed) return Promise.reject(new Error(`${this.label} already exited`));
    return new Promise((resolve, reject) => {
      const waiter = { predicate, resolve, reject, timer: null };
      waiter.timer = setTimeout(() => {
        this.waiters = this.waiters.filter((item) => item !== waiter);
        reject(new Error(`Timed out waiting for ${this.label}; stderr=${this.stderr.slice(-20).join('\n')}`));
      }, timeoutMs);
      this.waiters.push(waiter);
    });
  }

  waitForClose(timeoutMs = 20000) {
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

function testEnvironment(controllerUrl, autoExitMs = undefined) {
  const environment = { ...baseEnvironment, PROXYLENS_CONTROLLER_URL: controllerUrl };
  if (autoExitMs === undefined) delete environment.PROXYLENS_E2E_AUTO_EXIT_MS;
  else environment.PROXYLENS_E2E_AUTO_EXIT_MS = String(autoExitMs);
  return environment;
}

function runBinary(binary, args, environment = baseEnvironment, options = {}) {
  return execFileSync(binary, args, {
    cwd: rootDir,
    env: environment,
    encoding: 'utf8',
    stdio: ['pipe', 'pipe', 'pipe'],
    ...options,
  });
}

function parseJSONOutput(output, label) {
  try { return JSON.parse(output.trim()); } catch { throw new Error(`${label} returned invalid JSON`); }
}

function currentConfig() {
  const configPath = path.join(configDir, 'runtime.json');
  return { path: configPath, bytes: fs.readFileSync(configPath) };
}

function invokeStatus(binary, args = ['control', 'status']) {
  return parseJSONOutput(runBinary(binary, [...args, '--db', dbPath]), 'Supervisor lifecycle status');
}

function readReadyRecords() {
  if (!fs.existsSync(statusFile)) return [];
  return fs.readFileSync(statusFile, 'utf8').split(/\r?\n/).filter(Boolean).map((line) => {
    try { return JSON.parse(line); } catch { return null; }
  }).filter((record) => record?.type === 'proxylens-supervisor-ready');
}

function isPidAlive(pid) {
  if (!Number.isInteger(pid) || pid <= 0) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return error?.code === 'EPERM';
  }
}

async function waitFor(predicate, timeoutMs, label) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await predicate()) return;
    await new Promise((resolve) => setTimeout(resolve, 300));
  }
  throw new Error(`Timed out waiting for ${label}`);
}

async function waitForOwner(binary, expectedTaskRegistered, expectedSupervisorRunning = true) {
  let last;
  await waitFor(async () => {
    last = invokeStatus(binary, ['install', 'status']);
    return last.taskRegistered === expectedTaskRegistered
      && last.supervisorRunning === expectedSupervisorRunning
      && (!expectedSupervisorRunning || last.runtimeRunning === true);
  }, 20000, 'installed owner status');
  return last;
}

function captureOwnerPids() {
  const records = readReadyRecords();
  const started = [...records].reverse().find((record) => record.runtimeState === 'started' && record.pid > 0 && record.runtimePid > 0);
  if (!started) throw new Error('Supervisor status file did not contain a started owner record');
  trackedPids.add(started.pid);
  trackedPids.add(started.runtimePid);
  return { supervisorPid: started.pid, runtimePid: started.runtimePid };
}

function findFiles(root, predicate) {
  const found = [];
  const visit = (directory) => {
    if (!fs.statSync(directory, { throwIfNoEntry: false })?.isDirectory()) return;
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const child = path.join(directory, entry.name);
      if (entry.isDirectory()) visit(child);
      else if (entry.isFile() && predicate(child, entry.name)) found.push(child);
    }
  };
  visit(root);
  return found;
}

function discoverInstalledLayout() {
  const binaries = new Map();
  for (const name of ['proxylens-desktop.exe', 'proxylens-runtime.exe', 'proxylens-supervisor.exe', 'proxylens-query-api.exe']) {
    const matches = findFiles(installDir, (_file, entryName) => entryName.toLowerCase() === name);
    if (matches.length !== 1) throw new Error(`Expected one installed ${name}, found ${matches.length}`);
    binaries.set(name, matches[0]);
  }
  const uninstallers = findFiles(installDir, (_file, entryName) => entryName.toLowerCase() === 'uninstall.exe');
  if (uninstallers.length !== 1) throw new Error(`Expected one installed uninstaller, found ${uninstallers.length}`);
  installedDesktop = binaries.get('proxylens-desktop.exe');
  installedRuntime = binaries.get('proxylens-runtime.exe');
  installedSupervisor = binaries.get('proxylens-supervisor.exe');
  uninstaller = uninstallers[0];
}

function buildIsolatedPackage(version, label) {
  if (process.env.PROXYLENS_REUSE_LIFECYCLE_PACKAGES === '1') {
    const outputDir = path.join(uiDir, 'src-tauri', 'target', 'release', 'bundle', 'nsis');
    const candidates = findFiles(outputDir, (_file, entryName) => entryName === `ProxyLens Lifecycle Test_${version}_x64-setup.exe`);
    if (candidates.length !== 1) throw new Error(`Expected one reusable isolated ${label} NSIS package, found ${candidates.length}`);
    const destination = path.join(tempRoot, `package-${label}.exe`);
    fs.copyFileSync(candidates[0], destination);
    return destination;
  }
  const sourceConfig = JSON.parse(fs.readFileSync(path.join(uiDir, 'src-tauri', 'tauri.conf.json'), 'utf8'));
  sourceConfig.productName = 'ProxyLens Lifecycle Test';
  sourceConfig.version = version;
  sourceConfig.identifier = 'com.proxylens.desktop.lifecycle-test';
  sourceConfig.bundle.targets = ['nsis'];
  sourceConfig.bundle.windows = {
    ...(sourceConfig.bundle.windows || {}),
    nsis: {
      ...(sourceConfig.bundle.windows?.nsis || {}),
      installMode: 'currentUser',
      installerHooks: 'windows/hooks.nsh',
    },
  };
  const overlay = path.join(uiDir, 'src-tauri', `.phase3e2b2a-${label}.json`);
  fs.writeFileSync(overlay, `${JSON.stringify(sourceConfig, null, 2)}\n`, { mode: 0o600 });
  const outputDir = path.join(uiDir, 'src-tauri', 'target', 'release', 'bundle', 'nsis');
  const before = new Map(findFiles(outputDir, (_file, entryName) => entryName.toLowerCase().endsWith('.exe')).map((file) => [file, fs.statSync(file).mtimeMs]));
  try {
    execFileSync('npm.cmd', ['run', 'tauri', '--', 'build', '--config', `src-tauri/${path.basename(overlay)}`, '--bundles', 'nsis'], {
      cwd: uiDir,
      env: { ...process.env, PROXYLENS_E2E_MODE: '', PROXYLENS_E2E_TASK_NAME: '', PROXYLENS_E2E_DIRECT_OWNER: '' },
      shell: true,
      stdio: 'inherit',
    });
  } finally {
    fs.rmSync(overlay, { force: true });
  }
  const candidates = findFiles(outputDir, (_file, entryName) => entryName.toLowerCase().endsWith('.exe'))
    .filter((file) => !before.has(file) || fs.statSync(file).mtimeMs > before.get(file));
  if (candidates.length !== 1) throw new Error(`Expected one newly built isolated ${label} NSIS package, found ${candidates.length}`);
  const destination = path.join(tempRoot, `package-${label}.exe`);
  fs.copyFileSync(candidates[0], destination);
  return destination;
}

function installPackage(packagePath, label) {
  const result = spawnSync(packagePath, ['/S', `/D=${installDir}`], {
    cwd: rootDir,
    env: testEnvironment(controller.url),
    encoding: 'utf8',
    windowsHide: true,
  });
  if (result.status !== 0) {
    throw new Error(`isolated NSIS ${label} install failed with exit code ${result.status}`);
  }
  discoverInstalledLayout();
}

function launchDesktop(label, expectedState) {
  const child = spawn(installedDesktop, [], {
    cwd: path.dirname(installedDesktop),
    env: testEnvironment(controller.url, 5000),
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });
  const capture = new ProcessCapture(child, label);
  trackedCaptures.add(capture);
  return (async () => {
    const bootstrapLine = await capture.waitFor((line) => line.startsWith('PROXYLENS_SUPERVISOR_BOOTSTRAP '));
    const bootstrap = parseJSONOutput(bootstrapLine.slice('PROXYLENS_SUPERVISOR_BOOTSTRAP '.length), `${label} bootstrap`);
    if (bootstrap.state !== expectedState) throw new Error(`${label} bootstrap state was ${bootstrap.state}, expected ${expectedState}`);
    if (bootstrap.pid) trackedPids.add(bootstrap.pid);
    if (bootstrap.runtimePid) trackedPids.add(bootstrap.runtimePid);
    const queryLine = await capture.waitFor((line) => line.includes('Query sidecar ready at:'));
    if (!/http:\/\/127\.0\.0\.1:\d+/.test(queryLine)) throw new Error(`${label} did not expose a loopback Query API`);
    await capture.waitFor((line) => line.includes('PROXYLENS_WEBVIEW_E2E_READY meta=1 summary=1 connections=1'));
    const closed = await capture.waitForClose(20000);
    if (closed.code !== 0) throw new Error(`${label} exited with code ${closed.code}`);
    return bootstrap;
  })();
}

async function stopExactProcess(capture) {
  if (!capture || capture.closed) return;
  try { capture.child.kill(); } catch {}
  try { await capture.waitForClose(5000); } catch {}
}

async function stopExactPid(pid) {
  if (!isPidAlive(pid)) return;
  try { process.kill(pid); } catch {}
  await waitFor(() => !isPidAlive(pid), 10000, `exact test PID ${pid} cleanup`);
}

async function waitForPidsGone(pids, label) {
  await waitFor(() => pids.every((pid) => !isPidAlive(pid)), 15000, `${label} exact process shutdown`);
}

async function main() {
  fs.mkdirSync(dataDir, { recursive: true });
  fs.mkdirSync(configDir, { recursive: true });
  controller = await startMockController(syntheticSecret);
  baseEnvironment.PROXYLENS_CONTROLLER_URL = controller.url;
  execFileSync('go', ['build', '-o', controlBinary, './cmd/proxylens-supervisor'], { cwd: collectorDir, env: process.env, stdio: 'inherit' });
  runBinary(controlBinary, ['config', 'set-controller', '--controller', controller.url]);
  runBinary(controlBinary, ['config', 'set-secret'], baseEnvironment, { input: `${syntheticSecret}\n` });
  const configured = parseJSONOutput(runBinary(controlBinary, ['config', 'status']), 'isolated config status');
  if (configured.schemaVersion !== 2 || !configured.autostartEnabled || !configured.controllerConfigured || !configured.secretPresent) {
    throw new Error('isolated runtime config did not persist v2 enabled preference and secure credential presence');
  }
  const configBeforeInstall = currentConfig().bytes;

  packageA = buildIsolatedPackage('0.7.0', 'a');
  packageB = buildIsolatedPackage('0.7.1', 'b');
  installPackage(packageA, 'A');
  requireFile(installedSupervisor, 'installed Supervisor');
  requireFile(installedRuntime, 'installed Runtime');
  requireFile(installedDesktop, 'installed desktop binary');
  let owner = await waitForOwner(controlBinary, true, true);
  if (path.normalize(owner.actionPath) !== path.normalize(installedSupervisor)) throw new Error('fresh install task action was not the actual installed Supervisor');
  const firstOwner = captureOwnerPids();
  await launchDesktop('fresh-installed-ui', 'Installed');
  if (!isPidAlive(firstOwner.supervisorPid) || !isPidAlive(firstOwner.runtimePid)) throw new Error('Supervisor/Runtime did not survive fresh UI close');
  if (!fs.statSync(dbPath, { throwIfNoEntry: false })?.isFile()) throw new Error('fresh installed owner did not create the isolated authority DB');
  const dbAfterFresh = fs.statSync(dbPath).size;

  const configBeforeUpgrade = currentConfig().bytes;
  const oldOwnerPids = [firstOwner.supervisorPid, firstOwner.runtimePid];
  installPackage(packageB, 'B upgrade');
  await waitForPidsGone(oldOwnerPids, 'Package A');
  owner = await waitForOwner(controlBinary, true, true);
  if (path.normalize(owner.actionPath) !== path.normalize(installedSupervisor)) throw new Error('upgrade task action was not reconciled to the actual Package B Supervisor');
  if (!Buffer.from(configBeforeUpgrade).equals(currentConfig().bytes)) throw new Error('upgrade changed the persisted v2 config unexpectedly');
  if (fs.statSync(dbPath).size < dbAfterFresh) throw new Error('upgrade did not preserve the isolated authority DB');
  const secondOwner = captureOwnerPids();
  await launchDesktop('upgraded-installed-ui', 'Installed');
  if (!isPidAlive(secondOwner.supervisorPid) || !isPidAlive(secondOwner.runtimePid)) {
    throw new Error(`Supervisor/Runtime did not survive upgraded UI close pids=${JSON.stringify(secondOwner)} alive=${isPidAlive(secondOwner.supervisorPid)}/${isPidAlive(secondOwner.runtimePid)} status=${JSON.stringify(invokeStatus(controlBinary))}`);
  }

  runBinary(installedSupervisor, ['config', 'set-autostart', 'false']);
  const disabledConfig = parseJSONOutput(runBinary(installedSupervisor, ['config', 'status']), 'disabled config status');
  if (disabledConfig.autostartEnabled !== false || !disabledConfig.secretPresent) throw new Error('autostart false did not persist without losing credential state');
  if (!invokeStatus(controlBinary).supervisorRunning) throw new Error('disabling autostart unexpectedly stopped the current owner');
  const disabledBytes = currentConfig().bytes;
  const oldDisabledPids = [secondOwner.supervisorPid, secondOwner.runtimePid];
  installPackage(packageB, 'disabled-preference upgrade');
  await waitForPidsGone(oldDisabledPids, 'disabled Package B');
  owner = await waitForOwner(controlBinary, false, false);
  if (!Buffer.from(disabledBytes).equals(currentConfig().bytes)) throw new Error('disabled autostart preference changed during upgrade');
  const fallbackBootstrap = await launchDesktop('disabled-autostart-ui', 'Started');
  if (!fallbackBootstrap.pid || !fallbackBootstrap.runtimePid) throw new Error('disabled-autostart UI did not use direct current-session Supervisor fallback');
  const fallbackPids = { supervisorPid: fallbackBootstrap.pid, runtimePid: fallbackBootstrap.runtimePid };
  const disabledTaskAfterUI = parseJSONOutput(runBinary(controlBinary, ['install', 'status']), 'disabled task status after UI');
  if (disabledTaskAfterUI.taskRegistered !== false) throw new Error('disabled-autostart UI silently re-enabled the scheduled task');
  if (parseJSONOutput(runBinary(installedSupervisor, ['config', 'status']), 'post-fallback config status').autostartEnabled !== false) throw new Error('disabled-autostart UI changed the persisted preference');

  const uninstallPids = [fallbackPids.supervisorPid, fallbackPids.runtimePid];
  spawnSync(uninstaller, ['/S'], { cwd: path.dirname(uninstaller), env: testEnvironment(controller.url), encoding: 'utf8', windowsHide: true });
  await waitForPidsGone(uninstallPids, 'uninstall');
  if (findFiles(installDir, (_file, name) => ['proxylens-desktop.exe', 'proxylens-runtime.exe', 'proxylens-supervisor.exe', 'proxylens-query-api.exe'].includes(name.toLowerCase())).length !== 0) {
    throw new Error('uninstall left isolated program binaries behind');
  }
  const afterUninstallTask = invokeStatus(controlBinary, ['install', 'status']);
  if (afterUninstallTask.taskRegistered || afterUninstallTask.supervisorRunning || afterUninstallTask.runtimeRunning) throw new Error('uninstall left isolated owner state behind');
  if (!fs.statSync(dbPath, { throwIfNoEntry: false })?.isFile() || !fs.statSync(currentConfig().path, { throwIfNoEntry: false })?.isFile()) throw new Error('uninstall did not preserve isolated DB/config data');
  const preservedCredential = parseJSONOutput(runBinary(controlBinary, ['config', 'status']), 'preserved credential status');
  if (!preservedCredential.secretPresent) throw new Error('uninstall removed the isolated credential before explicit cleanup');
  runBinary(controlBinary, ['config', 'clear-secret']);
  const clearedCredential = parseJSONOutput(runBinary(controlBinary, ['config', 'status']), 'cleared credential status');
  if (clearedCredential.secretPresent) throw new Error('explicit isolated credential cleanup did not clear the credential');
  if (!controller.requests.some((request) => request.authMatches)) throw new Error('mock Controller did not observe an authenticated Runtime request');
  console.log(`PASS phase3e2b2a installed-lifecycle task=${taskName} product=com.proxylens.desktop.lifecycle-test`);
}

try {
  await main();
} finally {
  for (const capture of trackedCaptures) await stopExactProcess(capture);
  try { runBinary(controlBinary, ['install', 'unregister']); } catch {}
  try { runBinary(controlBinary, ['control', 'stop', '--wait', '15s']); } catch {}
  for (const pid of trackedPids) {
    try { await stopExactPid(pid); } catch {}
  }
  if (controller) await controller.close();
  fs.rmSync(tempRoot, { recursive: true, force: true });
}
