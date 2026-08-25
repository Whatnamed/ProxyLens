#!/usr/bin/env node

/**
 * ProxyLens Phase 3A Platform Integration Smoke & Performance Benchmark
 *
 * 验证目标:
 * 1. Go Local Query API 独立启动与标准输出 {"type":"proxylens-query-api-ready",...} 握手
 * 2. 内存单次会话 Bearer Token 鉴权 (正确 200, 错误 401)
 * 3. 严格 Origin CORS 校验 (合法 200, 恶意 403)
 * 4. 只读查询端点全量集成: /healthz, /meta, /summary, /top/processes, /coverage, /connections
 * 5. Sidecar 关闭时独立运行的 Collector 保持健康常驻 (零干扰解耦)
 * 6. 性能度量: Sidecar 启动握手耗时、首个 /meta 与 /summary 延迟
 */

import { spawn, execSync } from 'node:child_process';
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const rootDir = path.resolve(__dirname, '..', '..');
const collectorBinary = path.resolve(rootDir, 'collector', 'collector.exe');
const queryApiBinary = path.resolve(rootDir, 'collector', 'proxylens-query-api.exe');

if (!fs.existsSync(collectorBinary)) {
  execSync('go build -o collector.exe ./cmd/collector', { cwd: path.join(rootDir, 'collector') });
}
if (!fs.existsSync(queryApiBinary)) {
  execSync('go build -o proxylens-query-api.exe ./cmd/proxylens-query-api', { cwd: path.join(rootDir, 'collector') });
}

function httpRequest(options, postData = null) {
  return new Promise((resolve, reject) => {
    const req = http.request(options, (res) => {
      let body = '';
      res.on('data', (d) => { body += d; });
      res.on('end', () => {
        let json = null;
        try { json = JSON.parse(body); } catch {}
        resolve({ status: res.statusCode, headers: res.headers, body, json });
      });
    });
    req.on('error', reject);
    if (postData) req.write(postData);
    req.end();
  });
}

