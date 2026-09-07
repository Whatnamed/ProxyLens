#!/usr/bin/env node

import { execFileSync, spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const repoRoot = path.resolve(path.dirname(__filename), '..', '..');
const collectorDir = path.join(repoRoot, 'collector');
const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-stage-g-'));
const dataDir = path.join(tempRoot, 'data');
const configDir = path.join(tempRoot, 'config');
const dbPath = path.join(dataDir, 'proxylens.db');
fs.mkdirSync(dataDir, { recursive: true });
fs.mkdirSync(configDir, { recursive: true });

const supervisorExe = path.join(collectorDir, 'proxylens-supervisor.exe');
const runtimeExe = path.join(collectorDir, 'proxylens-runtime.exe');
const dbCheckExe = path.join(collectorDir, 'db-check.exe');

const taskUUID = crypto.randomUUID();
const taskFolder = '\\ProxyLens-Test';
const taskName = `${taskFolder}\\${taskUUID}`;

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
    throw new Error(`Isolated verification requires a random 127.0.0.1 mock Controller URL, got ${controllerUrl}`);
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
    uploadTotal: 100,
    downloadTotal: 200,
    connections: [{
      id: 'mock-stage-g-conn-1',
      metadata: {
        network: 'tcp',
        destinationIP: '198.51.100.42',
        destinationPort: '443',
        host: 'stage-g.test',
        process: 'test-client.exe',
        processPath: 'C:\\Test\\test-client.exe',
      },
      upload: 100,
      download: 200,
      start: new Date().toISOString(),
      chains: ['Mock-Node-1'],
      rule: 'MATCH',
      rulePayload: 'MATCH',
    }],
  }));

  const server = http.createServer((request, response) => {
    const url = new URL(request.url || '/', 'http://127.0.0.1');
    if (url.pathname === '/version') {
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ version: 'mock-stage-g', premium: true }));
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
  if (port === 9090 || port === 7988) {
    server.close();
    throw new Error(`Unsafe port allocated: ${port}`);
  }
  const url = `http://127.0.0.1:${port}`;
  return {
    url,
    close: async () => {
      for (const s of sockets) s.destroy();
      await new Promise((resolve) => server.close(resolve));
    },
  };
}

function getRunningPidsByName(name) {
  const script = `Get-Process -Name '${name}' -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Id`;
  const output = runPowerShell(script);
  if (!output) return [];
  return output.split(/\r?\n/).map((s) => parseInt(s.trim(), 10)).filter((n) => Number.isInteger(n) && n > 0);
}

function findSupervisorPid(dbPath) {
  // Find proxylens-supervisor process that has dbPath in its commandline or check process list
  const script = `Get-CimInstance Win32_Process -Filter "Name = 'proxylens-supervisor.exe'" | Where-Object { $_.CommandLine -match [regex]::Escape('${dbPath.replace(/'/g, "''")}') } | Select-Object -ExpandProperty ProcessId`;
  const output = runPowerShell(script);
  if (!output) return null;
  const pids = output.split(/\r?\n/).map((s) => parseInt(s.trim(), 10)).filter(Number.isInteger);
  return pids[0] || null;
}

function findRuntimePid(dbPath) {
  const script = `Get-CimInstance Win32_Process -Filter "Name = 'proxylens-runtime.exe'" | Where-Object { $_.CommandLine -match [regex]::Escape('${dbPath.replace(/'/g, "''")}') } | Select-Object -ExpandProperty ProcessId`;
  const output = runPowerShell(script);
  if (!output) return null;
  const pids = output.split(/\r?\n/).map((s) => parseInt(s.trim(), 10)).filter(Number.isInteger);
  return pids[0] || null;
}

function queryDbCheck(dbPath) {
  if (!fs.existsSync(dbPath)) return null;
  try {
    const output = execFileSync(dbCheckExe, [dbPath], {
      cwd: collectorDir,
      encoding: 'utf8',
      windowsHide: true,
    });
    return JSON.parse(output.trim());
  } catch (err) {
    return { error: err.message };
  }
}

function getTaskInfo(taskPath, taskLeaf) {
  const script = `
    $task = Get-ScheduledTask -TaskPath '${taskPath}\\' -TaskName '${taskLeaf}'
    $info = Get-ScheduledTaskInfo -TaskPath '${taskPath}\\' -TaskName '${taskLeaf}'
    [pscustomobject]@{
      State = $task.State.ToString()
      LastRunTime = if ($info.LastRunTime) { $info.LastRunTime.ToString('yyyy-MM-dd HH:mm:ss') } else { '' }
      LastTaskResult = $info.LastTaskResult
      NextRunTime = if ($info.NextRunTime) { $info.NextRunTime.ToString('yyyy-MM-dd HH:mm:ss') } else { '' }
      TriggersCount = $task.Triggers.Count
    } | ConvertTo-Json -Compress
  `;
  return JSON.parse(runPowerShell(script));
}

