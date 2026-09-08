#!/usr/bin/env node

/**
 * Phase 3E-2B2B isolated Settings / installed product acceptance.
 *
 * This harness uses one temporary current-user NSIS identity, one random
 * loopback mock Controller, one random WinCred target, one random
 * \ProxyLens-Test\<UUID> task, and one temporary authority DB. It never
 * enumerates or touches production tasks/credentials and never launches a
 * Mihomo or FLClash process.
 */

import { execFileSync, spawn, spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const uiDir = path.join(rootDir, 'ui');
const collectorDir = path.join(rootDir, 'collector');
const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-2b2b-settings-'));
const dataDir = path.join(tempRoot, 'data');
const configDir = path.join(tempRoot, 'config');
const installDir = path.join(tempRoot, 'installed');
const statusFile = path.join(tempRoot, 'supervisor-status.jsonl');
const taskActionWrapper = path.join(tempRoot, 'task-owner-wrapper.cmd');
const taskName = `\\ProxyLens-Test\\${crypto.randomUUID()}`;
const credentialTarget = `ProxyLens/Test/${crypto.randomUUID()}`;
const secretA = `settings-a-${crypto.randomUUID()}`;
const secretB = `settings-b-${crypto.randomUUID()}`;
const dbPath = path.join(dataDir, 'proxylens.db');
const trackedCaptures = new Set();
const trackedPids = new Set();
let controller;
let expectedSecret = '';
let expectedSecretGeneration = 0;
let packagePath;
let installedDesktop;
let installedSupervisor;
let installedSupervisorHost;
let uninstaller;

const baseEnvironment = {
  ...process.env,
  PROXYLENS_E2E_MODE: '1',
  PROXYLENS_SETTINGS_E2E: '1',
  PROXYLENS_DATA_DIR: dataDir,
  PROXYLENS_CONFIG_DIR: configDir,
  PROXYLENS_DB_PATH: dbPath,
  PROXYLENS_CONTROLLER_URL: '',
  PROXYLENS_E2E_CREDENTIAL_TARGET: credentialTarget,
  PROXYLENS_E2E_TASK_NAME: taskName,
  PROXYLENS_E2E_STATUS_FILE: statusFile,
  PROXYLENS_E2E_DIRECT_OWNER: '1',
};
delete baseEnvironment.PROXYLENS_E2E_TASK_EXE;
delete baseEnvironment.PROXYLENS_E2E_TASK_ARGS;
delete baseEnvironment.MIHOMO_SECRET;

function requireFile(filePath, label) {
  if (!fs.statSync(filePath, { throwIfNoEntry: false })?.isFile()) throw new Error(`${label} is missing`);
}

function assertMockControllerUrl(controllerUrl) {
  const parsed = new URL(controllerUrl);
  if (parsed.protocol !== 'http:' || parsed.hostname !== '127.0.0.1' || !parsed.port || parsed.pathname !== '/' || parsed.search || parsed.hash || parsed.username || parsed.password) {
    throw new Error('Settings acceptance requires a random 127.0.0.1 mock Controller URL');
  }
  if (Number(parsed.port) === 9090 || Number(parsed.port) === 7988) throw new Error('Settings acceptance refused a conventional Controller port');
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
  const requests = [];
  const sockets = new Set();
  const frame = encodeWebSocketText(JSON.stringify({
    uploadTotal: 41,
    downloadTotal: 59,
    connections: [{
      id: 'mock-phase3e2b2b-settings',
      metadata: {
        network: 'tcp',
        destinationIP: '203.0.113.42',
        destinationPort: '443',
        host: 'mock.phase3e2b2b.test',
        process: 'mock-settings-fixture.exe',
        processPath: 'C:\\Mock\\mock-settings-fixture.exe',
      },
      upload: 41,
      download: 59,
      start: '2026-09-05T00:00:00Z',
      chains: ['Mock-Settings-Node'],
      rule: 'MATCH',
      rulePayload: 'MATCH',
    }],
  }));
  const server = http.createServer((request, response) => {
    const requestPath = new URL(request.url || '/', 'http://127.0.0.1').pathname;
    const secretMatches = request.headers.authorization === `Bearer ${expectedSecret}`;
    requests.push({ path: requestPath, secret: secretMatches, generation: expectedSecretGeneration });
    if (request.method !== 'GET') {
      response.writeHead(405);
      response.end();
      return;
    }
    if (expectedSecret && !secretMatches) {
      response.writeHead(401);
      response.end();
      return;
    }
    if (requestPath === '/version') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ meta: true, version: 'mock-phase3e2b2b-settings' }));
      return;
    }
    response.writeHead(404);
    response.end();
  });
  server.on('upgrade', (request, socket) => {
    const requestPath = new URL(request.url || '/', 'http://127.0.0.1').pathname;
    const secretMatches = request.headers.authorization === `Bearer ${expectedSecret}`;
    requests.push({ path: requestPath, secret: secretMatches, generation: expectedSecretGeneration });
    if (request.method !== 'GET' || requestPath !== '/connections' || typeof request.headers['sec-websocket-key'] !== 'string') {
      socket.destroy();
      return;
    }
    if (expectedSecret && !secretMatches) {
      socket.destroy();
      return;
    }
    const accept = crypto.createHash('sha1')
      .update(`${request.headers['sec-websocket-key']}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`)
      .digest('base64');
    socket.write(['HTTP/1.1 101 Switching Protocols', 'Upgrade: websocket', 'Connection: Upgrade', `Sec-WebSocket-Accept: ${accept}`, '\r\n'].join('\r\n'));
    sockets.add(socket);
    const timer = setInterval(() => { if (!socket.destroyed) socket.write(frame); }, 100);
    const remove = () => { clearInterval(timer); sockets.delete(socket); };
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
        waiter.reject(new Error(`${this.label} exited before expected evidence; stderr=${this.stderr.slice(-20).join('\\n')}`));
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
  }

  flush(line) {
    for (const waiter of [...this.waiters]) {
      if (!waiter.predicate(line)) continue;
      clearTimeout(waiter.timer);
      this.waiters = this.waiters.filter((item) => item !== waiter);
      waiter.resolve(line);
    }
  }

  waitFor(predicate, timeoutMs = 20000) {
    for (const line of this.lines) if (predicate(line)) return Promise.resolve(line);
    if (this.closed) return Promise.reject(new Error(`${this.label} already exited`));
    return new Promise((resolve, reject) => {
      const waiter = { predicate, resolve, reject, timer: null };
      waiter.timer = setTimeout(() => {
        this.waiters = this.waiters.filter((item) => item !== waiter);
        reject(new Error(`Timed out waiting for ${this.label}; stderr=${this.stderr.slice(-12).join('\n')}`));
      }, timeoutMs);
      this.waiters.push(waiter);
    });
  }

  waitForClose(timeoutMs = 20000) {
    if (this.closed) return Promise.resolve(this.closed);
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`Timed out waiting for ${this.label} to exit`)), timeoutMs);
      this.child.once('exit', (code, signal) => { clearTimeout(timer); resolve({ code, signal }); });
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
  return execFileSync(binary, args, { cwd: rootDir, env: environment, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'], ...options });
}

function parseJSONOutput(output, label) {
  try { return JSON.parse(output.trim()); } catch { throw new Error(`${label} returned invalid JSON`); }
}

function isPidAlive(pid) {
  if (!Number.isInteger(pid) || pid <= 0) return false;
  try { process.kill(pid, 0); return true; } catch (error) { return error?.code === 'EPERM'; }
}

async function waitFor(predicate, timeoutMs, label) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await predicate()) return;
    await new Promise((resolve) => setTimeout(resolve, 300));
  }
  throw new Error(`Timed out waiting for ${label}`);
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
  const exact = (name) => {
    const matches = findFiles(installDir, (_file, entryName) => entryName.toLowerCase() === name);
    if (matches.length !== 1) throw new Error(`Expected one installed ${name}, found ${matches.length}`);
    return matches[0];
  };
  installedDesktop = exact('proxylens-desktop.exe');
  installedSupervisor = exact('proxylens-supervisor.exe');
  installedSupervisorHost = exact('proxylens-supervisor-host.exe');
  const uninstallers = findFiles(installDir, (_file, entryName) => entryName.toLowerCase() === 'uninstall.exe');
  if (uninstallers.length !== 1) throw new Error(`Expected one installed uninstaller, found ${uninstallers.length}`);
  uninstaller = uninstallers[0];
}

