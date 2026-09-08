#!/usr/bin/env node

/**
 * Isolated regression for the installed GUI-subsystem Supervisor host.
 *
 * The Task Scheduler half uses a mutex-owning, no-network Runtime fixture so
 * the task cannot read production config/credentials. The direct-host half
 * uses the real Runtime with a random loopback mock Controller and random E2E
 * config/WinCred identity. It never targets production tasks or databases.
 */

import { execFileSync, spawn, spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const collectorDir = path.join(rootDir, 'collector');
const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-background-console-'));
const taskBuildDir = path.join(tempRoot, 'task-binaries');
const actualBuildDir = path.join(tempRoot, 'actual-binaries');
const taskDataDir = path.join(tempRoot, 'task-data');
const actualDataDir = path.join(tempRoot, 'actual-data');
const actualConfigDir = path.join(tempRoot, 'actual-config');
const taskDB = path.join(taskDataDir, 'authority.db');
const actualDB = path.join(actualDataDir, 'authority.db');
const cliDB = path.join(actualDataDir, 'cli-authority.db');
const taskName = `\\ProxyLens-Test\\${crypto.randomUUID()}`;
const taskPath = '\\ProxyLens-Test\\';
const taskLeaf = taskName.slice(taskPath.length);
const credentialTarget = `ProxyLens/Test/${crypto.randomUUID()}`;
const fixtureSource = path.join(rootDir, 'tools', 'runtime', 'fixtures', 'runtime-owner-fixture.go');
const trackedPids = new Set();
let controller;
let registrationAttempted = false;

function psQuote(value) {
  return `'${String(value).replaceAll("'", "''")}'`;
}

function runPowerShell(script) {
  const result = spawnSync('pwsh.exe', ['-NoProfile', '-NonInteractive', '-Command', script], {
    cwd: rootDir,
    env: process.env,
    encoding: 'utf8',
    windowsHide: true,
  });
  if (result.status !== 0) {
    throw new Error(`PowerShell evidence query failed: ${result.stderr || result.stdout}`);
  }
  return result.stdout.trim();
}

function parseJson(output, label) {
  try {
    return JSON.parse(output);
  } catch {
    throw new Error(`${label} returned invalid JSON`);
  }
}

function jsonArray(output) {
  if (!output) return [];
  const parsed = JSON.parse(output);
  return Array.isArray(parsed) ? parsed : [parsed];
}

function assertMockControllerUrl(controllerUrl) {
  const parsed = new URL(controllerUrl);
  if (parsed.protocol !== 'http:' || parsed.hostname !== '127.0.0.1' || !parsed.port || parsed.pathname !== '/' || parsed.search || parsed.hash || parsed.username || parsed.password) {
    throw new Error('background-host regression requires a random loopback mock Controller URL');
  }
}

function buildBinaries(directory, runtimeSource) {
  fs.mkdirSync(directory, { recursive: true });
  const cli = path.join(directory, 'proxylens-supervisor.exe');
  const host = path.join(directory, 'proxylens-supervisor-host.exe');
  const runtime = path.join(directory, 'proxylens-runtime.exe');
  execFileSync('go', ['build', '-o', cli, './cmd/proxylens-supervisor'], { cwd: collectorDir, stdio: 'inherit' });
  execFileSync('go', ['build', '-ldflags=-H=windowsgui', '-o', host, './cmd/proxylens-supervisor-host'], { cwd: collectorDir, stdio: 'inherit' });
  execFileSync('go', ['build', '-o', runtime, runtimeSource], { cwd: collectorDir, stdio: 'inherit' });
  return { cli, host, runtime };
}

function readPeSubsystem(paths) {
  const entries = paths.map((value) => `$paths += ${psQuote(value)}`).join(';');
  const output = runPowerShell(`$paths=@(); ${entries}; $rows=@(); foreach($path in $paths) { $bytes=[IO.File]::ReadAllBytes($path); $pe=[BitConverter]::ToInt32($bytes,0x3c); $subsystem=[BitConverter]::ToUInt16($bytes,$pe+24+68); $rows += [pscustomobject]@{Path=$path;Subsystem=$subsystem} }; $rows | ConvertTo-Json -Compress`);
  return jsonArray(output);
}

function readTaskDefinition() {
  const output = runPowerShell(`$task=Get-ScheduledTask -TaskPath ${psQuote(taskPath)} -TaskName ${psQuote(taskLeaf)}; $xml=[xml](Export-ScheduledTask -TaskPath ${psQuote(taskPath)} -TaskName ${psQuote(taskLeaf)}); $triggers=@($task.Triggers | ForEach-Object { [pscustomobject]@{Type=$_.CimClass.CimClassName;Interval=[string]$_.Repetition.Interval;Duration=[string]$_.Repetition.Duration;StopAtDurationEnd=[string]$_.Repetition.StopAtDurationEnd} }); $restart=@($xml.Task.Settings.ChildNodes | Where-Object { $_.NodeType -eq [System.Xml.XmlNodeType]::Element -and $_.LocalName -eq 'RestartOnFailure' }); [pscustomobject]@{Action=$task.Actions[0].Execute;Arguments=$task.Actions[0].Arguments;UserId=$task.Principal.UserId;CurrentUser=([Security.Principal.WindowsIdentity]::GetCurrent()).Name;LogonType=$task.Principal.LogonType.ToString();RunLevel=$task.Principal.RunLevel.ToString();MultipleInstances=$task.Settings.MultipleInstances.ToString();Triggers=$triggers;HasRestartOnFailure=($restart.Count -gt 0)} | ConvertTo-Json -Compress`);
  return parseJson(output, 'Task Scheduler readback');
}

function readProcessRecords(paths) {
  const entries = paths.map((value) => `$paths += ${psQuote(value)}`).join(';');
  const output = runPowerShell(`$paths=@(); ${entries}; Get-CimInstance Win32_Process | Where-Object { $paths -contains $_.ExecutablePath } | Select-Object Name,ProcessId,ParentProcessId,ExecutablePath | ConvertTo-Json -Compress`);
  return jsonArray(output);
}

function readProcessTree() {
  return jsonArray(runPowerShell('Get-CimInstance Win32_Process | Select-Object Name,ProcessId,ParentProcessId,ExecutablePath | ConvertTo-Json -Compress'));
}

function readTerminalSnapshot() {
  const output = runPowerShell("Get-Process -Name WindowsTerminal -ErrorAction SilentlyContinue | Select-Object Id,MainWindowHandle,MainWindowTitle | ConvertTo-Json -Compress");
  return jsonArray(output);
}

function readWindowState(pid) {
  return parseJson(runPowerShell(`Get-Process -Id ${Number(pid)} -ErrorAction Stop | Select-Object Id,MainWindowHandle,MainWindowTitle | ConvertTo-Json -Compress`), `window state for ${pid}`);
}

function descendantsOf(rootPids) {
  const records = readProcessTree();
  const children = new Map();
  for (const record of records) {
    const parent = Number(record.ParentProcessId);
    if (!children.has(parent)) children.set(parent, []);
    children.get(parent).push(record);
  }
  const result = [];
  const queue = [...rootPids];
  const seen = new Set(queue);
  while (queue.length) {
    const parent = queue.shift();
    for (const child of children.get(parent) || []) {
      if (seen.has(Number(child.ProcessId))) continue;
      seen.add(Number(child.ProcessId));
      result.push(child);
      queue.push(Number(child.ProcessId));
    }
  }
  return result;
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
  let lastError;
  while (Date.now() < deadline) {
    try {
      if (await predicate()) return;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 300));
  }
  throw new Error(`Timed out waiting for ${label}${lastError ? `: ${lastError.message}` : ''}`);
}

