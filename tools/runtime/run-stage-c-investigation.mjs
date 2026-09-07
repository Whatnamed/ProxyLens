#!/usr/bin/env node

import { execFileSync, spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const repoRoot = path.resolve(path.dirname(__filename), '..', '..');
const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-investigation-'));
const fixtureSource = path.join(repoRoot, 'tools', 'runtime', 'fixtures', 'investigation-fixture.go');
const fixtureBin = path.join(tempRoot, 'investigation-fixture.exe');
const countPath = path.join(tempRoot, 'investigation-fixture.count');
const pidPath = path.join(tempRoot, 'investigation-fixture.pid');
const logPath = path.join(tempRoot, 'investigation-fixture.log');

const testTaskFolder = '\\ProxyLens-Test';
const testTaskUUID = crypto.randomUUID();
const testTaskName = `${testTaskFolder}\\${testTaskUUID}`;
const testTaskLeaf = testTaskUUID;

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
  // Scheduler-only investigation fixture intentionally uses no Controller.
  // PROXYLENS_CONTROLLER_URL is absent because this fixture never launches Runtime.
  if (controllerUrl !== undefined) throw new Error('Investigation fixture must not use a Controller');
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


function registerTaskWithExactProductionConfig(taskFolder, taskLeaf, exePath, args) {
  // Use exact same COM API and parameters as task_windows.go (isE2E = false)
  const script = `
    $service = New-Object -ComObject Schedule.Service
    $service.Connect()
    $root = $service.GetFolder('\\')
    $folderName = '${taskFolder}'.Trim('\\')
    try { $folder = $service.GetFolder('\\' + $folderName) } catch {
      try { $folder = $root.CreateFolder($folderName, $null) } catch { $folder = $service.GetFolder('\\' + $folderName) }
    }
    
    $definition = $service.NewTask(0)
    $regInfo = $definition.RegistrationInfo
    $regInfo.Description = 'ProxyLens investigation test task'
    
    $settings = $definition.Settings
    $settings.Enabled = $true
    $settings.StartWhenAvailable = $true
    $settings.StopIfGoingOnBatteries = $false
    $settings.DisallowStartIfOnBatteries = $false
    $settings.ExecutionTimeLimit = 'PT0S'
    $settings.MultipleInstances = 2 # taskInstancesIgnoreNew
    
    $principal = $definition.Principal
    $identity = [System.Security.Principal.WindowsIdentity]::GetCurrent()
    $principal.UserId = $identity.User.Value
    $principal.LogonType = 3 # taskLogonInteractiveToken
    $principal.RunLevel = 0 # taskRunLevelLimited
    
    $triggers = $definition.Triggers
    $trigger = $triggers.Create(9) # taskTriggerLogon
    $trigger.Enabled = $true
    $trigger.UserId = $identity.User.Value
    $repetition = $trigger.Repetition
    $repetition.Interval = 'PT1M'
    $repetition.StopAtDurationEnd = $false
    # Duration intentionally unset, identical to task_windows.go configureTaskTriggerRepetition
    
    $actions = $definition.Actions
    $action = $actions.Create(0) # taskActionExec
    $action.Path = '${exePath.replace(/'/g, "''")}'
    $action.Arguments = '${args.replace(/'/g, "''")}'
    $action.WorkingDirectory = '${path.dirname(exePath).replace(/'/g, "''")}'
    
    $folder.RegisterTaskDefinition('${taskLeaf}', $definition, 6, $identity.User.Value, $null, 3, $null) | Out-Null
  `;
  runPowerShell(script);
}

function registerTaskWithTimeTrigger(taskFolder, taskLeaf, exePath, args) {
  const script = `
    $service = New-Object -ComObject Schedule.Service
    $service.Connect()
    $root = $service.GetFolder('\\')
    $folderName = '${taskFolder}'.Trim('\\')
    try { $folder = $service.GetFolder('\\' + $folderName) } catch {
      try { $folder = $root.CreateFolder($folderName, $null) } catch { $folder = $service.GetFolder('\\' + $folderName) }
    }
    $definition = $service.NewTask(0)
    $regInfo = $definition.RegistrationInfo
    $regInfo.Description = 'ProxyLens investigation test task with TimeTrigger'
    
    $settings = $definition.Settings
    $settings.Enabled = $true
    $settings.StartWhenAvailable = $true
    $settings.StopIfGoingOnBatteries = $false
    $settings.DisallowStartIfOnBatteries = $false
    $settings.ExecutionTimeLimit = 'PT0S'
    $settings.MultipleInstances = 2 # taskInstancesIgnoreNew
    
    $principal = $definition.Principal
    $identity = [System.Security.Principal.WindowsIdentity]::GetCurrent()
    $principal.UserId = $identity.User.Value
    $principal.LogonType = 3 # taskLogonInteractiveToken
    $principal.RunLevel = 0 # taskRunLevelLimited
    
    $triggers = $definition.Triggers
    # Logon trigger
    $logonTrigger = $triggers.Create(9) # taskTriggerLogon
    $logonTrigger.Enabled = $true
    $logonTrigger.UserId = $identity.User.Value
    $logonRep = $logonTrigger.Repetition
    $logonRep.Interval = 'PT1M'
    $logonRep.StopAtDurationEnd = $false
    
    # Time trigger (like E2E activation trigger)
    $timeTrigger = $triggers.Create(1) # taskTriggerTime
    $timeTrigger.Enabled = $true
    $timeTrigger.StartBoundary = (Get-Date).AddSeconds(2).ToString('yyyy-MM-ddTHH:mm:ss')
    $timeRep = $timeTrigger.Repetition
    $timeRep.Interval = 'PT1M'
    $timeRep.StopAtDurationEnd = $false
    
    $actions = $definition.Actions
    $action = $actions.Create(0) # taskActionExec
    $action.Path = '${exePath.replace(/'/g, "''")}'
    $action.Arguments = '${args.replace(/'/g, "''")}'
    $action.WorkingDirectory = '${path.dirname(exePath).replace(/'/g, "''")}'
    
    $folder.RegisterTaskDefinition('${taskLeaf}', $definition, 6, $identity.User.Value, $null, 3, $null) | Out-Null
  `;
  runPowerShell(script);
}

function registerTaskWithRestartOnFailure(taskFolder, taskLeaf, exePath, args) {
  const script = `
    $service = New-Object -ComObject Schedule.Service
    $service.Connect()
    $root = $service.GetFolder('\\')
    $folderName = '${taskFolder}'.Trim('\\')
    try { $folder = $service.GetFolder('\\' + $folderName) } catch {
      try { $folder = $root.CreateFolder($folderName, $null) } catch { $folder = $service.GetFolder('\\' + $folderName) }
    }
    $definition = $service.NewTask(0)
    
    $settings = $definition.Settings
    $settings.Enabled = $true
    $settings.StartWhenAvailable = $true
    $settings.StopIfGoingOnBatteries = $false
    $settings.DisallowStartIfOnBatteries = $false
    $settings.ExecutionTimeLimit = 'PT0S'
    $settings.MultipleInstances = 2 # taskInstancesIgnoreNew
    $settings.RestartCount = 3
    $settings.RestartInterval = 'PT1M'
    
    $principal = $definition.Principal
    $identity = [System.Security.Principal.WindowsIdentity]::GetCurrent()
    $principal.UserId = $identity.User.Value
    $principal.LogonType = 3 # taskLogonInteractiveToken
    $principal.RunLevel = 0 # taskRunLevelLimited
    
    $triggers = $definition.Triggers
    $logonTrigger = $triggers.Create(9) # taskTriggerLogon
    $logonTrigger.Enabled = $true
    $logonTrigger.UserId = $identity.User.Value
    
    $actions = $definition.Actions
    $action = $actions.Create(0) # taskActionExec
    $action.Path = '${exePath.replace(/'/g, "''")}'
    $action.Arguments = '${args.replace(/'/g, "''")}'
    $action.WorkingDirectory = '${path.dirname(exePath).replace(/'/g, "''")}'
    
    $folder.RegisterTaskDefinition('${taskLeaf}', $definition, 6, $identity.User.Value, $null, 3, $null) | Out-Null
  `;
  runPowerShell(script);
}

function runTaskDemand(taskFolder, taskLeaf) {
  const script = `
    $service = New-Object -ComObject Schedule.Service
    $service.Connect()
    $folder = $service.GetFolder('${taskFolder}')
    $task = $folder.GetTask('${taskLeaf}')
    $task.Run($null) | Out-Null
  `;
  runPowerShell(script);
}

function getTaskInfo(taskFolder, taskLeaf) {
  const script = `
    $folder = '${taskFolder}'.Trim('\\')
    $path = '\\' + $folder + '\\'
    $task = Get-ScheduledTask -TaskPath $path -TaskName '${taskLeaf}'
    $info = Get-ScheduledTaskInfo -TaskPath $path -TaskName '${taskLeaf}'
    [pscustomobject]@{
      State = $task.State.ToString()
      LastRunTime = if ($info.LastRunTime) { $info.LastRunTime.ToString('yyyy-MM-dd HH:mm:ss') } else { '' }
      LastTaskResult = $info.LastTaskResult
      NextRunTime = if ($info.NextRunTime) { $info.NextRunTime.ToString('yyyy-MM-dd HH:mm:ss') } else { '' }
      NumberOfMissedRuns = $info.NumberOfMissedRuns
    } | ConvertTo-Json -Compress
  `;
  return JSON.parse(runPowerShell(script));
}

function unregisterTask(taskFolder, taskLeaf) {
  const script = `
    $service = New-Object -ComObject Schedule.Service
    $service.Connect()
    try {
      $folder = $service.GetFolder('${taskFolder}')
      $folder.DeleteTask('${taskLeaf}', 0)
    } catch {}
  `;
  try { runPowerShell(script); } catch {}
}

function cleanFiles() {
  try { fs.unlinkSync(countPath); } catch {}
  try { fs.unlinkSync(pidPath); } catch {}
  try { fs.unlinkSync(logPath); } catch {}
}

function getCount() {
  try {
    return parseInt(fs.readFileSync(countPath, 'utf8').trim(), 10) || 0;
  } catch {
    return 0;
  }
}

function getPid() {
  try {
    return parseInt(fs.readFileSync(pidPath, 'utf8').trim(), 10) || 0;
  } catch {
    return 0;
  }
}

async function main() {
  console.log('=== Stage C: Isolated Minimal Reproduction ===');
  console.log(`Temp dir: ${tempRoot}`);
  console.log(`Test task: ${testTaskName}`);

  // Build fixture
  console.log('Compiling investigation fixture...');
  execFileSync('go', ['build', '-o', fixtureBin, fixtureSource], {
    cwd: repoRoot,
    stdio: 'inherit',
  });

  const results = {};

  try {
    // --- CASE 1: 正常启动 -> 正常退出 (exit 0) on Production Config ---
    console.log('\n--- Running Case 1: Normal Exit (Exit 0) on Production Config ---');
    cleanFiles();
    unregisterTask(testTaskFolder, testTaskLeaf);
    registerTaskWithExactProductionConfig(testTaskFolder, testTaskLeaf, fixtureBin, '--mode=exit-zero');
    
    let info = getTaskInfo(testTaskFolder, testTaskLeaf);
    console.log(`Registered. Initial State=${info.State}, NextRunTime=${info.NextRunTime || 'none'}`);
    
    console.log('Starting task via Run()...');
    runTaskDemand(testTaskFolder, testTaskLeaf);
    for (let i = 0; i < 10; i++) {
      await sleep(500);
      if (getCount() >= 1) break;
    }
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    let countAfterRun = getCount();
    console.log(`After exit 0: State=${info.State}, LastTaskResult=${info.LastTaskResult}, NextRunTime=${info.NextRunTime || 'none'}, Count=${countAfterRun}`);
    
    console.log('Waiting 70s to observe if Task Scheduler restarts it...');
    await sleep(70000);
    
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    let finalCount1 = getCount();
    let restarted1 = finalCount1 > countAfterRun;
    console.log(`After 70s: State=${info.State}, LastTaskResult=${info.LastTaskResult}, NextRunTime=${info.NextRunTime || 'none'}, FinalCount=${finalCount1}, Restarted=${restarted1}`);
    results.case1 = {
      description: 'Production config: normal exit 0',
      initialState: info.State,
      lastTaskResult: info.LastTaskResult,
      nextRunTime: info.NextRunTime,
      restarted: restarted1,
    };

    // --- CASE 2: 正常启动 -> process self-exit non-zero (exit 1) on Production Config ---
    console.log('\n--- Running Case 2: Process Self-Exit Non-Zero (Exit 1) on Production Config ---');
    cleanFiles();
    unregisterTask(testTaskFolder, testTaskLeaf);
    registerTaskWithExactProductionConfig(testTaskFolder, testTaskLeaf, fixtureBin, '--mode=exit-nonzero');
    
    console.log('Starting task via Run()...');
    runTaskDemand(testTaskFolder, testTaskLeaf);
    await sleep(2000);
    
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    countAfterRun = getCount();
    console.log(`After exit 1: State=${info.State}, LastTaskResult=${info.LastTaskResult}, NextRunTime=${info.NextRunTime || 'none'}, Count=${countAfterRun}`);
    
    console.log('Waiting 70s to observe if Task Scheduler restarts it...');
    await sleep(70000);
    
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    let finalCount2 = getCount();
    let restarted2 = finalCount2 > countAfterRun;
    console.log(`After 70s: State=${info.State}, LastTaskResult=${info.LastTaskResult}, NextRunTime=${info.NextRunTime || 'none'}, FinalCount=${finalCount2}, Restarted=${restarted2}`);
    results.case2 = {
      description: 'Production config: exit non-zero 1',
      lastTaskResult: info.LastTaskResult,
      nextRunTime: info.NextRunTime,
      restarted: restarted2,
    };

    // --- CASE 3: 正常启动 -> 外部精确 kill on Production Config ---
    console.log('\n--- Running Case 3: External Kill (Stop-Process) on Production Config ---');
    cleanFiles();
    unregisterTask(testTaskFolder, testTaskLeaf);
    registerTaskWithExactProductionConfig(testTaskFolder, testTaskLeaf, fixtureBin, '--mode=sleep');
    
    console.log('Starting task via Run()...');
    runTaskDemand(testTaskFolder, testTaskLeaf);
    
    // Wait for process to write PID
    let pid = 0;
    for (let i = 0; i < 20; i++) {
      await sleep(500);
      pid = getPid();
      if (pid > 0) break;
    }
    console.log(`Process running with PID=${pid}`);
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    console.log(`Task State=${info.State}`);
    
    console.log(`Terminating process PID=${pid} via Stop-Process...`);
    runPowerShell(`Stop-Process -Id ${pid} -Force`);
    await sleep(2000);
    
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    countAfterRun = getCount();
    console.log(`After kill: State=${info.State}, LastTaskResult=${info.LastTaskResult} (hex: 0x${(info.LastTaskResult >>> 0).toString(16).toUpperCase()}), NextRunTime=${info.NextRunTime || 'none'}, Count=${countAfterRun}`);
    
    console.log('Waiting 70s to observe if Task Scheduler restarts it...');
    await sleep(70000);
    
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    let finalCount3 = getCount();
    let restarted3 = finalCount3 > countAfterRun;
    console.log(`After 70s: State=${info.State}, LastTaskResult=${info.LastTaskResult}, NextRunTime=${info.NextRunTime || 'none'}, FinalCount=${finalCount3}, Restarted=${restarted3}`);
    results.case3 = {
      description: 'Production config: external kill',
      lastTaskResult: info.LastTaskResult,
      lastTaskResultHex: `0x${(info.LastTaskResult >>> 0).toString(16).toUpperCase()}`,
      nextRunTime: info.NextRunTime,
      restarted: restarted3,
    };

    // --- CASE 4: Supervisor crash / unhandled exception on Production Config ---
    console.log('\n--- Running Case 4: Process Crash (Panic) on Production Config ---');
    cleanFiles();
    unregisterTask(testTaskFolder, testTaskLeaf);
    registerTaskWithExactProductionConfig(testTaskFolder, testTaskLeaf, fixtureBin, '--mode=crash');
    
    console.log('Starting task via Run()...');
    runTaskDemand(testTaskFolder, testTaskLeaf);
    await sleep(2000);
    
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    countAfterRun = getCount();
    console.log(`After crash: State=${info.State}, LastTaskResult=${info.LastTaskResult} (hex: 0x${(info.LastTaskResult >>> 0).toString(16).toUpperCase()}), NextRunTime=${info.NextRunTime || 'none'}, Count=${countAfterRun}`);
    
    console.log('Waiting 70s to observe if Task Scheduler restarts it...');
    await sleep(70000);
    
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    let finalCount4 = getCount();
    let restarted4 = finalCount4 > countAfterRun;
    console.log(`After 70s: State=${info.State}, LastTaskResult=${info.LastTaskResult}, NextRunTime=${info.NextRunTime || 'none'}, FinalCount=${finalCount4}, Restarted=${restarted4}`);
    results.case4 = {
      description: 'Production config: crash panic',
      lastTaskResult: info.LastTaskResult,
      nextRunTime: info.NextRunTime,
      restarted: restarted4,
    };

    // --- CASE 5: 机器无需 reboot 的 scheduler trigger 再触发行为 ---
    console.log('\n--- Running Case 5A: TimeTrigger with Repetition Interval=PT1M (like E2E) ---');
    cleanFiles();
    unregisterTask(testTaskFolder, testTaskLeaf);
    registerTaskWithTimeTrigger(testTaskFolder, testTaskLeaf, fixtureBin, '--mode=sleep');
    
    // Time trigger starts automatically after 2 seconds
    console.log('Waiting for TimeTrigger to fire...');
    let pid5a = 0;
    for (let i = 0; i < 30; i++) {
      await sleep(500);
      pid5a = getPid();
      if (pid5a > 0) break;
    }
    console.log(`TimeTrigger started process with PID=${pid5a}, Count=${getCount()}`);
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    console.log(`Task State=${info.State}, NextRunTime=${info.NextRunTime}`);
    
    console.log(`Terminating PID=${pid5a}...`);
    runPowerShell(`Stop-Process -Id ${pid5a} -Force`);
    await sleep(2000);
    
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    let countBefore5a = getCount();
    console.log(`After kill: State=${info.State}, NextRunTime=${info.NextRunTime}, Count=${countBefore5a}`);
    
    console.log('Waiting up to 75s for TimeTrigger repetition to restart process...');
    let restarted5a = false;
    let restartLatency5a = 0;
    const startWait5a = Date.now();
    while (Date.now() - startWait5a < 75000) {
      await sleep(1000);
      if (getCount() > countBefore5a) {
        restarted5a = true;
        restartLatency5a = Math.round((Date.now() - startWait5a) / 1000);
        break;
      }
    }
    let newPid5a = getPid();
    console.log(`TimeTrigger Restarted=${restarted5a}, Latency=${restartLatency5a}s, NewPID=${newPid5a}`);
    if (newPid5a > 0) {
      try { runPowerShell(`Stop-Process -Id ${newPid5a} -Force`); } catch {}
    }
    results.case5a = {
      description: 'TimeTrigger with PT1M repetition',
      nextRunTime: info.NextRunTime,
      restarted: restarted5a,
      latencySec: restartLatency5a,
    };

    console.log('\n--- Running Case 5B: Settings.RestartOnFailure (RestartCount=3, RestartInterval=PT1M) on External Kill ---');
    cleanFiles();
    unregisterTask(testTaskFolder, testTaskLeaf);
    registerTaskWithRestartOnFailure(testTaskFolder, testTaskLeaf, fixtureBin, '--mode=sleep');
    
    console.log('Starting task via Run()...');
    runTaskDemand(testTaskFolder, testTaskLeaf);
    
    let pid5b = 0;
    for (let i = 0; i < 20; i++) {
      await sleep(500);
      pid5b = getPid();
      if (pid5b > 0) break;
    }
    console.log(`Process running with PID=${pid5b}`);
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    console.log(`Task State=${info.State}`);
    
    console.log(`Terminating process PID=${pid5b} via Stop-Process...`);
    runPowerShell(`Stop-Process -Id ${pid5b} -Force`);
    await sleep(2000);
    
    info = getTaskInfo(testTaskFolder, testTaskLeaf);
    let countBefore5b = getCount();
    console.log(`After kill: State=${info.State}, LastTaskResult=${info.LastTaskResult}, NextRunTime=${info.NextRunTime || 'none'}, Count=${countBefore5b}`);
    
    console.log('Waiting 75s to observe if Task Scheduler RestartOnFailure restarts it...');
    let restarted5b = false;
    let restartLatency5b = 0;
    const startWait5b = Date.now();
    while (Date.now() - startWait5b < 75000) {
      await sleep(1000);
      if (getCount() > countBefore5b) {
        restarted5b = true;
        restartLatency5b = Math.round((Date.now() - startWait5b) / 1000);
        break;
      }
    }
    let newPid5b = getPid();
    console.log(`RestartOnFailure on External Kill: Restarted=${restarted5b}, Latency=${restartLatency5b}s, NewPID=${newPid5b}`);
    if (newPid5b > 0) {
      try { runPowerShell(`Stop-Process -Id ${newPid5b} -Force`); } catch {}
    }
    results.case5b = {
      description: 'Settings.RestartOnFailure on external kill',
      restarted: restarted5b,
      latencySec: restartLatency5b,
    };

    console.log('\n=== Investigation Results Summary ===');
    console.log(JSON.stringify(results, null, 2));

  } finally {
    console.log('\nCleaning up investigation task and temporary files...');
    unregisterTask(testTaskFolder, testTaskLeaf);
    cleanFiles();
    try { fs.rmSync(tempRoot, { recursive: true, force: true }); } catch {}
    console.log('Cleanup complete.');
  }
}

main().catch((err) => {
  console.error('Investigation script error:', err);
  process.exit(1);
});
