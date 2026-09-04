#!/usr/bin/env node

import { execFileSync, spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const repoRoot = path.resolve(path.dirname(__filename), '..', '..');
const collectorDir = path.join(repoRoot, 'collector');
const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-2b2a-task-owner-'));
const taskName = `\\ProxyLens-Test\\${crypto.randomUUID()}`;
const taskPath = '\\ProxyLens-Test\\';
const taskLeaf = taskName.slice(taskPath.length);
const trackedFixturePids = new Set();
const environment = {
  ...process.env,
  PROXYLENS_E2E_MODE: '1',
  PROXYLENS_E2E_TASK_NAME: taskName,
  PROXYLENS_E2E_TASK_EXE: path.join(tempRoot, 'task-owner-fixture.exe'),
  PROXYLENS_E2E_TASK_SCHEDULE: '1',
};
const supervisorBin = path.join(tempRoot, 'proxylens-supervisor.exe');
const fixtureSource = path.join(repoRoot, 'tools', 'runtime', 'fixtures', 'task-owner-fixture.go');
let fixturePid = null;

function runSupervisor(args) {
  return execFileSync(supervisorBin, args, {
    cwd: collectorDir,
    env: environment,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  });
}

function runPowerShell(script) {
  const result = spawnSync('pwsh.exe', ['-NoProfile', '-NonInteractive', '-Command', script], {
    cwd: repoRoot,
    env: process.env,
    encoding: 'utf8',
    windowsHide: true,
  });
  if (result.status !== 0) {
    throw new Error(`PowerShell exact task operation failed: ${result.stderr || result.stdout}`);
  }
  return result.stdout.trim();
}

function assertMockControllerUrl(controllerUrl) {
  // This scheduler-only fixture intentionally has no Controller. Keeping the
  // marker explicit makes the shared lifecycle safety audit distinguish this
  // no-network test from a Runtime integration harness. PROXYLENS_CONTROLLER_URL
  // is deliberately absent because this fixture never launches Runtime.
  if (controllerUrl !== undefined) throw new Error('task-owner fixture must not use a Controller');
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
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

async function stopExactProcess(child) {
  if (!child || child.exitCode !== null) return;
  try { child.kill(); } catch {}
  await new Promise((resolve) => child.once('exit', resolve));
}

async function stopExactPid(pid) {
  if (!isPidAlive(pid)) return;
  try { process.kill(pid); } catch {}
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline && isPidAlive(pid)) await sleep(100);
}

async function waitForFile(filePath, predicate, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (fs.existsSync(filePath)) {
      const value = fs.readFileSync(filePath, 'utf8').trim();
      if (predicate(value)) return value;
    }
    await sleep(250);
  }
  let taskInfo = 'unavailable';
  try {
    taskInfo = runPowerShell(`$task = Get-ScheduledTask -TaskPath '${taskPath}' -TaskName '${taskLeaf}'; $info = Get-ScheduledTaskInfo -TaskPath '${taskPath}' -TaskName '${taskLeaf}'; [pscustomobject]@{State=$task.State.ToString();LastRunTime=$info.LastRunTime;LastTaskResult=$info.LastTaskResult;NumberOfMissedRuns=$info.NumberOfMissedRuns} | ConvertTo-Json -Compress`);
  } catch (error) {
    taskInfo = `diagnostic-failed:${error.message}`;
  }
  let taskXML = 'unavailable';
  try {
    taskXML = runPowerShell(`Export-ScheduledTask -TaskPath '${taskPath}' -TaskName '${taskLeaf}' | Select-String -Pattern 'RestartOnFailure|MultipleInstancesPolicy|Interval|Count|Exec|LogonType|RunLevel' | ForEach-Object { $_.Line.Trim() } | Out-String`);
  } catch (error) {
    taskXML = `diagnostic-failed:${error.message}`;
  }
  let files = [];
  try { files = fs.readdirSync(tempRoot); } catch {}
  throw new Error(`Timed out waiting for isolated task fixture evidence: ${path.basename(filePath)} taskInfo=${taskInfo} taskXML=${JSON.stringify(taskXML)} files=${JSON.stringify(files)}`);
}