async function main() {
  console.log('================================================================');
  console.log('ProxyLens Phase 3A UI Platform & Query API Integration Smoke');
  console.log('================================================================');

  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'proxylens-smoke-'));
  const dbPath = path.join(tmpDir, 'smoke.db');

  // 1. 先用 Collector 产生真实的权威种子数据
  console.log('[1/5] Initializing synthetic seed database with Collector and Accounting Rebuild...');
  const initProc = spawn(collectorBinary, [
    'run',
    '--controller', 'http://127.0.0.1:9090', // 空/不可达 controller 走冷启动与心跳初始化
    '--db', dbPath,
    '--connections-interval', '250'
  ], { stdio: ['pipe', 'pipe', 'pipe'] });

  await new Promise((r) => setTimeout(r, 1000));
  try {
    initProc.stdin.write('STOP\n');
    initProc.stdin.end();
  } catch {}
  await new Promise((r) => initProc.on('close', r));

  // 执行一次 Accounting Rebuild
  execSync(`"${collectorBinary}" accounting rebuild --db "${dbPath}"`, { stdio: 'pipe' });
  console.log('  ✔ Seed database created and Accounting Rebuild completed.');

  // 2. 启动一个常驻的独立 Collector 进程 (验证解耦)
  console.log('\n[2/5] Spawning independent background Collector instance...');
  const bgCollector = spawn(collectorBinary, [
    'run',
    '--controller', 'http://127.0.0.1:9090',
    '--db', dbPath,
    '--connections-interval', '250'
  ], { stdio: ['pipe', 'pipe', 'pipe'] });

  let bgCollectorRunning = true;
  bgCollector.on('close', () => { bgCollectorRunning = false; });
  await new Promise((r) => setTimeout(r, 500));

  // 3. 启动 Go Query API Sidecar 并进行握手
  console.log('\n[3/5] Spawning Go Query API Sidecar and verifying handshake...');
  const token = 'smoke-secret-test-token-256-bit-entropy-mock';
  const t0Start = Date.now();

  const queryProc = spawn(queryApiBinary, [
    '--db', dbPath,
    '--listen', '127.0.0.1:0',
    '--token', token
  ], { stdio: ['pipe', 'pipe', 'pipe'] });

  let readySignal = null;
  const readyPromise = new Promise((resolve, reject) => {
    queryProc.stdout.on('data', (d) => {
      const lines = d.toString().split('\n');
      for (const line of lines) {
        const trimmed = line.trim();
        if (trimmed.startsWith('{') && trimmed.includes('proxylens-query-api-ready')) {
          try {
            resolve(JSON.parse(trimmed));
            return;
          } catch {}
        }
      }
    });
    setTimeout(() => reject(new Error('Timeout waiting for query API ready signal')), 5000);
  });

  readySignal = await readyPromise;
  const sidecarStartupMs = Date.now() - t0Start;
  console.log(`  ✔ Query API Ready Signal received in ${sidecarStartupMs} ms:`);
  console.log(`    Host: ${readySignal.host}, Port: ${readySignal.port}, API Version: ${readySignal.apiVersion}`);

  const baseUrl = `http://${readySignal.host}:${readySignal.port}`;

  // 4. 发送 API 请求测试鉴权、CORS 与端点响应
  console.log('\n[4/5] Testing Local Query API Endpoints & Security Policy...');

  // 4.1 Healthz 免鉴权
  const healthRes = await httpRequest({
    hostname: readySignal.host,
    port: readySignal.port,
    path: '/healthz',
    method: 'GET'
  });
  if (healthRes.status !== 200 || healthRes.json?.status !== 'ok') {
    throw new Error(`Healthz check failed: ${healthRes.status}`);
  }
  console.log('  ✔ GET /healthz -> 200 OK (Unauthenticated)');

  // 4.2 缺失 Token -> 401
  const unauthRes = await httpRequest({
    hostname: readySignal.host,
    port: readySignal.port,
    path: '/api/v1/meta',
    method: 'GET'
  });
  if (unauthRes.status !== 401 || unauthRes.json?.error?.code !== 'UNAUTHORIZED') {
    throw new Error(`Expected 401 UNAUTHORIZED for missing token, got ${unauthRes.status}`);
  }
  console.log('  ✔ GET /api/v1/meta (Missing Token) -> 401 UNAUTHORIZED (PASS)');

  // 4.3 恶意 Origin -> 403
  const badOriginRes = await httpRequest({
    hostname: readySignal.host,
    port: readySignal.port,
    path: '/api/v1/meta',
    method: 'GET',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Origin': 'http://malicious-website.com'
    }
  });
  if (badOriginRes.status !== 403 || badOriginRes.json?.error?.code !== 'FORBIDDEN_ORIGIN') {
    throw new Error(`Expected 403 FORBIDDEN_ORIGIN for disallowed origin, got ${badOriginRes.status}`);
  }
  console.log('  ✔ GET /api/v1/meta (Disallowed Origin) -> 403 FORBIDDEN_ORIGIN (PASS)');

  // 4.4 合法请求 /meta 与度量延迟
  const t0Meta = Date.now();
  const metaRes = await httpRequest({
    hostname: readySignal.host,
    port: readySignal.port,
    path: '/api/v1/meta',
    method: 'GET',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Origin': 'http://localhost:1420'
    }
  });
  const metaLatencyMs = Date.now() - t0Meta;
  if (metaRes.status !== 200 || metaRes.json?.dbState !== 'READY') {
    throw new Error(`Meta request failed: ${metaRes.status}, body: ${metaRes.body}`);
  }
  console.log(`  ✔ GET /api/v1/meta (Authorized) -> 200 OK (${metaLatencyMs} ms, DBState=${metaRes.json.dbState})`);

  // 4.5 合法请求 /analytics/summary 与度量延迟
  const t0Sum = Date.now();
  const sumRes = await httpRequest({
    hostname: readySignal.host,
    port: readySignal.port,
    path: '/api/v1/analytics/summary',
    method: 'GET',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Origin': 'http://localhost:1420'
    }
  });
  const sumLatencyMs = Date.now() - t0Sum;
  if (sumRes.status !== 200 || !sumRes.json?.accountingVersion) {
    throw new Error(`Summary request failed: ${sumRes.status}`);
  }
  console.log(`  ✔ GET /api/v1/analytics/summary -> 200 OK (${sumLatencyMs} ms, AccountingVersion=${sumRes.json.accountingVersion})`);

  // 4.6 请求 /analytics/top/processes
  const topRes = await httpRequest({
    hostname: readySignal.host,
    port: readySignal.port,
    path: '/api/v1/analytics/top/processes?limit=5',
    method: 'GET',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Origin': 'http://localhost:1420'
    }
  });
  if (topRes.status !== 200 || !Array.isArray(topRes.json?.items)) {
    throw new Error(`Top processes request failed: ${topRes.status}`);
  }
  console.log(`  ✔ GET /api/v1/analytics/top/processes -> 200 OK (Items: ${topRes.json.items.length})`);

  // 4.7 请求 /coverage
  const covRes = await httpRequest({
    hostname: readySignal.host,
    port: readySignal.port,
    path: '/api/v1/coverage',
    method: 'GET',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Origin': 'http://localhost:1420'
    }
  });
  if (covRes.status !== 200 || !Array.isArray(covRes.json?.mergedGaps)) {
    throw new Error(`Coverage request failed: ${covRes.status}`);
  }
  console.log(`  ✔ GET /api/v1/coverage -> 200 OK (MergedGaps: ${covRes.json.mergedGaps.length})`);

  // 4.8 请求 /connections
  const connsRes = await httpRequest({
    hostname: readySignal.host,
    port: readySignal.port,
    path: '/api/v1/connections?limit=5',
    method: 'GET',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Origin': 'http://localhost:1420'
    }
  });
  if (connsRes.status !== 200 || !Array.isArray(connsRes.json?.items)) {
    throw new Error(`Connections request failed: ${connsRes.status}`);
  }
  console.log(`  ✔ GET /api/v1/connections -> 200 OK (Items: ${connsRes.json.items.length}, hasMore=${connsRes.json.hasMore})`);

  // 5. 优雅关闭 Sidecar 并验证独立 Collector 存活
  console.log('\n[5/5] Terminating Query API Sidecar and verifying Collector independence...');
  try {
    queryProc.stdin.write('STOP\n');
    queryProc.stdin.end();
  } catch {}
  await new Promise((r) => queryProc.on('close', r));
  console.log('  ✔ Go Query API Sidecar stopped cleanly.');

  if (!bgCollectorRunning) {
    throw new Error('FATAL: Background Collector died unexpectedly when Query API stopped!');
  }
  console.log('  ✔ Background Collector remains ALIVE and INDEPENDENT (Zero coupling confirmed).');

  // 关闭后台 Collector
  try {
    bgCollector.stdin.write('STOP\n');
    bgCollector.stdin.end();
  } catch {}
  await new Promise((r) => bgCollector.on('close', r));

  try { fs.rmSync(tmpDir, { recursive: true, force: true }); } catch {}

  console.log('\n================================================================');
  console.log('Performance Sanity Metrics:');
  console.log('----------------------------------------------------------------');
  console.log(`Sidecar Handshake Startup Latency : ${sidecarStartupMs} ms`);
  console.log(`First /api/v1/meta Request Latency: ${metaLatencyMs} ms`);
  console.log(`First /analytics/summary Latency  : ${sumLatencyMs} ms`);
  console.log('================================================================');
  console.log('Phase 3A UI Platform Foundation Verified Successfully!');
  console.log('================================================================');
}

main().catch((err) => {
  console.error('\n[FATAL SMOKE ERROR]', err.message);
  process.exit(1);
});