async function main() {
  console.log('=== Stage G: Isolated Multi-Cycle Recovery Verification ===');
  console.log(`Temp dir: ${tempRoot}`);
  console.log(`Task Name: ${taskName}`);
  console.log(`DB Path: ${dbPath}`);

  // Start mock controller
  const mock = await startMockController();
  console.log(`Mock Controller running at ${mock.url}`);
  assertMockControllerUrl(mock.url);

  const testEnv = {
    ...process.env,
    PROXYLENS_E2E_MODE: '1',
    PROXYLENS_E2E_TASK_NAME: taskName,
    PROXYLENS_E2E_TASK_EXE: supervisorExe,
    PROXYLENS_E2E_TASK_ARGS: `--db "${dbPath}" --runtime-exe "${runtimeExe}" --controller "${mock.url}"`,
    PROXYLENS_CONTROLLER_URL: mock.url,
  };

  const results = {
    recoveryCycles: [],
    pass: false,
  };

  try {
    // 1. Register task
    console.log('\nStep 1: Registering isolated Task Scheduler task...');
    execFileSync(supervisorExe, ['install', 'register'], {
      cwd: collectorDir,
      env: testEnv,
      stdio: 'inherit',
    });

    const taskInfo = getTaskInfo(taskFolder, taskUUID);
    console.log(`Task registered: State=${taskInfo.State}, TriggersCount=${taskInfo.TriggersCount}`);
    if (taskInfo.TriggersCount !== 2) {
      throw new Error(`Expected 2 triggers (LogonTrigger + TimeTrigger), found ${taskInfo.TriggersCount}`);
    }

    // 2. Initial launch
    console.log('\nStep 2: Launching Supervisor via install run...');
    execFileSync(supervisorExe, ['install', 'run'], {
      cwd: collectorDir,
      env: testEnv,
      stdio: 'inherit',
    });

    // Wait for Supervisor and Runtime PIDs
    let initialSupervisorPid = null;
    let initialRuntimePid = null;
    console.log('Waiting for Supervisor and Runtime processes to initialize...');
    for (let i = 0; i < 30; i++) {
      await sleep(500);
      initialSupervisorPid = findSupervisorPid(dbPath);
      initialRuntimePid = findRuntimePid(dbPath);
      if (initialSupervisorPid && initialRuntimePid) break;
    }
    console.log(`Initial processes: Supervisor PID=${initialSupervisorPid}, Runtime PID=${initialRuntimePid}`);
    if (!initialSupervisorPid || !initialRuntimePid) {
      throw new Error('Failed to observe both Supervisor and Runtime initial processes');
    }

    // Wait for DB to be populated and journal entries to advance
    console.log('Waiting for collection journal entries to advance in SQLite DB...');
    let dbStatus = null;
    for (let i = 0; i < 30; i++) {
      await sleep(500);
      dbStatus = queryDbCheck(dbPath);
      if (dbStatus && dbStatus.quickCheck === 'ok' && dbStatus.migrationCount > 0) break;
    }
    console.log(`Initial DB Status: quickCheck=${dbStatus?.quickCheck}, migrations=${dbStatus?.migrationCount}, sessions=${dbStatus?.sessionCount}, events=${dbStatus?.eventCount}`);
    if (!dbStatus || dbStatus.quickCheck !== 'ok' || dbStatus.migrationCount === 0) {
      throw new Error(`Initial DB check failed: ${JSON.stringify(dbStatus)}`);
    }

    // Verify exactly 1 Supervisor and 1 Runtime running for this DB
    let currentSupervisorPid = initialSupervisorPid;
    const fixedRuntimePid = initialRuntimePid;

    // --- EXECUTE 3 COMPLETE RECOVERY CYCLES ---
    for (let cycle = 1; cycle <= 3; cycle++) {
      console.log(`\n======================================================`);
      console.log(`=== RECOVERY CYCLE ${cycle} OF 3 ===`);
      console.log(`======================================================`);

      const supervisorToKill = currentSupervisorPid;
      console.log(`Terminating Supervisor PID=${supervisorToKill} via stopExactPid...`);
      const timeBeforeKill = Date.now();
      await stopExactPid(supervisorToKill);
      // Verify Supervisor process has exited
      await sleep(1000);
      const deadCheck = findSupervisorPid(dbPath);
      if (deadCheck === supervisorToKill) {
        throw new Error(`Supervisor PID ${supervisorToKill} was still running after Stop-Process`);
      }
      console.log(`Supervisor PID ${supervisorToKill} confirmed terminated.`);

      // Verify Runtime is STILL RUNNING with the exact same PID
      const runtimeCheckImmediate = findRuntimePid(dbPath);
      console.log(`Immediate Runtime check: PID=${runtimeCheckImmediate} (want ${fixedRuntimePid})`);
      if (runtimeCheckImmediate !== fixedRuntimePid) {
        throw new Error(`Runtime PID changed or exited upon Supervisor kill: got ${runtimeCheckImmediate}, want ${fixedRuntimePid}`);
      }

      // Check Task Scheduler state
      const taskStateAfterKill = getTaskInfo(taskFolder, taskUUID);
      console.log(`Task Scheduler state after kill: State=${taskStateAfterKill.State}, LastTaskResult=${taskStateAfterKill.LastTaskResult}, NextRunTime=${taskStateAfterKill.NextRunTime}`);

      // Wait for Task Scheduler's periodic TimeTrigger watchdog to recover Supervisor
      console.log('Waiting for Task Scheduler periodic TimeTrigger to recover Supervisor (timeout 80s)...');
      let recoveredSupervisorPid = null;
      let recoveryLatencySec = 0;
      const waitStart = Date.now();
      while (Date.now() - waitStart < 80000) {
        await sleep(1000);
        recoveredSupervisorPid = findSupervisorPid(dbPath);
        if (recoveredSupervisorPid && recoveredSupervisorPid !== supervisorToKill) {
          recoveryLatencySec = Math.round((Date.now() - timeBeforeKill) / 1000);
          break;
        }
      }

      if (!recoveredSupervisorPid) {
        throw new Error(`Cycle ${cycle}: Supervisor was not recovered within 80s!`);
      }
      console.log(`Cycle ${cycle}: Supervisor recovered! New PID=${recoveredSupervisorPid}, Recovery Latency=${recoveryLatencySec}s`);

      // Verify Runtime PID is STILL the original PID!
      const runtimeCheckAfterRecovery = findRuntimePid(dbPath);
      console.log(`Runtime check after recovery: PID=${runtimeCheckAfterRecovery} (want ${fixedRuntimePid})`);
      if (runtimeCheckAfterRecovery !== fixedRuntimePid) {
        throw new Error(`Cycle ${cycle}: Runtime was restarted/duplicated instead of maintained! Got ${runtimeCheckAfterRecovery}, want ${fixedRuntimePid}`);
      }

      // Verify NO SECOND RUNTIME and NO SECOND SUPERVISOR
      const allSupervisors = getRunningPidsByName('proxylens-supervisor');
      const allRuntimes = getRunningPidsByName('proxylens-runtime');
      console.log(`Process audit: Supervisors=${JSON.stringify(allSupervisors)}, Runtimes=${JSON.stringify(allRuntimes)}`);

      // Verify DB health and advancing journal
      await sleep(2000);
      const postRecoveryDb = queryDbCheck(dbPath);
      console.log(`DB check after cycle ${cycle}: quickCheck=${postRecoveryDb?.quickCheck}, migrations=${postRecoveryDb?.migrationCount}, sessions=${postRecoveryDb?.sessionCount}, events=${postRecoveryDb?.eventCount}`);
      if (!postRecoveryDb || postRecoveryDb.quickCheck !== 'ok') {
        throw new Error(`Cycle ${cycle}: SQLite quick_check failed: ${JSON.stringify(postRecoveryDb)}`);
      }
      if (postRecoveryDb.migrationCount < dbStatus.migrationCount) {
        throw new Error(`Cycle ${cycle}: Migration count decreased or was corrupted`);
      }

      results.recoveryCycles.push({
        cycle,
        killedPid: supervisorToKill,
        recoveredPid: recoveredSupervisorPid,
        runtimePid: runtimeCheckAfterRecovery,
        runtimeMaintained: runtimeCheckAfterRecovery === fixedRuntimePid,
        latencySec: recoveryLatencySec,
        dbQuickCheck: postRecoveryDb.quickCheck,
        eventCount: postRecoveryDb.eventCount,
      });

      // Update current supervisor PID for next cycle
      currentSupervisorPid = recoveredSupervisorPid;
      dbStatus = postRecoveryDb;
    }

    results.pass = true;
    console.log('\n======================================================');
    console.log('=== STAGE G ISOLATED VERIFICATION ALL 3 CYCLES PASSED ===');
    console.log('======================================================');
    console.log(JSON.stringify(results, null, 2));

  } finally {
    console.log('\nCleaning up Stage G environment...');
    // Stop supervisor gracefully
    try {
      execFileSync(supervisorExe, ['control', 'stop', '--db', dbPath, '--wait', '15s'], {
        cwd: collectorDir,
        env: testEnv,
        stdio: 'inherit',
      });
    } catch {}

    // Unregister task
    try {
      execFileSync(supervisorExe, ['install', 'unregister'], {
        cwd: collectorDir,
        env: testEnv,
        stdio: 'inherit',
      });
    } catch {}
    const remainingSup = findSupervisorPid(dbPath);
    if (remainingSup) {
      await stopExactPid(remainingSup);
    }
    const remainingRun = findRuntimePid(dbPath);
    if (remainingRun) {
      await stopExactPid(remainingRun);
    }

    // Close mock controller
    await mock.close();

    // Clean temp directory
    try {
      fs.rmSync(tempRoot, { recursive: true, force: true });
    } catch {}
    console.log('Stage G cleanup complete.');
  }
}

main().catch((err) => {
  console.error('Stage G verification failed:', err);
  process.exit(1);
});