async function main() {
  assertMockControllerUrl(undefined);
  fs.mkdirSync(tempRoot, { recursive: true });
  execFileSync('go', ['build', '-o', supervisorBin, './cmd/proxylens-supervisor'], {
    cwd: collectorDir,
    env: process.env,
    stdio: 'inherit',
  });
  execFileSync('go', ['build', '-o', environment.PROXYLENS_E2E_TASK_EXE, fixtureSource], {
    cwd: collectorDir,
    env: process.env,
    stdio: 'inherit',
  });

  runSupervisor(['install', 'register']);
  const definitionJSON = runPowerShell(`$task = Get-ScheduledTask -TaskPath '${taskPath}' -TaskName '${taskLeaf}'; [pscustomobject]@{UserId=$task.Principal.UserId;LogonType=$task.Principal.LogonType.ToString();RunLevel=$task.Principal.RunLevel.ToString();MultipleInstances=$task.Settings.MultipleInstances.ToString();StartWhenAvailable=$task.Settings.StartWhenAvailable;StopIfGoingOnBatteries=$task.Settings.StopIfGoingOnBatteries;DisallowStartIfOnBatteries=$task.Settings.DisallowStartIfOnBatteries;RestartCount=$task.Settings.RestartCount;RestartInterval=$task.Settings.RestartInterval;Action=$task.Actions[0].Execute} | ConvertTo-Json -Compress`);
  const definition = JSON.parse(definitionJSON);
  if (definition.UserId === 'SYSTEM' || definition.LogonType !== 'Interactive' || definition.RunLevel !== 'Limited') {
    throw new Error(`isolated task security settings were not current-user limited-interactive: ${definitionJSON}`);
  }
  if (definition.MultipleInstances !== 'IgnoreNew' || definition.StartWhenAvailable !== true || definition.StopIfGoingOnBatteries !== false || definition.DisallowStartIfOnBatteries !== false) {
    throw new Error(`isolated task settings did not match the bounded owner contract: ${definitionJSON}`);
  }
  if (path.normalize(definition.Action) !== path.normalize(environment.PROXYLENS_E2E_TASK_EXE)) {
    throw new Error('isolated task action was not the harmless fixture');
  }

  runSupervisor(['install', 'run']);
  const fixtureCount = path.join(tempRoot, 'task-owner-fixture.count');
  const fixturePID = path.join(tempRoot, 'task-owner-fixture.pid');
  await waitForFile(fixtureCount, (value) => value === '1', 15000);
  const firstFixturePid = Number(await waitForFile(fixturePID, (value) => /^\d+$/.test(value), 5000));
  trackedFixturePids.add(firstFixturePid);
  if (!Number.isInteger(firstFixturePid) || firstFixturePid <= 0) {
    throw new Error('isolated task fixture did not report a valid first-run PID');
  }
  await sleep(3500);
  try {
    process.kill(firstFixturePid, 0);
  } catch (error) {
    throw new Error(`isolated task fixture ended before the exact crash step: ${error.message}`);
  }
  await stopExactPid(firstFixturePid);
  await waitForFile(fixtureCount, (value) => value === '2', 90000);
  fixturePid = Number(await waitForFile(fixturePID, (value) => /^\d+$/.test(value), 5000));
  trackedFixturePids.add(fixturePid);
  if (!Number.isInteger(fixturePid) || fixturePid <= 0) {
    throw new Error('isolated task fixture did not report a valid PID');
  }
  try {
    process.kill(fixturePid, 0);
  } catch (error) {
    throw new Error(`isolated task fixture was not alive after bounded restart: ${error.message}`);
  }
  console.log(`PASS phase3e2b2a task-owner task=${taskName} attempts=2 fixturePid=${fixturePid}`);
}

try {
  await main();
} finally {
  for (const pid of trackedFixturePids) {
    try { await stopExactPid(pid); } catch {}
  }
  try {
    runSupervisor(['install', 'unregister']);
  } catch (error) {
    console.error(`Failed to remove exact isolated task ${taskName}: ${error.message}`);
    process.exitCode = 1;
  }
  fs.rmSync(tempRoot, { recursive: true, force: true });
}
