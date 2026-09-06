#!/usr/bin/env node

/**
 * ProxyLens UI Fixture Generator Runner
 *
 * 供前端开发者一键生成 5 种确定性合成测试数据库：
 * - healthy : 包含 PROXY/DIRECT/TCP/UDP/多规则/多节点/节点切换等典型日常数据
 * - gaps    : 包含 controller_stream 与 collector_session_boundary 缺口
 * - stale   : 包含最新核算 Run 但追加了新未核算事件 (freshness lag > 0)
 * - empty   : 有效 Schema 但无流量记录
 * - scaled  : 100,000 events / 5,000 distinct connections / 30 days
 * - review  : deterministic candidates for all four Phase 4A detector families
 * - review-temporal : Phase 4B1 comparison fixture plus Phase 4A candidates
 */

import { execFileSync } from 'node:child_process';
import path from 'node:path';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const rootDir = path.resolve(__dirname, '..', '..');

const fixtureDir = path.resolve(rootDir, 'fixtures');
if (!fs.existsSync(fixtureDir)) {
  fs.mkdirSync(fixtureDir, { recursive: true });
}

const profiles = ['healthy', 'gaps', 'stale', 'empty', 'scaled', 'review', 'review-temporal'];

console.log('================================================================');
console.log('Generating Deterministic Synthetic UI Fixtures');
console.log('================================================================');

const anchorArg = process.argv.find((a) => a.startsWith('--anchor=') || a === '--anchor');
const explicitAnchor = anchorArg
  ? (anchorArg.includes('=') ? anchorArg.split('=').slice(1).join('=') : process.argv[process.argv.indexOf(anchorArg) + 1])
  : undefined;
const anchor = explicitAnchor || new Date().toISOString();
const scaleArg = process.argv.find((a) => a.startsWith('--scale=') || a === '--scale');
const scale = scaleArg
  ? (scaleArg.includes('=') ? scaleArg.split('=').slice(1).join('=') : process.argv[process.argv.indexOf(scaleArg) + 1])
  : '100000';

const metadataPath = path.resolve(rootDir, 'tmp', 'ui-fixtures-metadata.json');
fs.mkdirSync(path.dirname(metadataPath), { recursive: true });

console.log(`Anchor UTC: ${anchor}`);

for (const p of profiles) {
  const targetDb = path.join(fixtureDir, `fixture_${p}.db`);
  console.log(`\nGenerating profile [${p}] -> ${targetDb}...`);
  const args = ['run', './cmd/proxylens-ui-fixture', '--profile', p, '--out', targetDb, '--anchor', anchor];
  if (p === 'scaled') args.push('--scale', scale);
  execFileSync('go', args, {
    cwd: path.join(rootDir, 'collector'),
    stdio: 'inherit'
  });
}

fs.writeFileSync(metadataPath, `${JSON.stringify({
  anchor,
  profiles,
  scaledEvents: Number(scale),
  generatedAt: new Date().toISOString(),
}, null, 2)}\n`, 'utf8');

console.log('\n================================================================');
console.log('All synthetic fixtures generated successfully in ./fixtures/');
console.log(`Shared anchor: ${anchor}`);
console.log(`Fixture metadata: ${path.relative(rootDir, metadataPath)}`);
console.log('Run Tauri with a fixture:');
console.log('  $env:PROXYLENS_DB_PATH="fixtures/fixture_healthy.db"; npm run tauri:dev');
console.log('================================================================');
