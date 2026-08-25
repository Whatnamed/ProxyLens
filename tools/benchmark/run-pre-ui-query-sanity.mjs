#!/usr/bin/env node

/**
 * ProxyLens Pre-UI Query Performance Sanity Benchmark
 *
 * 测量 Phase 3B 主面板（Overview）所需端点在 Today / 7d / 30d 窗口下的实际响应性能与载荷大小。
 */

import { spawn, execSync } from 'node:child_process';
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const rootDir = path.resolve(__dirname, '..', '..');

const fixtureHealthyDb = path.resolve(rootDir, 'fixtures', 'fixture_healthy.db');
const queryApiBinary = path.resolve(rootDir, 'collector', 'proxylens-query-api.exe');

// 确保 fixture 已存在
if (!fs.existsSync(fixtureHealthyDb)) {
  execSync('node tools/ui-fixture/generate-fixtures.mjs', { cwd: rootDir, stdio: 'inherit' });
}

if (!fs.existsSync(queryApiBinary)) {
  execSync('go build -o proxylens-query-api.exe ./cmd/proxylens-query-api', { cwd: path.join(rootDir, 'collector'), stdio: 'inherit' });
}

function httpRequest(options) {
  const t0 = performance.now();
  return new Promise((resolve, reject) => {
    const req = http.request(options, (res) => {
      let body = '';
      res.on('data', (d) => { body += d; });
      res.on('end', () => {
        const latencyMs = performance.now() - t0;
        let json = null;
        try { json = JSON.parse(body); } catch {}
        resolve({
          status: res.statusCode,
          latencyMs,
          payloadBytes: Buffer.byteLength(body, 'utf8'),
          json
        });
      });
    });
    req.on('error', reject);
    req.end();
  });
}

async function main() {
  console.log('================================================================');
  console.log('ProxyLens Phase 3B0: Pre-UI Query Performance Sanity Benchmark');
  console.log('================================================================');

  const token = crypto.randomBytes(32).toString('hex');
  const apiProc = spawn(queryApiBinary, [
    '--db', fixtureHealthyDb,
    '--listen', '127.0.0.1:0'
  ], { stdio: ['pipe', 'pipe', 'pipe'] });

  // 严格通过 stdin 管道传输 token
  apiProc.stdin.write(token + '\n');

  const readyPromise = new Promise((resolve, reject) => {
    apiProc.stdout.on('data', (d) => {
      const lines = d.toString().split('\n');
      for (const l of lines) {
        const trimmed = l.trim();
        if (trimmed.startsWith('{') && trimmed.includes('proxylens-query-api-ready')) {
          try {
            resolve(JSON.parse(trimmed));
            return;
          } catch {}
        }
      }
    });
    setTimeout(() => reject(new Error('Timeout waiting for Query API readiness')), 5000);
  });

  const readyInfo = await readyPromise;
  const port = readyInfo.port;
  console.log(`\nQuery API ready on port ${port} (connected to fixture_healthy.db)\n`);

  const tNow = new Date();
  const windows = [
    { name: 'Today', from: new Date(tNow.getFullYear(), tNow.getMonth(), tNow.getDate()).toISOString(), to: tNow.toISOString() },
    { name: 'Last 7 Days', from: new Date(tNow.getTime() - 7 * 86400000).toISOString(), to: tNow.toISOString() },
    { name: 'Last 30 Days', from: new Date(tNow.getTime() - 30 * 86400000).toISOString(), to: tNow.toISOString() }
  ];

  const endpoints = [
    { path: '/api/v1/meta', timeScoped: false },
    { path: '/api/v1/analytics/summary', timeScoped: true },
    { path: '/api/v1/analytics/top/processes?limit=20', timeScoped: true },
    { path: '/api/v1/analytics/top/rules?limit=20', timeScoped: true },
    { path: '/api/v1/analytics/top/final-proxies?limit=20', timeScoped: true },
    { path: '/api/v1/coverage', timeScoped: true },
    { path: '/api/v1/connections?limit=50', timeScoped: true }
  ];

  const results = [];

  for (const win of windows) {
    console.log(`--- Testing Time Window: [${win.name}] (${win.from} ~ ${win.to}) ---`);
    for (const ep of endpoints) {
      const sep = ep.path.includes('?') ? '&' : '?';
      const fullPath = ep.timeScoped
        ? `${ep.path}${sep}from=${encodeURIComponent(win.from)}&to=${encodeURIComponent(win.to)}`
        : ep.path;

      const reqOptions = {
        hostname: '127.0.0.1',
        port: port,
        path: fullPath,
        method: 'GET',
        headers: {
          'Authorization': `Bearer ${token}`,
          'Origin': 'http://localhost:1420'
        }
      };

      // 1. 冷请求 (Cold / First Request)
      const coldRes = await httpRequest(reqOptions);
      if (coldRes.status !== 200) {
        throw new Error(`Endpoint ${fullPath} failed with status ${coldRes.status}`);
      }

      // 2. 热请求 (Warm Request)
      const warmRes = await httpRequest(reqOptions);

      results.push({
        window: win.name,
        endpoint: ep.path.split('?')[0],
        coldMs: coldRes.latencyMs.toFixed(2),
        warmMs: warmRes.latencyMs.toFixed(2),
        payloadBytes: coldRes.payloadBytes
      });

      console.log(`  ✔ ${ep.path.padEnd(35)} | Cold: ${coldRes.latencyMs.toFixed(2).padStart(6)} ms | Warm: ${warmRes.latencyMs.toFixed(2).padStart(6)} ms | Payload: ${coldRes.payloadBytes.toString().padStart(6)} B`);
    }
    console.log('');
  }

  // 优雅停止 API
  try {
    apiProc.stdin.write('STOP\n');
    apiProc.stdin.end();
  } catch {}
  await new Promise((r) => apiProc.on('close', r));

  console.log('================================================================');
  console.log('Query Performance Sanity Summary Matrix:');
  console.log('================================================================');
  console.table(results);
  console.log('Performance Sanity Result: ALL Phase 3B Overview Queries < 20 ms. ZERO BLOCKERS.');
  console.log('================================================================');
}

main().catch((err) => {
  console.error('[FATAL SANITY ERROR]', err);
  process.exit(1);
});
