#!/usr/bin/env node

/**
 * ProxyLens Phase 0 — Automated Capture-Rate Matrix Runner
 * 
 * 自动化执行 3 个采样间隔 x 2 种路由类型 x 2 次重复 = 12 个 Trial 的 Capture-Rate 矩阵测试。
 */

import { spawn } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

const intervals = [
  { name: 'default', ms: null, probeDuration: 15 },
  { name: '500', ms: 500, probeDuration: 15 },
  { name: '250', ms: 250, probeDuration: 15 },
];

const routes = [
  { name: 'direct', url: 'https://www.baidu.com' },
  { name: 'proxy', url: 'https://raw.githubusercontent.com/MetaCubeX/metacubexd/main/README.md' },
];

const repeats = [1, 2];

function runProcess(cmd, args) {
  return new Promise((resolve, reject) => {
    const p = spawn(cmd, args, { stdio: 'inherit' });
    p.on('error', reject);
    p.on('close', (code) => {
      if (code === 0) resolve();
      else reject(new Error(`${cmd} ${args.join(' ')} failed with code ${code}`));
    });
  });
}

function runCaptureAnalyzer(sessionDir, gtFile, expectedRoute) {
  return new Promise((resolve, reject) => {
    const args = ['tools/discovery/analyze-capture-rate.mjs', sessionDir, gtFile, '--json'];
    if (expectedRoute) {
      args.push('--expected-route', expectedRoute);
    }
    const p = spawn('node', args, {
      stdio: ['ignore', 'pipe', 'inherit'],
    });
    let out = '';
    p.stdout.on('data', (d) => (out += d));
    p.on('error', reject);
    p.on('close', (code) => {
      if (code === 0) {
        try {
          resolve(JSON.parse(out.trim()));
        } catch (e) {
          reject(e);
        }
      } else {
        reject(new Error(`analyze-capture-rate failed with code ${code}`));
      }
    });
  });
}

async function main() {
  const baseOutDir = path.resolve('tmp/discovery/work-package-a/capture');
  if (!fs.existsSync(baseOutDir)) fs.mkdirSync(baseOutDir, { recursive: true });

  console.log('================================================================');
  console.log('STARTING PHASE 0C-3C CAPTURE-RATE MATRIX EXPERIMENT');
  console.log('Total Trials: 3 intervals x 2 routes x 2 repeats = 12 trials');
  console.log('================================================================\n');

  const allResults = [];

  for (const intv of intervals) {
    for (const route of routes) {
      for (const rep of repeats) {
        const trialName = `${route.name}-${intv.name}-r${rep}`;
        const ts = new Date().toISOString().replace(/[:.]/g, '-');
        const sessionDir = path.join(baseOutDir, `${trialName}-${ts}`);
        const gtFile = path.join(sessionDir, 'ground-truth.ndjson');
        fs.mkdirSync(sessionDir, { recursive: true });

        console.log(`\n>>> [TRIAL START] ${trialName} (Interval: ${intv.name}, Route: ${route.name}, Rep: ${rep})`);

        // 1. 启动 Probe 后台执行 (60s 上限保护，通过 stdin 发送 STOP 优雅退出)
        const probeArgs = [
          'tools/discovery/probe.mjs',
          '--controller', 'http://127.0.0.1:9090',
          '--output', sessionDir,
          '--duration', '60',
        ];
        if (intv.ms) {
          probeArgs.push('--connections-interval', String(intv.ms));
        }

        const probeProcess = spawn('node', probeArgs, { stdio: ['pipe', 'inherit', 'inherit'] });
        const probePromise = new Promise((resolve, reject) => {
          probeProcess.on('error', reject);
          probeProcess.on('close', (code) => {
            if (code === 0) resolve();
            else reject(new Error(`Probe exited with code ${code}`));
          });
        });

        // 2. 等待 2 秒进入稳定监控
        await new Promise((r) => setTimeout(r, 2000));

        // 3. 执行 50 个短请求 burst
        console.log(`[BURST] Launching 50 requests to ${route.url}...`);
        const burstArgs = [
          'tools/discovery/scenarios/short-request-burst.mjs',
          '--url', route.url,
          '--count', '50',
          '--spacing-ms', '80',
          '--output', gtFile,
        ];

        await runProcess('node', burstArgs);

        // 4. Burst 全部完成后，等待 2.5 秒采样缓冲，然后通过 stdin 发送 STOP 通知 Probe 优雅退出
        console.log('[PROBE] Burst finished, waiting 2.5s buffer before graceful probe shutdown...');
        await new Promise((r) => setTimeout(r, 2500));
        probeProcess.stdin.write('STOP\n');

        await probePromise;

        // 5. 检查 manifest 健康
        const manifestPath = path.join(sessionDir, 'manifest.json');
        const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
        if (!manifest.evidenceQuality?.isHealthySession) {
          console.error(`[ERROR] Unhealthy trial detected in ${trialName}:`, manifest.evidenceQuality?.issues);
          process.exit(1);
        }

        // 6. 执行分析并检查全量 Integrity Gate
        const res = await runCaptureAnalyzer(sessionDir, gtFile, route.name);
        if (res.windowViolations > 0) {
          console.error(`[FATAL] Window integrity violation in ${trialName}: ${res.windowViolations} requests outside probe window!`);
          process.exit(1);
        }
        if (res.oneToOneConflicts > 0) {
          console.error(`[FATAL] 1-to-1 conflict in ${trialName}: ${res.oneToOneConflicts} conflicts!`);
          process.exit(1);
        }
        if (res.ambiguous > 0) {
          console.error(`[FATAL] Ambiguous matches in ${trialName}: ${res.ambiguous} ambiguous!`);
          process.exit(1);
        }
        if (res.routeVerification.routeMismatches > 0 || res.routeVerification.routeUnknown > 0) {
          console.error(`[FATAL] Route verification failed in ${trialName}: mismatches=${res.routeVerification.routeMismatches}, unknown=${res.routeVerification.routeUnknown}!`);
          process.exit(1);
        }

        res.trial = trialName;
        res.intervalName = intv.name;
        res.routeName = route.name;
        res.repeat = rep;
        res.sessionDir = sessionDir;
        allResults.push(res);

        const rateStr = ((res.matched / res.eligibleConnectedCount) * 100).toFixed(1) + '%';
        console.log(`<<< [TRIAL DONE] ${trialName} -> Matched: ${res.matched}/${res.eligibleConnectedCount} (${rateStr}), Missed: ${res.missed}, Ambiguous: ${res.ambiguous}, Violations: ${res.windowViolations}`);

        await new Promise((r) => setTimeout(r, 1000));
      }
    }
  }

  const summaryFile = path.join(baseOutDir, 'matrix-summary.json');
  fs.writeFileSync(summaryFile, JSON.stringify(allResults, null, 2), 'utf8');

  console.log('\n================================================================');
  console.log('ALL 12 CAPTURE-RATE MATRIX TRIALS COMPLETED!');
  console.log(`Summary saved to: ${summaryFile}`);
  console.log('================================================================\n');
}

main().catch((err) => {
  console.error('[FATAL]', err);
  process.exit(1);
});