function configureE2ETaskAction(supervisorPath) {
  const commandShell = process.env.ComSpec || path.join(process.env.WINDIR || 'C:\\Windows', 'System32', 'cmd.exe');
  requireFile(commandShell, 'Windows command shell');
  const setLine = (name, value) => `set "${name}=${String(value).replace(/"/g, '')}"`;
  const wrapper = [
    '@echo off',
    'setlocal EnableExtensions',
    setLine('PROXYLENS_E2E_MODE', '1'),
    setLine('PROXYLENS_SETTINGS_E2E', '1'),
    setLine('PROXYLENS_DATA_DIR', dataDir),
    setLine('PROXYLENS_CONFIG_DIR', configDir),
    setLine('PROXYLENS_DB_PATH', dbPath),
    setLine('PROXYLENS_CONTROLLER_URL', controller.url),
    setLine('PROXYLENS_E2E_CREDENTIAL_TARGET', credentialTarget),
    setLine('PROXYLENS_E2E_TASK_NAME', taskName),
    setLine('PROXYLENS_E2E_STATUS_FILE', statusFile),
    setLine('PROXYLENS_E2E_DIRECT_OWNER', '1'),
    `"${supervisorPath}" --db "${dbPath}"`,
    'exit /b %ERRORLEVEL%',
    '',
  ].join('\r\n');
  fs.writeFileSync(taskActionWrapper, wrapper, { encoding: 'utf8', mode: 0o600 });
  baseEnvironment.PROXYLENS_E2E_TASK_EXE = commandShell;
  baseEnvironment.PROXYLENS_E2E_TASK_ARGS = `/D /S /C ""${taskActionWrapper}""`;
}