async function stopExactPid(pid) {
  if (!isPidAlive(pid)) return;
  try { process.kill(pid); } catch {}
  await waitFor(() => !isPidAlive(pid), 10000, `exact PID ${pid} shutdown`);
}

async function stopExactProcess(child) {
  if (!child || child.exitCode !== null) return;
  try { child.kill(); } catch {}
  await new Promise((resolve) => child.once('exit', resolve));
}

function assertNoVisibleConsole(rootPids, terminalBaseline, label) {
  for (const pid of rootPids) {
    const window = readWindowState(pid);
    if (Number(window.MainWindowHandle?.value ?? window.MainWindowHandle) !== 0) {
      throw new Error(`${label} PID ${pid} exposed a main window`);
    }
  }
  const descendants = descendantsOf(rootPids);
  const visibleConsoleChildren = descendants.filter((record) => ['conhost.exe', 'windowsterminal.exe'].includes(String(record.Name).toLowerCase()));
  if (visibleConsoleChildren.length) {
    throw new Error(`${label} created console host descendants: ${JSON.stringify({ roots: readProcessTree().filter((record) => rootPids.includes(Number(record.ProcessId))), visible: visibleConsoleChildren.map((record) => ({ name: record.Name, pid: record.ProcessId, parent: record.ParentProcessId, path: record.ExecutablePath })), descendants: descendants.map((record) => ({ name: record.Name, pid: record.ProcessId, parent: record.ParentProcessId, path: record.ExecutablePath })) })}`);
  }
  const baseline = new Set(terminalBaseline.map((item) => Number(item.Id)));
  const baselineTitles = new Map(terminalBaseline.map((item) => [Number(item.Id), String(item.MainWindowTitle || '')]));
  const currentTerminals = readTerminalSnapshot();
  const newTerminals = currentTerminals.filter((item) => !baseline.has(Number(item.Id)));
  if (newTerminals.length) {
    throw new Error(`${label} created new Windows Terminal processes: ${JSON.stringify(newTerminals)}`);
  }
  const changedTerminals = currentTerminals.filter((item) => baselineTitles.has(Number(item.Id)) && String(item.MainWindowTitle || '') !== baselineTitles.get(Number(item.Id)));
  if (changedTerminals.length) {
    throw new Error(`${label} changed an existing Windows Terminal tab/window title: ${JSON.stringify(changedTerminals)}`);
  }
}

