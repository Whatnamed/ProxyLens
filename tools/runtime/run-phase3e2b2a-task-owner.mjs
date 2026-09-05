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

function assertNonElevatedToken() {
  const output = runPowerShell(`$identity = [Security.Principal.WindowsIdentity]::GetCurrent(); $principal = [Security.Principal.WindowsPrincipal]::new($identity); [pscustomobject]@{User=$identity.Name;IsElevated=$principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)} | ConvertTo-Json -Compress`);
  let token;
  try { token = JSON.parse(output); } catch { throw new Error('non-elevated feasibility gate failed: current test token could not be inspected'); }
  if (!token || typeof token.User !== 'string' || typeof token.IsElevated !== 'boolean') {
    throw new Error('non-elevated feasibility gate failed: current test token result was incomplete');
  }
  if (token.IsElevated) {
    throw new Error('non-elevated feasibility gate failed: current test token is elevated; cannot prove non-elevated registration');
  }
  return token;
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

function readTaskDefinition() {
  return JSON.parse(runPowerShell(`$task = Get-ScheduledTask -TaskPath '${taskPath}' -TaskName '${taskLeaf}'; $xml = [xml](Export-ScheduledTask -TaskPath '${taskPath}' -TaskName '${taskLeaf}'); $identity = [Security.Principal.WindowsIdentity]::GetCurrent(); $taskUserSid = ''; try { $taskUserSid = ([Security.Principal.NTAccount]::new([string]$task.Principal.UserId)).Translate([Security.Principal.SecurityIdentifier]).Value } catch { $taskUserSid = [string]$task.Principal.UserId }; function Get-ChildText($node, $localName) { if ($null -eq $node) { return '' }; $child = @($node.ChildNodes | Where-Object { $_.NodeType -eq [System.Xml.XmlNodeType]::Element -and $_.LocalName -eq $localName }) | Select-Object -First 1; if ($null -eq $child) { return '' }; return [string]$child.InnerText }; function Get-Repetition($trigger) { $repetition = @($trigger.ChildNodes | Where-Object { $_.NodeType -eq [System.Xml.XmlNodeType]::Element -and $_.LocalName -eq 'Repetition' }) | Select-Object -First 1; $duration = Get-ChildText $repetition 'Duration'; return [pscustomobject]@{Interval=(Get-ChildText $repetition 'Interval');Duration=$duration;HasDuration=($duration -ne '');StopAtDurationEnd=(Get-ChildText $repetition 'StopAtDurationEnd')} }; $triggers = @($xml.Task.Triggers.ChildNodes | Where-Object { $_.NodeType -eq [System.Xml.XmlNodeType]::Element }); $logon = @($triggers | Where-Object { $_.LocalName -eq 'LogonTrigger' }); $time = @($triggers | Where-Object { $_.LocalName -eq 'TimeTrigger' }); $logonRepetition = if ($logon.Count -eq 1) { Get-Repetition $logon[0] } else { [pscustomobject]@{Interval='';Duration='';HasDuration=$false;StopAtDurationEnd=''} }; $timeRepetition = if ($time.Count -eq 1) { Get-Repetition $time[0] } else { [pscustomobject]@{Interval='';Duration='';HasDuration=$false;StopAtDurationEnd=''} }; [pscustomobject]@{UserId=$task.Principal.UserId;CurrentUser=$identity.Name;TaskUserSid=$taskUserSid;CurrentUserSid=$identity.User.Value;LogonType=$task.Principal.LogonType.ToString();RunLevel=$task.Principal.RunLevel.ToString();MultipleInstances=$task.Settings.MultipleInstances.ToString();StartWhenAvailable=$task.Settings.StartWhenAvailable;StopIfGoingOnBatteries=$task.Settings.StopIfGoingOnBatteries;DisallowStartIfOnBatteries=$task.Settings.DisallowStartIfOnBatteries;Action=$task.Actions[0].Execute;TriggerNames=@($triggers | ForEach-Object { $_.LocalName });TotalTriggerCount=$triggers.Count;LogonTriggerCount=$logon.Count;TimeTriggerCount=$time.Count;LogonRepetitionInterval=$logonRepetition.Interval;LogonRepetitionDuration=$logonRepetition.Duration;LogonHasDuration=$logonRepetition.HasDuration;LogonStopAtDurationEnd=$logonRepetition.StopAtDurationEnd;TimeRepetitionInterval=$timeRepetition.Interval;TimeRepetitionDuration=$timeRepetition.Duration;TimeHasDuration=$timeRepetition.HasDuration;TimeStopAtDurationEnd=$timeRepetition.StopAtDurationEnd;HasRestartOnFailure=(@($xml.Task.Settings.ChildNodes | Where-Object { $_.NodeType -eq [System.Xml.XmlNodeType]::Element -and $_.LocalName -eq 'RestartOnFailure' }).Count -gt 0)} | ConvertTo-Json -Compress`));
}

function readTaskTriggerRepetitionProperties() {
  const output = runPowerShell(`$task = Get-ScheduledTask -TaskPath '${taskPath}' -TaskName '${taskLeaf}'; @($task.Triggers | ForEach-Object { [pscustomobject]@{Type=$_.CimClass.CimClassName;Interval=[string]$_.Repetition.Interval;Duration=[string]$_.Repetition.Duration;StopAtDurationEnd=[string]$_.Repetition.StopAtDurationEnd} }) | ConvertTo-Json -Compress`);
  const parsed = JSON.parse(output);
  return Array.isArray(parsed) ? parsed : [parsed];
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

  const token = assertNonElevatedToken();
  console.log(`PASS non-elevated token IsElevated=false user=${token.User}`);
  registrationAttempted = true;
  runSupervisor(['install', 'register']);
  const definition = readTaskDefinition();
  if (definition.TaskUserSid !== definition.CurrentUserSid || definition.LogonType !== 'Interactive' || definition.RunLevel !== 'Limited') {
    throw new Error(`isolated task security settings were not current-user limited-interactive: ${JSON.stringify(definition)}`);
  }
  if (definition.MultipleInstances !== 'IgnoreNew' || definition.StartWhenAvailable !== true || definition.StopIfGoingOnBatteries !== false || definition.DisallowStartIfOnBatteries !== false) {
    throw new Error(`isolated task settings did not match the bounded owner contract: ${JSON.stringify(definition)}`);
  }
  if (definition.HasRestartOnFailure || definition.LogonTriggerCount !== 1 || definition.TimeTriggerCount !== 1 || definition.TotalTriggerCount !== 2) {
    throw new Error(`isolated task trigger set did not match the E2E activation contract: ${JSON.stringify(definition)}`);
  }
  const triggerRepetitions = readTaskTriggerRepetitionProperties();
  if (triggerRepetitions.length !== 2) {
    throw new Error(`isolated task repetition readback did not contain exactly LogonTrigger and TimeTrigger: ${JSON.stringify(triggerRepetitions)}`);
  }
  for (const prefix of ['Logon', 'Time']) {
    const expectedType = `MSFT_Task${prefix}Trigger`;
    const repetition = triggerRepetitions.find((item) => item.Type === expectedType);
    if (!repetition || repetition.Interval !== 'PT1M' || repetition.Duration !== '' || String(repetition.StopAtDurationEnd).toLowerCase() !== 'false') {
      throw new Error(`isolated task ${prefix}Trigger repetition did not match the indefinite PT1M contract: ${JSON.stringify({ definition, triggerRepetitions })}`);
    }
  }
  if (path.normalize(definition.Action) !== path.normalize(environment.PROXYLENS_E2E_TASK_EXE)) {
    throw new Error('isolated task action was not the harmless fixture');
  }

  // The E2E TimeTrigger is the only activation aid. There is deliberately no
  // install run, second Run, manual spawn, or extra restart trigger.
  const fixtureCount = path.join(tempRoot, 'task-owner-fixture.count');
  const fixturePID = path.join(tempRoot, 'task-owner-fixture.pid');
  await waitForFile(fixtureCount, (value) => value === '1', 15000);
  const firstFixturePid = Number(await waitForFile(fixturePID, (value) => /^\d+$/.test(value), 5000));
  trackedFixturePids.add(firstFixturePid);
  if (!Number.isInteger(firstFixturePid) || firstFixturePid <= 0) {
    throw new Error('isolated task fixture did not report a valid first-run PID');
  }
  try {
    process.kill(firstFixturePid, 0);
  } catch (error) {
    throw new Error(`isolated task fixture ended before the exact crash step: ${error.message}`);
  }
  await stopExactPid(firstFixturePid);
  if (isPidAlive(firstFixturePid)) {
    throw new Error('isolated task fixture PID A remained alive after exact crash simulation');
  }
  await waitForFile(fixtureCount, (value) => value === '2', 90000);
  fixturePid = Number(await waitForFile(fixturePID, (value) => /^\d+$/.test(value) && Number(value) !== firstFixturePid, 10000));
  trackedFixturePids.add(fixturePid);
  if (!Number.isInteger(fixturePid) || fixturePid <= 0) {
    throw new Error('isolated task fixture did not report a valid PID');
  }
  try {
    process.kill(fixturePid, 0);
  } catch (error) {
    throw new Error(`isolated task fixture was not alive after bounded restart: ${error.message}`);
  }
  console.log(`PASS phase3e2b2a task-owner task=${taskName} user=${definition.CurrentUser} pidA=${firstFixturePid} pidB=${fixturePid} logonRepetition=PT1M/indefinite/stopAtDurationEnd=false timeRepetition=PT1M/indefinite/stopAtDurationEnd=false multipleInstances=${definition.MultipleInstances} restartOnFailure=false`);
}

let registrationAttempted = false;
try {
  await main();
} finally {
  let cleanupFailed = false;
  for (const pid of trackedFixturePids) {
    try {
      await stopExactPid(pid);
      if (isPidAlive(pid)) cleanupFailed = true;
    } catch {
      cleanupFailed = true;
    }
  }
  if (registrationAttempted) {
    try {
      runSupervisor(['install', 'unregister']);
    } catch (error) {
      console.error(`Failed to remove exact isolated task ${taskName}: ${error.message}`);
      cleanupFailed = true;
    }
  }
  fs.rmSync(tempRoot, { recursive: true, force: true });
  if (fs.existsSync(tempRoot)) cleanupFailed = true;
  if (cleanupFailed) {
    process.exitCode = 1;
  } else {
    console.log(`PASS exact cleanup task=${taskName} fixturePids=${[...trackedFixturePids].join(',') || 'none'}`);
  }
}