function buildPackage() {
  const outputDir = path.join(uiDir, 'src-tauri', 'target', 'release', 'bundle', 'nsis');
  if (process.env.PROXYLENS_REUSE_SETTINGS_PACKAGE === '1') {
    const reusable = findFiles(outputDir, (_file, name) => name === 'ProxyLens Settings Test_0.7.0_x64-setup.exe');
    if (reusable.length !== 1) throw new Error(`Expected one reusable isolated Settings package, found ${reusable.length}`);
    return reusable[0];
  }
  const config = JSON.parse(fs.readFileSync(path.join(uiDir, 'src-tauri', 'tauri.conf.json'), 'utf8'));
  config.productName = 'ProxyLens Settings Test';
  config.version = '0.7.0';
  config.identifier = 'com.proxylens.desktop.settings-test';
  config.bundle.targets = ['nsis'];
  config.bundle.windows = { ...(config.bundle.windows || {}), nsis: { installMode: 'currentUser', installerHooks: 'windows/hooks.nsh' } };
  const overlay = path.join(uiDir, 'src-tauri', '.phase3e2b2b-settings.json');
  fs.writeFileSync(overlay, `${JSON.stringify(config, null, 2)}\n`, { mode: 0o600 });
  const before = new Map(findFiles(outputDir, (_file, name) => name.toLowerCase().endsWith('.exe')).map((file) => [file, fs.statSync(file).mtimeMs]));
  try {
    execFileSync('npm.cmd', ['run', 'tauri', '--', 'build', '--config', 'src-tauri/.phase3e2b2b-settings.json', '--bundles', 'nsis'], { cwd: uiDir, env: { ...process.env, PROXYLENS_E2E_MODE: '' }, shell: true, stdio: 'inherit' });
  } finally {
    fs.rmSync(overlay, { force: true });
  }
  const candidates = findFiles(outputDir, (_file, name) => name.toLowerCase().endsWith('.exe')).filter((file) => !before.has(file) || fs.statSync(file).mtimeMs > before.get(file));
  if (candidates.length !== 1) throw new Error(`Expected one new isolated Settings package, found ${candidates.length}`);
  return candidates[0];
}

function installPackage() {
  const result = spawnSync(packagePath, ['/S', `/D=${installDir}`], { cwd: rootDir, env: testEnvironment(controller.url), encoding: 'utf8', windowsHide: true });
  if (result.status !== 0) throw new Error(`isolated Settings package install failed with exit code ${result.status}`);
  discoverInstalledLayout();
}

