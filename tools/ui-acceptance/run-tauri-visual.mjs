#!/usr/bin/env node

/**
 * Run one real Tauri window against an isolated, read-only synthetic fixture.
 * This runner never starts the installed owner, Supervisor, Runtime or
 * Collector: the Rust visual-QA double gate is responsible for that boundary.
 */

import { createHash } from 'node:crypto';
import { spawn, execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const rootDir = path.resolve(__dirname, '..', '..');
const uiDir = path.join(rootDir, 'ui');
const fixtureDir = path.join(rootDir, 'fixtures');
const evidenceRoot = path.join(rootDir, 'tmp', 'phase3-final-acceptance');
const profiles = new Set(['healthy', 'gaps', 'stale', 'empty', 'scaled']);
const sizes = new Set(['1280x800', '1440x900', '1600x1000', 'max']);

const args = parseArgs(process.argv.slice(2));
const profile = args.profile || 'healthy';
const size = args.size || '1600x1000';
const runId = safeRunId(args['run-id'] || `${profile}-${size}-${Date.now()}`);
const autoExitMs = parsePositiveInt(args['auto-exit-ms'] || '30000', 'auto-exit-ms');
const cdpPort = parsePositiveInt(args['cdp-port'] || '9223', 'cdp-port');

if (!profiles.has(profile)) throw new Error(`Unsupported profile: ${profile}`);
if (!sizes.has(size)) throw new Error(`Unsupported size: ${size}`);

const sourceDb = path.join(fixtureDir, `fixture_${profile}.db`);
if (!fs.existsSync(sourceDb)) {
  throw new Error(`Fixture not found: ${sourceDb}. Run node tools/ui-fixture/generate-fixtures.mjs first.`);
}

const metadataPath = path.join(rootDir, 'tmp', 'ui-fixtures-metadata.json');
const fixtureMetadata = fs.existsSync(metadataPath)
  ? JSON.parse(fs.readFileSync(metadataPath, 'utf8'))
  : null;
if (fixtureMetadata && !fixtureMetadata.profiles?.includes(profile)) {
  throw new Error(`Fixture metadata does not include profile ${profile}`);
}

fs.mkdirSync(evidenceRoot, { recursive: true });
const runDir = fs.mkdtempSync(path.join(evidenceRoot, `${runId}-`));
const copyDb = path.join(runDir, 'fixture.db');
const configDir = path.join(runDir, 'config');
const logPath = path.join(runDir, 'tauri.log');
const reportPath = path.join(runDir, 'run.json');
fs.mkdirSync(configDir, { recursive: true });

const sourceShaBefore = sha256File(sourceDb);
fs.copyFileSync(sourceDb, copyDb);
const copyShaBefore = sha256File(copyDb);
if (sourceShaBefore !== copyShaBefore) throw new Error('Fixture copy SHA256 mismatch before launch');

const environment = { ...process.env };
for (const name of [
  'PROXYLENS_CONTROLLER_URL',
  'MIHOMO_SECRET',
  'PROXYLENS_DATA_DIR',
  'PROXYLENS_E2E_CREDENTIAL_TARGET',
  'PROXYLENS_E2E_STATUS_FILE',
  'PROXYLENS_E2E_TASK_NAME',
  'PROXYLENS_E2E_TASK_EXE',
  'PROXYLENS_E2E_TASK_ARGS',
  'PROXYLENS_E2E_DIRECT_OWNER',
  'PROXYLENS_E2E_TASK_SCHEDULE',
  'PROXYLENS_SETTINGS_E2E',
]) {
  delete environment[name];
}
environment.PROXYLENS_E2E_MODE = '1';
environment.PROXYLENS_VISUAL_QA_QUERY_ONLY = '1';
environment.PROXYLENS_DB_PATH = copyDb;
environment.PROXYLENS_CONFIG_DIR = configDir;
environment.PROXYLENS_E2E_AUTO_EXIT_MS = String(autoExitMs);
environment.WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS = `--remote-debugging-port=${cdpPort}`;
if (size !== 'max') environment.PROXYLENS_VISUAL_QA_WINDOW_SIZE = size;
else delete environment.PROXYLENS_VISUAL_QA_WINDOW_SIZE;

const logStream = fs.createWriteStream(logPath, { flags: 'w' });
let output = '';
let child;

try {
  // Keep this check explicit without ever printing a secret value.
  if (environment.PROXYLENS_CONTROLLER_URL || environment.MIHOMO_SECRET) {
    throw new Error('Visual QA runner refused to start with Controller/Secret environment authority');
  }

  child = spawn('cmd.exe', ['/d', '/s', '/c', 'npm.cmd run tauri:dev'], {
    cwd: uiDir,
    env: environment,
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });

  const append = (chunk) => {
    const text = chunk.toString();
    output += text;
    logStream.write(text);
    process.stdout.write(text);
  };
  child.stdout.on('data', append);
  child.stderr.on('data', append);

  await waitForOutput(child, () => output.includes('PROXYLENS_VISUAL_QA_READY'), 90000, 'visual QA query sidecar');
  const expectedProbe = profile === 'empty'
    ? 'PROXYLENS_WEBVIEW_E2E_READY meta=0 summary=0 connections=0'
    : 'PROXYLENS_WEBVIEW_E2E_READY meta=1 summary=1 connections=1';
  await waitForOutput(child, () => output.includes(expectedProbe), 30000, 'WebView Query API probe');
  await waitForProcessExit(child, autoExitMs + 15000, 'Tauri visual QA auto-exit');
  if (child.exitCode !== 0) {
    throw new Error(`Tauri visual QA process exited with code ${child.exitCode}; evidence log: ${logPath}`);
  }

  const sourceShaAfter = sha256File(sourceDb);
  const copyShaAfter = sha256File(copyDb);
  if (sourceShaAfter !== sourceShaBefore) throw new Error('Source fixture changed during visual QA');
  if (copyShaAfter !== copyShaBefore) throw new Error('Visual QA copy changed; query-only DB must remain read-only');
  if (!output.includes('PROXYLENS_VISUAL_QA_MODE queryOnly=1 owner=0 runtime=0 controller=0')) {
    throw new Error('Visual QA mode marker did not confirm owner/runtime/controller skip');
  }

  const report = {
    runId,
    profile,
    size,
    sourceFixture: path.relative(rootDir, sourceDb),
    sourceSha256: sourceShaBefore,
    copiedFixtureSha256: copyShaAfter,
    anchor: fixtureMetadata?.anchor || null,
    fixtureMetadata: fixtureMetadata ? path.relative(rootDir, metadataPath) : null,
    visualQa: 'query-only',
    controller: 'none',
    productionDatabase: false,
    supervisorStarted: false,
    runtimeStarted: false,
    sourceUnchanged: sourceShaAfter === sourceShaBefore,
    copyUnchanged: copyShaAfter === copyShaBefore,
    cdpPort,
    webviewProbe: expectedProbe,
    evidenceDir: path.relative(rootDir, runDir),
  };
  fs.writeFileSync(reportPath, `${JSON.stringify(report, null, 2)}\n`, 'utf8');
  console.log(`PASS tauri-visual profile=${profile} size=${size} run=${runId} queryOnly=1 owner=0 runtime=0 controller=0 sourceUnchanged=1 copyUnchanged=1`);
} finally {
  logStream.end();
  if (child && child.exitCode === null && !child.killed) terminateProcessTree(child.pid);
}

function parseArgs(argv) {
  const parsed = {};
  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index];
    if (!token.startsWith('--')) continue;
    const equal = token.indexOf('=');
    if (equal >= 0) {
      parsed[token.slice(2, equal)] = token.slice(equal + 1);
    } else {
      parsed[token.slice(2)] = argv[index + 1] && !argv[index + 1].startsWith('--') ? argv[++index] : 'true';
    }
  }
  return parsed;
}