function assertOwnerShape(records, hostPath, runtimePath, label) {
  const host = records.filter((record) => String(record.ExecutablePath).toLowerCase() === hostPath.toLowerCase());
  const runtime = records.filter((record) => String(record.ExecutablePath).toLowerCase() === runtimePath.toLowerCase());
  if (host.length !== 1 || runtime.length !== 1) {
    throw new Error(`${label} expected exactly one Supervisor and Runtime: ${JSON.stringify({ host, runtime })}`);
  }
  const hostPID = Number(host[0].ProcessId);
  const runtimePID = Number(runtime[0].ProcessId);
  trackedPids.add(hostPID);
  trackedPids.add(runtimePID);
  return { hostPID, runtimePID };
}

function assertNonElevatedToken() {
  const output = runPowerShell('$identity=[Security.Principal.WindowsIdentity]::GetCurrent(); $principal=[Security.Principal.WindowsPrincipal]::new($identity); [pscustomobject]@{User=$identity.Name;IsElevated=$principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)} | ConvertTo-Json -Compress');
  const token = parseJson(output, 'non-elevated token');
  if (token.IsElevated !== false) throw new Error('non-elevated feasibility gate failed: current token is elevated');
  return token;
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
  const frame = encodeWebSocketText(JSON.stringify({ uploadTotal: 0, downloadTotal: 0, connections: [] }));
  const server = http.createServer((request, response) => {
    const requestPath = new URL(request.url || '/', 'http://127.0.0.1').pathname;
    if (request.method === 'GET' && requestPath === '/version') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ meta: true, version: 'background-host-fixture' }));
      return;
    }
    response.writeHead(404);
    response.end();
  });
  server.on('upgrade', (request, socket) => {
    const requestPath = new URL(request.url || '/', 'http://127.0.0.1').pathname;
    if (request.method !== 'GET' || requestPath !== '/connections' || typeof request.headers['sec-websocket-key'] !== 'string') {
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
    close: async () => {
      for (const socket of sockets) socket.destroy();
      await new Promise((resolve) => server.close(resolve));
    },
  };
}

function cliEnvironment(extra = {}) {
  return { ...process.env, ...extra };
}

function runCli(binary, args, env) {
  return execFileSync(binary, args, {
    cwd: rootDir,
    env,
    encoding: 'utf8',
    stdio: ['pipe', 'pipe', 'pipe'],
  });
}

async function waitForOwner(paths, timeoutMs, label) {
  let owner;
  await waitFor(() => {
    owner = assertOwnerShape(readProcessRecords([paths.host, paths.runtime]), paths.host, paths.runtime, label);
    return true;
  }, timeoutMs, label);
  return owner;
}

async function waitForRuntimeReplacement(paths, oldRuntimePID, timeoutMs, label) {
  let owner;
  await waitFor(() => {
    const records = readProcessRecords([paths.host, paths.runtime]);
    const hosts = records.filter((record) => String(record.ExecutablePath).toLowerCase() === paths.host.toLowerCase());
    const runtimes = records.filter((record) => String(record.ExecutablePath).toLowerCase() === paths.runtime.toLowerCase());
    if (hosts.length !== 1 || runtimes.length !== 1 || Number(runtimes[0].ProcessId) === oldRuntimePID) return false;
    owner = assertOwnerShape(records, paths.host, paths.runtime, label);
    return true;
  }, timeoutMs, label);
  return owner;
}

async function waitForHostReplacement(paths, oldHostPID, timeoutMs, label) {
  let owner;
  await waitFor(() => {
    const records = readProcessRecords([paths.host, paths.runtime]);
    const hosts = records.filter((record) => String(record.ExecutablePath).toLowerCase() === paths.host.toLowerCase());
    const runtimes = records.filter((record) => String(record.ExecutablePath).toLowerCase() === paths.runtime.toLowerCase());
    if (hosts.length !== 1 || runtimes.length !== 1 || Number(hosts[0].ProcessId) === oldHostPID) return false;
    owner = assertOwnerShape(records, paths.host, paths.runtime, label);
    return true;
  }, timeoutMs, label);
  return owner;
}

async function waitForChildExit(child, timeoutMs) {
  if (child.exitCode !== null) return { code: child.exitCode, signal: child.signalCode };
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('timed out waiting for CLI Supervisor exit')), timeoutMs);
    child.once('exit', (code, signal) => {
      clearTimeout(timer);
      resolve({ code, signal });
    });
  });
}