function installStatus() {
  return parseJSONOutput(runBinary(installedSupervisor, ['install', 'status', '--db', dbPath]), 'installed owner status');
}

function controlStatus() {
  return parseJSONOutput(runBinary(installedSupervisor, ['control', 'status', '--db', dbPath]), 'control status');
}

function configApply(controllerUrl, autostartEnabled, secretAction, secret) {
  return parseJSONOutput(runBinary(installedSupervisor, ['config', 'apply'], testEnvironment(controller.url), {
    input: JSON.stringify({ controllerUrl, autostartEnabled, secretAction, secret }),
  }), 'settings apply');
}

function setExpectedSecret(secret) {
  expectedSecretGeneration += 1;
  expectedSecret = secret;
  return expectedSecretGeneration;
}

function requireSecretGenerationEvidence(label, baseline, generation) {
  const freshRequests = controller.requests.slice(baseline);
  const version = freshRequests.some((request) => request.generation === generation && request.secret && request.path === '/version');
  const connections = freshRequests.some((request) => request.generation === generation && request.secret && request.path === '/connections');
  if (!version || !connections) {
    throw new Error(`${label} generation ${generation} did not produce fresh authenticated version/connections evidence version=${version} connections=${connections}`);
  }
  return { generation, version, connections };
}

function startedRecords() {
  return fs.existsSync(statusFile)
    ? fs.readFileSync(statusFile, 'utf8')
      .split(/\r?\n/)
      .filter(Boolean)
      .map((line) => { try { return JSON.parse(line); } catch { return null; } })
      .filter((value) => value?.type === 'proxylens-supervisor-ready' && value.runtimeState === 'started')
    : [];
}

async function stopAndEnsureOwner() {
  const previousRecordCount = startedRecords().length;
  runBinary(installedSupervisor, ['control', 'stop', '--db', dbPath, '--wait', '15s']);
  await waitFor(() => {
    try {
      const status = controlStatus();
      return !status.supervisorRunning && !status.runtimeRunning;
    } catch {
      return false;
    }
  }, 20000, 'previous owner shutdown');
  runBinary(installedSupervisor, ['install', 'ensure-owner', '--db', dbPath]);
  let owner = null;
  await waitFor(() => {
    owner = latestStartedRecord(previousRecordCount);
    return owner !== null && isPidAlive(owner.supervisorPid) && isPidAlive(owner.runtimePid);
  }, 20000, 'fresh isolated Supervisor owner');
  return owner;
}

function latestStartedRecord(afterCount = 0) {
  const record = startedRecords().slice(afterCount).at(-1);
  if (!record?.pid || !record.runtimePid) return null;
  trackedPids.add(record.pid);
  trackedPids.add(record.runtimePid);
  return { supervisorPid: record.pid, runtimePid: record.runtimePid };
}

