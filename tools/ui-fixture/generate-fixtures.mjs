#!/usr/bin/env node

/**
 * ProxyLens UI Fixture Generator Runner
 *
 * 供前端开发者一键生成 4 种确定性合成测试数据库：
 * - healthy : 包含 PROXY/DIRECT/TCP/UDP/多规则/多节点/节点切换等典型日常数据
 * - gaps    : 包含 controller_stream 与 collector_session_boundary 缺口
 * - stale   : 包含最新核算 Run 但追加了新未核算事件 (freshness lag > 0)
 * - empty   : 有效 Schema 但无流量记录
 */

import { execSync } from 'node:child_process';
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

const profiles = ['healthy', 'gaps', 'stale', 'empty'];

console.log('================================================================');
console.log('Generating Deterministic Synthetic UI Fixtures');
console.log('================================================================');

const anchorArg = process.argv.find((a) => a.startsWith('--anchor=') || a === '--anchor');
let anchorFlag = '';
if (anchorArg) {
  const val = anchorArg.includes('=') ? anchorArg.split('=')[1] : process.argv[process.argv.indexOf(anchorArg) + 1];
  if (val) anchorFlag = ` --anchor "${val}"`;
}

for (const p of profiles) {
  const targetDb = path.join(fixtureDir, `fixture_${p}.db`);
  console.log(`\nGenerating profile [${p}] -> ${targetDb}...`);
  execSync(`go run ./cmd/proxylens-ui-fixture --profile "${p}" --out "${targetDb}"${anchorFlag}`, {
    cwd: path.join(rootDir, 'collector'),
    stdio: 'inherit'
  });
}

console.log('\n================================================================');
console.log('All synthetic fixtures generated successfully in ./fixtures/');
console.log('Run Tauri with a fixture:');
console.log('  $env:PROXYLENS_DB_PATH="fixtures/fixture_healthy.db"; npm run tauri:dev');
console.log('================================================================');
