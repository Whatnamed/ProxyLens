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
import {
  validateVisualQaRunnerInputs,
} from './visual-qa-policy.mjs';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const rootDir = path.resolve(__dirname, '..', '..');
const uiDir = path.join(rootDir, 'ui');
const fixtureDir = path.join(rootDir, 'fixtures');
const evidenceRoot = path.join(rootDir, 'tmp', 'phase3-final-acceptance');
const profiles = new Set(['healthy', 'gaps', 'stale', 'empty', 'scaled', 'review', 'review-temporal', 'review-temporal-incomplete']);
const sizes = new Set(['1280x800', '1440x900', '1600x1000', 'max']);
const locales = new Set(['en', 'zh-CN']);
const themes = new Set(['light', 'dark']);

const args = parseArgs(process.argv.slice(2));
const profile = args.profile || 'healthy';
const size = args.size || '1600x1000';
const locale = args.locale || 'en';
const theme = args.theme || 'light';
const view = args.view || (profile === 'review' || profile === 'review-temporal' || profile === 'review-temporal-incomplete' ? 'review' : 'overview');
const runId = safeRunId(args['run-id'] || `${profile}-${size}-${Date.now()}`);
const autoExitMs = parsePositiveInt(args['auto-exit-ms'] || '30000', 'auto-exit-ms');
const cdpPort = parsePositiveInt(args['cdp-port'] || '9223', 'cdp-port');

if (!profiles.has(profile)) throw new Error(`Unsupported profile: ${profile}`);
if (!sizes.has(size)) throw new Error(`Unsupported size: ${size}`);
if (!locales.has(locale)) throw new Error(`Unsupported locale: ${locale}`);
if (!themes.has(theme)) throw new Error(`Unsupported theme: ${theme}`);
if (!['overview', 'review'].includes(view)) throw new Error(`Unsupported view: ${view}`);

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
  validateVisualQaRunnerInputs({
    e2eMode: environment.PROXYLENS_E2E_MODE,
    visualQaFlag: environment.PROXYLENS_VISUAL_QA_QUERY_ONLY,
    explicitDbPath: copyDb,
    fixtureExists: fs.existsSync(copyDb),
    canonicalProductionDbPath: environment.LOCALAPPDATA
      ? path.join(environment.LOCALAPPDATA, 'ProxyLens', 'data', 'proxylens.db')
      : null,
    controllerUrl: environment.PROXYLENS_CONTROLLER_URL,
    mihomoSecret: environment.MIHOMO_SECRET,
  });

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
  await setWebviewPreferences(cdpPort, locale, theme);
  await waitForWebviewPreferences(cdpPort, locale, theme, 30000);
  await selectWebviewView(cdpPort, view);
  const actualState = await waitForWebviewState(cdpPort, size, locale, theme, view, 30000);
  const temporalEvidence = profile === 'review-temporal'
    ? await assertTemporalReviewEvidence(cdpPort, locale)
    : profile === 'review-temporal-incomplete'
      ? await assertTemporalAccountingIncompleteEvidence(cdpPort, locale)
      : null;
  const actualViewport = { width: actualState.width, height: actualState.height, dpr: actualState.dpr };
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
    locale,
    theme,
    view,
    requestedSize: size,
    actualViewport,
    actualLocale: actualState.locale,
    actualTheme: actualState.theme,
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
    temporalEvidence,
    evidenceDir: path.relative(rootDir, runDir),
  };
  fs.writeFileSync(reportPath, `${JSON.stringify(report, null, 2)}\n`, 'utf8');
  console.log(`PASS tauri-visual profile=${profile} view=${view} size=${size} locale=${locale} theme=${theme} viewport=${actualViewport.width}x${actualViewport.height} run=${runId} queryOnly=1 owner=0 runtime=0 controller=0 sourceUnchanged=1 copyUnchanged=1`);
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

async function setWebviewPreferences(port, locale, theme) {
  await evaluateWebview(port, `(() => {
    localStorage.setItem('pl-locale', ${JSON.stringify(locale)});
    localStorage.setItem('pl-theme', ${JSON.stringify(theme)});
    setTimeout(() => location.reload(), 0);
    return true;
  })()`);
}