async function launchDesktop(label) {
  const child = spawn(installedDesktop, [], { cwd: path.dirname(installedDesktop), env: testEnvironment(controller.url, 5000), stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true });
  const capture = new ProcessCapture(child, label);
  trackedCaptures.add(capture);
  await capture.waitFor((line) => line.includes('Query sidecar ready at:'));
  await capture.waitFor((line) => line.includes('PROXYLENS_WEBVIEW_E2E_READY meta=1 summary=1 connections=1'));
  await capture.waitFor((line) => line.includes('PROXYLENS_SETTINGS_E2E_READY settings=1 owner=1 runtime=1'));
  const closed = await capture.waitForClose(20000);
  if (closed.code !== 0) throw new Error(`${label} exited with code ${closed.code}`);
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

async function main() {
  fs.mkdirSync(dataDir, { recursive: true });
  fs.mkdirSync(configDir, { recursive: true });
  controller = await startMockController();
  baseEnvironment.PROXYLENS_CONTROLLER_URL = controller.url;
  const expectedInstalledSupervisor = path.join(installDir, 'proxylens-supervisor.exe');
  configureE2ETaskAction(expectedInstalledSupervisor);
  packagePath = buildPackage();
  installPackage();
  requireFile(installedSupervisor, 'installed Supervisor');
  requireFile(installedSupervisorHost, 'installed Supervisor background host');
  if (path.resolve(installedSupervisor) !== path.resolve(expectedInstalledSupervisor)) {
    throw new Error('installed Supervisor path did not match the isolated Task Scheduler wrapper');
  }
  requireFile(installedDesktop, 'installed desktop binary');
  await waitFor(() => {
    const status = installStatus();
    return status.installedLayout === true && status.taskRegistered;
  }, 20000, 'initial installed owner');
  let owner;

  const initialApply = configApply(controller.url, true, 'replace', secretA);
  if (!initialApply.saved || !initialApply.restartRequired || !initialApply.secretChanged) throw new Error('initial secure Secret apply did not report a required restart');
  const secretABaseline = controller.requests.length;
  const secretAGeneration = setExpectedSecret(secretA);
  owner = await stopAndEnsureOwner();
  const dbAfterSecretA = fs.statSync(dbPath).size;
  await launchDesktop('settings-product-secret-a');
  requireSecretGenerationEvidence('Secret A', secretABaseline, secretAGeneration);
  if (!isPidAlive(owner.supervisorPid) || !isPidAlive(owner.runtimePid)) {
    throw new Error('UI close stopped the Secret A owner supervisor=' + owner.supervisorPid + ':' + isPidAlive(owner.supervisorPid) + ' runtime=' + owner.runtimePid + ':' + isPidAlive(owner.runtimePid));
  }
  if (fs.statSync(dbPath).size < dbAfterSecretA) throw new Error('Secret A restart did not preserve the authority DB');

  const replaceApply = configApply(controller.url, true, 'replace', secretB);
  if (!replaceApply.saved || !replaceApply.restartRequired) throw new Error('Secret B replacement did not require restart');
  const secretBBaseline = controller.requests.length;
  const secretBGeneration = setExpectedSecret(secretB);
  owner = await stopAndEnsureOwner();
  await launchDesktop('settings-product-secret-b');
  if (!isPidAlive(owner.supervisorPid) || !isPidAlive(owner.runtimePid)) throw new Error('UI close stopped the Secret B owner');
  const secretBEvidence = requireSecretGenerationEvidence('Secret B', secretBBaseline, secretBGeneration);

  const beforeAutostart = { ...owner };
  const disabled = configApply(controller.url, false, 'keep', '');
  if (!disabled.saved || disabled.restartRequired) throw new Error('autostart-only disable unexpectedly required Runtime restart');
  await waitFor(() => installStatus().taskRegistered === false, 10000, 'autostart false task removal');
  if (!isPidAlive(beforeAutostart.supervisorPid) || !isPidAlive(beforeAutostart.runtimePid)) throw new Error('autostart false interrupted current collection');
  const enabled = configApply(controller.url, true, 'keep', '');
  if (!enabled.saved || enabled.restartRequired) throw new Error('autostart-only enable unexpectedly required Runtime restart');
  await waitFor(() => installStatus().taskRegistered === true && installStatus().taskEnabled === true, 10000, 'autostart true task restore');
  if (fs.statSync(dbPath).size < dbAfterSecretA) throw new Error('autostart reconciliation did not preserve the authority DB');

  console.log(`PASS phase3e2b2b settings-product task=${taskName} installedLayout=${installStatus().installedLayout} secretA->secretB=verified generations=${secretAGeneration}->${secretBEvidence.generation} version=${secretBEvidence.version} connections=${secretBEvidence.connections} autostart=false->true=verified ui-close=survived`);
}

try {
  await main();
} finally {
  for (const capture of trackedCaptures) await stopExactProcess(capture);
  if (installedSupervisor) {
    try { runBinary(installedSupervisor, ['control', 'stop', '--db', dbPath, '--wait', '15s']); } catch {}
    try { runBinary(installedSupervisor, ['install', 'unregister', '--db', dbPath]); } catch {}
    try { runBinary(installedSupervisor, ['config', 'clear-secret']); } catch {}
  }
  for (const pid of trackedPids) {
    try { await stopExactPid(pid); } catch {}
  }
  if (uninstaller && fs.existsSync(uninstaller)) {
    try { spawnSync(uninstaller, ['/S'], { cwd: path.dirname(uninstaller), env: testEnvironment(controller?.url || ''), encoding: 'utf8', windowsHide: true }); } catch {}
  }
  if (controller) await controller.close();
  fs.rmSync(tempRoot, { recursive: true, force: true, maxRetries: 8, retryDelay: 500 });
}