function safeRunId(value) {
  const safe = String(value).replace(/[^a-zA-Z0-9._-]+/g, '-').replace(/^-+|-+$/g, '');
  if (!safe) throw new Error('run-id must contain at least one safe filename character');
  return safe.slice(0, 80);
}

function parsePositiveInt(value, name) {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) throw new Error(`--${name} must be a positive integer`);
  return parsed;
}

function sha256File(filePath) {
  return createHash('sha256').update(fs.readFileSync(filePath)).digest('hex');
}

function waitForOutput(processHandle, predicate, timeoutMs, label) {
  if (predicate()) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const deadline = setTimeout(() => {
      cleanup();
      reject(new Error(`Timed out waiting for ${label}. Evidence log: ${logPath}`));
    }, timeoutMs);
    const onExit = (code, signal) => {
      cleanup();
      reject(new Error(`${label} process exited before readiness (code=${code}, signal=${signal})`));
    };
    const interval = setInterval(() => {
      if (predicate()) {
        cleanup();
        resolve();
      }
    }, 50);
    processHandle.once('exit', onExit);
    function cleanup() {
      clearTimeout(deadline);
      clearInterval(interval);
      processHandle.removeListener('exit', onExit);
    }
  });
}

function waitForProcessExit(processHandle, timeoutMs, label) {
  if (processHandle.exitCode !== null) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const deadline = setTimeout(() => {
      cleanup();
      reject(new Error(`Timed out waiting for ${label}`));
    }, timeoutMs);
    const onExit = () => {
      cleanup();
      resolve();
    };
    processHandle.once('exit', onExit);
    function cleanup() {
      clearTimeout(deadline);
      processHandle.removeListener('exit', onExit);
    }
  });
}

function terminateProcessTree(pid) {
  if (!pid) return;
  try {
    execFileSync('taskkill', ['/PID', String(pid), '/T', '/F'], { stdio: 'ignore' });
  } catch {
    try { process.kill(pid); } catch {}
  }
}