async function waitForWebviewPreferences(port, expectedLocale, expectedTheme, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastError = null;
  while (Date.now() < deadline) {
    try {
      const state = await readWebviewState(port);
      if (state.locale === expectedLocale && state.theme === expectedTheme) return;
      throw new Error(`preferences not applied: observed ${state.locale}/${state.theme}`);
    } catch (error) {
      lastError = error;
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
  }
  throw new Error(`Timed out waiting for WebView preferences: ${lastError?.message || 'unavailable'}`);
}

async function selectWebviewView(port, view) {
  await evaluateWebview(port, `(() => {
    const button = document.querySelector('[data-pl-view="${view}"]');
    if (!button) throw new Error('visual QA view button is unavailable');
    button.click();
    return true;
  })()`);
}

async function assertTemporalReviewEvidence(port, locale) {
  const deadline = Date.now() + 30000;
  let evidence = null;
  let lastError = null;
  while (Date.now() < deadline) {
    try {
      evidence = JSON.parse(await evaluateWebview(port, `(() => {
    const text = document.body.innerText || '';
    const reviewScroll = document.querySelector('[data-pl-page="review"] .pl-page__scroll');
    const temporalRows = document.querySelectorAll('.pl-review__temporal-finding');
    const investigateButtons = document.querySelectorAll('.pl-review__temporal-finding .pl-review__investigate');
    return JSON.stringify({
      comparisonTitle: ${JSON.stringify(locale === 'zh-CN' ? '与对比时段相比的变化' : 'Changes from comparison period')},
      hasComparisonTitle: text.includes(${JSON.stringify(locale === 'zh-CN' ? '与对比时段相比的变化' : 'Changes from comparison period')}),
      hasRateEvidence: text.includes('/h') || text.includes('每小时'),
      hasTemporalProcess: text.includes('alpha.exe') && text.includes('growth.exe'),
      hasLongProcess: text.includes('very-long-observed-process-name-for-temporal-review.exe'),
      hasPhase4AFallback: text.includes('MATCH fallback') || text.includes('MATCH 兜底'),
      temporalRows: temporalRows.length,
      investigateButtons: investigateButtons.length,
      noHorizontalOverflow: !reviewScroll || reviewScroll.scrollWidth <= reviewScroll.clientWidth + 2,
      documentNoHorizontalOverflow: document.documentElement.scrollWidth <= document.documentElement.clientWidth + 2,
    });
      })()`));
      if (isCompleteTemporalEvidence(evidence)) break;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 150));
  }
  if (!isCompleteTemporalEvidence(evidence)) {
    throw new Error(`Temporal Review evidence failed: ${JSON.stringify(evidence)}${lastError ? ` (${lastError.message})` : ''}`);
  }
  console.log(
    `PROXYLENS_VISUAL_QA_TEMPORAL comparison=1 rate=1 process=1 longProcess=1 phase4a=1 rows=${evidence.temporalRows} investigate=${evidence.investigateButtons} overflow=0`,
  );
  return evidence;
}

function isCompleteTemporalEvidence(evidence) {
  return Boolean(evidence?.hasComparisonTitle && evidence.hasRateEvidence && evidence.hasTemporalProcess
    && evidence.hasLongProcess && evidence.hasPhase4AFallback && evidence.temporalRows >= 2
    && evidence.investigateButtons >= 2 && evidence.noHorizontalOverflow
    && evidence.documentNoHorizontalOverflow);
}

