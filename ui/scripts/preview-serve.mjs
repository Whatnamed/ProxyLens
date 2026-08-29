#!/usr/bin/node

/**
 * ProxyLens Fixture Preview Server
 *
 * 一键启动「浏览器预览」形态的 UI + Go Query API 组合，供冻结工作树快速开服务预览。
 * 端口与来源约定均来自已验证的约束，请勿随意更换：
 * - UI 固定 5173：Query API 的 CORS 白名单只放行 5173/1420 两个开发端口
 *   （collector/pkg/api/auth.go defaultAllowedOrigins），1420 预留给 Tauri dev。
 * - API 默认 49153：避免与默认 49152 或其他工作树正在运行的实例冲突。
 * - Query API 的会话 token 通过 stdin 首行传入，且 stdin 关闭即视为父进程死亡而退出，
 *   因此必须保持 stdin 管道打开（本脚本持有子进程管道，不主动关闭）。
 *
 * 用法（在 ui/ 目录）:
 *   npm run preview:serve
 *   npm run preview:serve -- --db ../fixtures/fixture_gaps.db
 *   npm run preview:serve -- --api-port 49154 --ui-port 5173
 *
 * 浏览器访问 http://127.0.0.1:5173 （或 http://localhost:5173）。
 * Ctrl+C 退出时会同时结束两个子进程树；请勿用强杀方式结束本进程，
 * 否则子进程可能残留（可用 netstat + taskkill 清理）。
 */

import { spawn } from 'node:child_process';
import http from 'node:http';
import path from 'node:path';
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const uiRoot = path.resolve(__dirname, '..');
const repoRoot = path.resolve(uiRoot, '..');

const DEV_TOKEN = 'dev-local-session-token';

function parseArg(name, fallback) {
  const prefix = `--${name}=`;
  const inline = process.argv.find((a) => a.startsWith(prefix));
  if (inline) return inline.slice(prefix.length);
  const idx = process.argv.indexOf(`--${name}`);
  if (idx !== -1 && process.argv[idx + 1] && !process.argv[idx + 1].startsWith('--')) {
    return process.argv[idx + 1];
  }
  return fallback;
}

const apiPort = parseArg('api-port', '49153');
const uiPort = parseArg('ui-port', '5173');
const dbArg = parseArg('db', path.join(repoRoot, 'fixtures', 'fixture_healthy.db'));
const dbPath = path.resolve(dbArg);

const sidecarPath = path.join(
  uiRoot,
  'src-tauri',
  'binaries',
  'proxylens-query-api-x86_64-pc-windows-msvc.exe'
);
const viteJsPath = path.join(uiRoot, 'node_modules', 'vite', 'bin', 'vite.js');

if (!fs.existsSync(dbPath)) {
  console.error(`[preview] 数据库不存在: ${dbPath}`);
  console.error('[preview] 可先运行 tools/ui-fixture/generate-fixtures.mjs 生成合成 fixture。');
  process.exit(1);
}
if (!fs.existsSync(sidecarPath)) {
  console.error('[preview] Query API sidecar 不存在，请先执行: npm run sidecar:build');
  process.exit(1);
}
if (!fs.existsSync(viteJsPath)) {
  console.error('[preview] 未找到 vite，请先执行: npm install');
  process.exit(1);
}

function treeKill(pid) {
  if (process.platform === 'win32') {
    spawn('taskkill', ['/pid', String(pid), '/T', '/F'], { stdio: 'ignore' });
  } else {
    process.kill(-pid, 'SIGTERM');
  }
}

const children = [];

console.log('================================================================');
console.log('ProxyLens Fixture Preview');
console.log('================================================================');
console.log(`[preview] db      : ${dbPath}`);
console.log(`[preview] api     : http://127.0.0.1:${apiPort}`);
console.log(`[preview] ui      : http://127.0.0.1:${uiPort}`);

// 1. Query API：stdin 首行传 token，管道保持打开
const api = spawn(sidecarPath, ['--db', dbPath, '--listen', `127.0.0.1:${apiPort}`], {
  cwd: uiRoot,
  stdio: ['pipe', 'pipe', 'pipe'],
});
children.push(api);
api.stdin.write(`${DEV_TOKEN}\n`);
api.stdout.on('data', (d) => process.stdout.write(`[api] ${d}`));
api.stderr.on('data', (d) => process.stderr.write(`[api] ${d}`));
api.on('exit', (code) => console.log(`[preview] query api exited (code=${code})`));

// 2. Vite dev server：直连 node 进程，避免 npm 包装层留下孤儿
const vite = spawn(
  process.execPath,
  [viteJsPath, '--port', uiPort, '--strictPort'],
  {
    cwd: uiRoot,
    env: { ...process.env, VITE_PROXYLENS_API_PORT: apiPort },
    stdio: ['ignore', 'pipe', 'pipe'],
  }
);
children.push(vite);
vite.stdout.on('data', (d) => process.stdout.write(`[vite] ${d}`));
vite.stderr.on('data', (d) => process.stderr.write(`[vite] ${d}`));
vite.on('exit', (code) => console.log(`[preview] vite exited (code=${code})`));

function shutdown() {
  console.log('\n[preview] shutting down...');
  for (const c of children) {
    if (c.exitCode === null && !c.killed) treeKill(c.pid);
  }
  setTimeout(() => process.exit(0), 500);
}
process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
process.on('SIGBREAK', shutdown);

// 3. /healthz 免鉴权，就绪后提示访问地址
function waitForApi(retries = 30) {
  const req = http.get({ host: '127.0.0.1', port: apiPort, path: '/healthz', timeout: 1000 }, (res) => {
    res.resume();
    if (res.statusCode === 200) {
      console.log('================================================================');
      console.log(`[preview] ready -> 打开 http://127.0.0.1:${uiPort} 查看完整 UI`);
      console.log('================================================================');
    } else {
      retry();
    }
  });
  req.on('error', retry);
  req.on('timeout', () => {
    req.destroy();
    retry();
  });
  let n = 0;
  function retry() {
    if (++n < retries) setTimeout(waitForApi, 500);
    else console.error('[preview] query api 未在预期时间内就绪，请检查上方日志。');
  }
}
setTimeout(waitForApi, 800);
