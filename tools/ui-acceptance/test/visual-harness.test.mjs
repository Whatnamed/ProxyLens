import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const testDir = path.dirname(fileURLToPath(import.meta.url));
const rootDir = path.resolve(testDir, '..', '..', '..');
const libSource = fs.readFileSync(path.join(rootDir, 'ui', 'src-tauri', 'src', 'lib.rs'), 'utf8');
const sidecarSource = fs.readFileSync(path.join(rootDir, 'ui', 'src-tauri', 'src', 'sidecar.rs'), 'utf8');
const runnerSource = fs.readFileSync(path.join(rootDir, 'tools', 'ui-acceptance', 'run-tauri-visual.mjs'), 'utf8');
const fixtureRunnerSource = fs.readFileSync(path.join(rootDir, 'tools', 'ui-fixture', 'generate-fixtures.mjs'), 'utf8');

test('visual QA requires both explicit gates and never falls through to production bootstrap', () => {
  assert.match(sidecarSource, /PROXYLENS_E2E_MODE/);
  assert.match(sidecarSource, /PROXYLENS_VISUAL_QA_QUERY_ONLY/);
  assert.match(libSource, /visual_qa_query_only_enabled\(\)/);
  assert.match(libSource, /does not call bootstrap_supervisor/);
  assert.match(libSource, /owner=0 runtime=0 controller=0/);
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
});

test('fixture runner generates all five profiles from one recorded anchor', () => {
  assert.match(fixtureRunnerSource, /\['healthy', 'gaps', 'stale', 'empty', 'scaled'\]/);
  assert.match(fixtureRunnerSource, /const anchor = explicitAnchor \|\| new Date\(\)\.toISOString\(\)/);
  assert.match(fixtureRunnerSource, /--anchor', anchor/);
  assert.match(fixtureRunnerSource, /ui-fixtures-metadata\.json/);
  assert.match(fixtureRunnerSource, /scaledEvents: Number\(scale\)/);
});