async function assertCliHandshake(paths, env) {
  const child = spawn(paths.cli, ['--db', cliDB, '--runtime-exe', paths.runtime, '--controller', controller.url], {
    cwd: rootDir,
    env,
    stdio: ['pipe', 'pipe', 'pipe'],
    windowsHide: true,
  });
  let pending = '';
  let ready = null;
  const stderr = [];
  child.stdout.setEncoding('utf8');
  child.stdout.on('data', (chunk) => {
    pending += chunk;
    const lines = pending.split(/\r?\n/);
    pending = lines.pop() || '';
    for (const line of lines) {
      try {
        const value = JSON.parse(line);
        if (value.type === 'proxylens-supervisor-ready' && value.runtimeState === 'started') ready = value;
      } catch {}
    }
  });
  child.stderr.setEncoding('utf8');
  child.stderr.on('data', (chunk) => stderr.push(chunk));
  await waitFor(() => ready !== null, 15000, 'CLI Supervisor READY handshake');
  if (!ready.pid || !ready.runtimePid) throw new Error('CLI READY handshake omitted exact Supervisor/Runtime identity');
  child.stdin.write('STOP\n');
  const exit = await waitForChildExit(child, 15000);
  if (exit.code !== 0) throw new Error(`CLI Supervisor did not stop cleanly: ${JSON.stringify({ exit, stderr: stderr.join('').slice(-500) })}`);
  return ready;
}