async function assertTemporalAccountingIncompleteEvidence(port, locale) {
  const deadline = Date.now() + 30000;
  let evidence = null;
  let lastError = null;
  while (Date.now() < deadline) {
    try {
      evidence = JSON.parse(await evaluateWebview(port, `(() => {
        const text = document.body.innerText || '';
        const reviewScroll = document.querySelector('[data-pl-page="review"] .pl-page__scroll');
        return JSON.stringify({
          comparisonTitle: ${JSON.stringify(locale === 'zh-CN' ? '与对比时段相比的变化' : 'Changes from comparison period')},
          hasComparisonTitle: text.includes(${JSON.stringify(locale === 'zh-CN' ? '与对比时段相比的变化' : 'Changes from comparison period')}),
          hasAccountingIncomplete: text.includes(${JSON.stringify(locale === 'zh-CN'
            ? '近期对比时段内仍有证据尚未完成核算发布'
            : 'accounting has not yet published all evidence in the recent comparison window')}),
          hasPhase4AFallback: text.includes('MATCH fallback') || text.includes('MATCH 兜底'),
          temporalRows: document.querySelectorAll('.pl-review__temporal-finding').length,
          noHorizontalOverflow: !reviewScroll || reviewScroll.scrollWidth <= reviewScroll.clientWidth + 2,
          documentNoHorizontalOverflow: document.documentElement.scrollWidth <= document.documentElement.clientWidth + 2,
        });
      })()`));
      if (evidence.hasComparisonTitle && evidence.hasAccountingIncomplete && evidence.hasPhase4AFallback
        && evidence.temporalRows === 0 && evidence.noHorizontalOverflow && evidence.documentNoHorizontalOverflow) break;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 150));
  }
  if (!evidence?.hasComparisonTitle || !evidence?.hasAccountingIncomplete || !evidence?.hasPhase4AFallback
    || evidence.temporalRows !== 0 || !evidence.noHorizontalOverflow || !evidence.documentNoHorizontalOverflow) {
    throw new Error(`Temporal accounting-incomplete evidence failed: ${JSON.stringify(evidence)}${lastError ? ` (${lastError.message})` : ''}`);
  }
  console.log('PROXYLENS_VISUAL_QA_TEMPORAL_ACCOUNTING_INCOMPLETE unavailable=1 phase4a=1 rows=0 overflow=0');
  return evidence;
}

async function waitForWebviewState(port, requestedSize, expectedLocale, expectedTheme, expectedView, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastError = null;
  while (Date.now() < deadline) {
    try {
      const viewport = await readWebviewState(port);
      if (requestedSize !== 'max') {
        const [expectedWidth, expectedHeight] = requestedSize.split('x').map(Number);
        if (Math.abs(viewport.width - expectedWidth) > 2
          || Math.abs(viewport.height - expectedHeight) > 2) {
          throw new Error(
            `requested viewport ${requestedSize} but observed ${viewport.width}x${viewport.height}`,
          );
        }
      }
      if (viewport.locale !== expectedLocale || viewport.theme !== expectedTheme) {
        throw new Error(`requested preferences ${expectedLocale}/${expectedTheme} but observed ${viewport.locale}/${viewport.theme}`);
      }
      if (viewport.view !== expectedView) {
        throw new Error(`requested view ${expectedView} but observed ${viewport.view}`);
      }
      console.log(
        `PROXYLENS_VISUAL_QA_VIEWPORT requested=${requestedSize} actual=${viewport.width}x${viewport.height} locale=${viewport.locale} theme=${viewport.theme} view=${viewport.view}`,
      );
      return viewport;
    } catch (error) {
      lastError = error;
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
  }
  throw new Error(`Timed out reading WebView viewport: ${lastError?.message || 'unavailable'}`);
}

async function readWebviewState(port) {
  const value = await evaluateWebview(port, 'JSON.stringify({width: innerWidth, height: innerHeight, dpr: devicePixelRatio, locale: document.documentElement.lang, theme: document.documentElement.getAttribute("data-theme"), view: document.querySelector("[data-pl-page]")?.getAttribute("data-pl-page") || document.querySelector("[data-pl-view][aria-current=page]")?.getAttribute("data-pl-view")})');
  return JSON.parse(value);
}

async function evaluateWebview(port, expression) {
  const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
  const page = targets.find((target) => target.type === 'page');
  if (!page?.webSocketDebuggerUrl) throw new Error('WebView page target is unavailable');

  const socket = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    socket.onopen = resolve;
    socket.onerror = () => reject(new Error('WebView CDP connection failed'));
  });

  let nextId = 1;
  const pending = new Map();
  socket.onmessage = (event) => {
    const message = JSON.parse(event.data);
    const resolve = pending.get(message.id);
    if (resolve) {
      pending.delete(message.id);
      resolve(message);
    }
  };
  const call = (method, params = {}) => new Promise((resolve) => {
    const id = nextId++;
    pending.set(id, resolve);
    socket.send(JSON.stringify({ id, method, params }));
  });

  try {
    const response = await call('Runtime.evaluate', {
      expression,
      returnByValue: true,
    });
    if (response.error || response.result?.exceptionDetails) {
      throw new Error('WebView evaluation failed');
    }
    return response.result.result.value;
  } finally {
    socket.close();
  }
}

function terminateProcessTree(pid) {
  if (!pid) return;
  try {
    execFileSync('taskkill', ['/PID', String(pid), '/T', '/F'], { stdio: 'ignore' });
  } catch {
    try { process.kill(pid); } catch {}
  }
}
