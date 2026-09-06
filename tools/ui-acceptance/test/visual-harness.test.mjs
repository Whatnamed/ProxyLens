import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import {
  classifyVisualQaGate,
  settingsCommandAllowedInVisualQa,
  validateVisualQaRunnerInputs,
  VisualQaGateState,
} from '../visual-qa-policy.mjs';

const testDir = path.dirname(fileURLToPath(import.meta.url));
const rootDir = path.resolve(testDir, '..', '..', '..');
const libSource = fs.readFileSync(path.join(rootDir, 'ui', 'src-tauri', 'src', 'lib.rs'), 'utf8');
const sidecarSource = fs.readFileSync(path.join(rootDir, 'ui', 'src-tauri', 'src', 'sidecar.rs'), 'utf8');
const runnerSource = fs.readFileSync(path.join(rootDir, 'tools', 'ui-acceptance', 'run-tauri-visual.mjs'), 'utf8');
const fixtureRunnerSource = fs.readFileSync(path.join(rootDir, 'tools', 'ui-fixture', 'generate-fixtures.mjs'), 'utf8');

test('visual QA requires both explicit gates and never falls through to production bootstrap', () => {
  assert.match(sidecarSource, /PROXYLENS_E2E_MODE/);
  assert.match(sidecarSource, /PROXYLENS_VISUAL_QA_QUERY_ONLY/);
  assert.match(sidecarSource, /visual_qa_gate\(\)/);
  assert.match(libSource, /does not call bootstrap_supervisor/);
  assert.match(libSource, /owner=0 runtime=0 controller=0/);
  assert.match(libSource, /RefusedIncomplete/);
});

test('visual QA path requires an explicit fixture DB and rejects authority env', () => {
  assert.match(sidecarSource, /PROXYLENS_DB_PATH must explicitly point to an existing fixture DB/);
  assert.match(sidecarSource, /canonical production database is not allowed/);
  assert.match(sidecarSource, /PROXYLENS_CONTROLLER_URL/);
  assert.match(sidecarSource, /MIHOMO_SECRET/);
  assert.match(runnerSource, /PROXYLENS_CONTROLLER_URL/);
  assert.match(runnerSource, /MIHOMO_SECRET/);
  assert.match(runnerSource, /PROXYLENS_DB_PATH = copyDb/);
});

test('visual runner only launches a query-only Tauri process and verifies read-only evidence', () => {
  assert.match(runnerSource, /PROXYLENS_VISUAL_QA_QUERY_ONLY = '1'/);
  assert.match(runnerSource, /PROXYLENS_VISUAL_QA_READY/);
  assert.match(runnerSource, /sourceShaAfter !== sourceShaBefore/);
  assert.match(runnerSource, /copyShaAfter !== copyShaBefore/);
  assert.match(runnerSource, /PROXYLENS_E2E_TASK_NAME/);
  assert.match(runnerSource, /PROXYLENS_E2E_DIRECT_OWNER/);
  assert.match(runnerSource, /fixtureMetadata/);
  assert.match(runnerSource, /anchor: fixtureMetadata\?\.anchor/);
  assert.match(runnerSource, /requestedSize: size/);
  assert.match(runnerSource, /actualViewport/);
  assert.match(runnerSource, /actualLocale/);
  assert.match(runnerSource, /actualTheme/);
  assert.match(runnerSource, /view=\$\{view\}/);
});

test('fixture runner generates all six profiles from one recorded anchor', () => {
  assert.match(fixtureRunnerSource, /\['healthy', 'gaps', 'stale', 'empty', 'scaled', 'review'\]/);
  assert.match(fixtureRunnerSource, /const anchor = explicitAnchor \|\| new Date\(\)\.toISOString\(\)/);
  assert.match(fixtureRunnerSource, /--anchor', anchor/);
  assert.match(fixtureRunnerSource, /ui-fixtures-metadata\.json/);
  assert.match(fixtureRunnerSource, /scaledEvents: Number\(scale\)/);
});

test('visual QA policy preserves normal/E2E modes and refuses incomplete requests', () => {
  assert.equal(
    classifyVisualQaGate({ e2eMode: undefined, visualQaFlag: undefined }),
    VisualQaGateState.disabled,
  );
  assert.equal(
    classifyVisualQaGate({ e2eMode: '1', visualQaFlag: undefined }),
    VisualQaGateState.disabled,
  );
  assert.equal(
    classifyVisualQaGate({ e2eMode: '1', visualQaFlag: '1' }),
    VisualQaGateState.active,
  );
  assert.equal(
    classifyVisualQaGate({ e2eMode: undefined, visualQaFlag: '1' }),
    VisualQaGateState.refusedIncomplete,
  );
  assert.equal(
    classifyVisualQaGate({ e2eMode: '1', visualQaFlag: '0' }),
    VisualQaGateState.refusedIncomplete,
  );
  assert.equal(settingsCommandAllowedInVisualQa(VisualQaGateState.disabled), true);
  assert.equal(settingsCommandAllowedInVisualQa(VisualQaGateState.active), false);
  assert.equal(settingsCommandAllowedInVisualQa(VisualQaGateState.refusedIncomplete), false);
});

test('visual QA policy refuses unsafe fixture and authority inputs', () => {
  const safe = {
    e2eMode: '1',
    visualQaFlag: '1',
    explicitDbPath: path.resolve(rootDir, 'tmp', 'fixture-copy.db'),
    fixtureExists: true,
    canonicalProductionDbPath: path.resolve(rootDir, 'tmp', 'production.db'),
    controllerUrl: undefined,
    mihomoSecret: undefined,
  };
  assert.doesNotThrow(() => validateVisualQaRunnerInputs(safe));
  assert.throws(
    () => validateVisualQaRunnerInputs({ ...safe, e2eMode: undefined }),
    /incomplete visual QA gate/,
  );
  assert.throws(
    () => validateVisualQaRunnerInputs({ ...safe, explicitDbPath: 'fixture.db' }),
    /absolute/,
  );
  assert.throws(
    () => validateVisualQaRunnerInputs({ ...safe, fixtureExists: false }),
    /does not exist/,
  );
  assert.throws(
    () => validateVisualQaRunnerInputs({
      ...safe,
      canonicalProductionDbPath: safe.explicitDbPath,
    }),
    /canonical production database/,
  );
  assert.throws(
    () => validateVisualQaRunnerInputs({ ...safe, controllerUrl: 'http://127.0.0.1:43127' }),
    /Controller authority/,
  );
  assert.throws(
    () => validateVisualQaRunnerInputs({ ...safe, mihomoSecret: 'synthetic-secret' }),
    /Secret authority/,
  );
});