async function main() {
  const token = assertNonElevatedToken();
  controller = await startMockController();
  fs.mkdirSync(taskDataDir, { recursive: true });
  fs.mkdirSync(actualDataDir, { recursive: true });
  fs.mkdirSync(actualConfigDir, { recursive: true });

  const taskBinaries = buildBinaries(taskBuildDir, '../tools/runtime/fixtures/runtime-owner-fixture.go');
  const actualBinaries = buildBinaries(actualBuildDir, './cmd/proxylens-runtime');
  const subsystems = readPeSubsystem([taskBinaries.cli, taskBinaries.host, taskBinaries.runtime]);
  const subsystemByName = new Map(subsystems.map((item) => [path.basename(item.Path), Number(item.Subsystem)]));
  if (subsystemByName.get(path.basename(taskBinaries.cli)) !== 3 || subsystemByName.get(path.basename(taskBinaries.runtime)) !== 3 || subsystemByName.get(path.basename(taskBinaries.host)) !== 2) {
    throw new Error(`unexpected PE subsystem contract: ${JSON.stringify(subsystems)}`);
  }

  const terminalBaseline = readTerminalSnapshot();
  const taskEnvironment = cliEnvironment({
    PROXYLENS_E2E_MODE: '1',
    PROXYLENS_E2E_TASK_NAME: taskName,
    PROXYLENS_E2E_TASK_EXE: taskBinaries.host,
    PROXYLENS_E2E_TASK_ARGS: `--db "${taskDB}"`,
  });
  runCli(taskBinaries.cli, ['install', 'register'], taskEnvironment);
  registrationAttempted = true;

  const definition = readTaskDefinition();
  const triggerTypes = (Array.isArray(definition.Triggers) ? definition.Triggers : [definition.Triggers]).map((trigger) => trigger.Type);
  if (path.normalize(definition.Action) !== path.normalize(taskBinaries.host)) throw new Error('Task Scheduler action did not use the GUI-subsystem background host');
  if (definition.LogonType !== 'Interactive' || definition.RunLevel !== 'Limited' || definition.MultipleInstances !== 'IgnoreNew') throw new Error(`Task Scheduler security/instance contract mismatch: ${JSON.stringify(definition)}`);
  if (definition.HasRestartOnFailure || !triggerTypes.includes('MSFT_TaskLogonTrigger') || !triggerTypes.includes('MSFT_TaskTimeTrigger')) throw new Error(`Task Scheduler trigger contract mismatch: ${JSON.stringify(definition)}`);
  for (const trigger of definition.Triggers) {
    if (trigger.Interval !== 'PT1M' || trigger.Duration !== '' || String(trigger.StopAtDurationEnd).toLowerCase() !== 'false') throw new Error(`Task Scheduler repetition contract mismatch: ${JSON.stringify(trigger)}`);
  }

  const firstOwner = await waitForOwner(taskBinaries, 20000, 'first Task Scheduler GUI-host launch');
  assertNoVisibleConsole([firstOwner.hostPID, firstOwner.runtimePID], terminalBaseline, 'first Task Scheduler launch');

  await stopExactPid(firstOwner.runtimePID);
  const afterRuntimeCrash = await waitForRuntimeReplacement(taskBinaries, firstOwner.runtimePID, 20000, 'Supervisor Runtime crash recovery');
  assertNoVisibleConsole([afterRuntimeCrash.hostPID, afterRuntimeCrash.runtimePID], terminalBaseline, 'Runtime crash recovery');

  await stopExactPid(afterRuntimeCrash.hostPID);
  const afterSupervisorCrash = await waitForHostReplacement(taskBinaries, afterRuntimeCrash.hostPID, 90000, 'Task Scheduler PT1M Supervisor recovery');
  assertNoVisibleConsole([afterSupervisorCrash.hostPID, afterSupervisorCrash.runtimePID], terminalBaseline, 'Supervisor periodic recovery');
  const status = parseJson(runCli(taskBinaries.cli, ['control', 'status', '--db', taskDB], taskEnvironment).trim(), 'task control status');
  if (!status.supervisorRunning || !status.runtimeRunning) throw new Error(`Task Scheduler recovery status was not healthy: ${JSON.stringify(status)}`);
  runCli(taskBinaries.cli, ['install', 'unregister'], taskEnvironment);
  await stopExactPid(afterSupervisorCrash.hostPID);
  await stopExactPid(afterSupervisorCrash.runtimePID);

  const actualEnvironment = cliEnvironment({
    PROXYLENS_E2E_MODE: '1',
    PROXYLENS_E2E_CONFIG_DIR: actualConfigDir,
    PROXYLENS_DATA_DIR: actualDataDir,
    PROXYLENS_CONTROLLER_URL: controller.url,
    PROXYLENS_E2E_CREDENTIAL_TARGET: credentialTarget,
  });
  const directHost = spawn(actualBinaries.host, ['--db', actualDB, '--controller', controller.url], {
    cwd: actualBuildDir,
    env: actualEnvironment,
    stdio: ['ignore', 'ignore', 'ignore'],
    windowsHide: true,
  });
  trackedPids.add(directHost.pid);
  const directOwner = await waitForOwner(actualBinaries, 20000, 'direct GUI-host real Runtime launch');
  assertNoVisibleConsole([directOwner.hostPID, directOwner.runtimePID], terminalBaseline, 'direct GUI-host real Runtime launch');
  await stopExactPid(directOwner.runtimePID);
  const directAfterRuntimeCrash = await waitForRuntimeReplacement(actualBinaries, directOwner.runtimePID, 20000, 'real Runtime crash recovery');
  assertNoVisibleConsole([directAfterRuntimeCrash.hostPID, directAfterRuntimeCrash.runtimePID], terminalBaseline, 'real Runtime crash recovery');
  runCli(actualBinaries.cli, ['control', 'stop', '--db', actualDB, '--wait', '15s'], actualEnvironment);
  await waitFor(() => !isPidAlive(directAfterRuntimeCrash.hostPID) && !isPidAlive(directAfterRuntimeCrash.runtimePID), 15000, 'direct GUI-host graceful stop');
  await stopExactProcess(directHost);

  const handshake = await assertCliHandshake(actualBinaries, actualEnvironment);
  const versionOutput = runCli(actualBinaries.cli, ['--version'], actualEnvironment);
  if (!versionOutput.includes('ProxyLens Supervisor')) throw new Error('console Supervisor CLI version output was not preserved');

  console.log(`PASS installed-background-console token=${token.User} IsElevated=false taskAction=GUI subsystem=2 cli= CUI/3 runtime=CUI/3 taskPidA=${firstOwner.hostPID} taskRuntimeA=${firstOwner.runtimePID} runtimeCrashReplacement=${afterRuntimeCrash.runtimePID} supervisorCrashReplacement=${afterSupervisorCrash.hostPID} realRuntimeCrashReplacement=${directAfterRuntimeCrash.runtimePID} cliReadyPid=${handshake.pid} noNewTerminal=true exactCleanup=pending`);
}

async function cleanup() {
  const taskEnvironment = cliEnvironment({
    PROXYLENS_E2E_MODE: '1',
    PROXYLENS_E2E_TASK_NAME: taskName,
    PROXYLENS_E2E_TASK_EXE: path.join(taskBuildDir, 'proxylens-supervisor-host.exe'),
    PROXYLENS_E2E_TASK_ARGS: `--db "${taskDB}"`,
  });
  try { runCli(path.join(taskBuildDir, 'proxylens-supervisor.exe'), ['control', 'stop', '--db', taskDB, '--wait', '15s'], taskEnvironment); } catch {}
  try { runCli(path.join(taskBuildDir, 'proxylens-supervisor.exe'), ['install', 'unregister'], taskEnvironment); } catch {}
  try {
    runPowerShell(`Unregister-ScheduledTask -TaskPath ${psQuote(taskPath)} -TaskName ${psQuote(taskLeaf)} -Confirm:$false -ErrorAction SilentlyContinue`);
  } catch {}
  const exactPaths = [
    path.join(taskBuildDir, 'proxylens-supervisor-host.exe'),
    path.join(taskBuildDir, 'proxylens-runtime.exe'),
    path.join(actualBuildDir, 'proxylens-supervisor-host.exe'),
    path.join(actualBuildDir, 'proxylens-runtime.exe'),
  ];
  for (const record of readProcessRecords(exactPaths)) {
    try { await stopExactPid(Number(record.ProcessId)); } catch {}
  }
  for (const pid of trackedPids) {
    try { await stopExactPid(pid); } catch {}
  }
  if (controller) await controller.close();
  fs.rmSync(tempRoot, { recursive: true, force: true, maxRetries: 8, retryDelay: 500 });
}

let succeeded = false;
try {
  await main();
  succeeded = true;
} finally {
  await cleanup();
  if (succeeded) console.log(`PASS exact cleanup task=${taskName}`);
}
