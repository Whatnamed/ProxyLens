/**
 * run-config-patch-experiment.mjs
 * 
 * Stage R4: Basic Configuration Runtime PATCH Experiment
 * 
 * 安全机制:
 * 1. 默认仅进行 Preflight / Dry-run，绝不直接向 Controller 发起 PATCH
 * 2. 必须显式传入 --allow-live-mutation 选项才允许发起实际网络变更
 * 3. 支持参数化 --controller
 * 
 * 官方 API 说明 [Documented]:
 * - PATCH /configs             -> Update basic configuration
 * - PUT /configs?force=true    -> Reload basic configuration (Default: DO NOT CALL ON LIVE FLCLASH)
 * - POST /restart              -> Restart kernel (Default: DO NOT CALL ON LIVE FLCLASH)
 * 
 * 用法:
 *   node tools/discovery/scenarios/run-config-patch-experiment.mjs [--controller <url>] [--allow-live-mutation]
 */

import http from 'http';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '../../..');

function parseArgs() {
  const args = process.argv.slice(2);
  const options = {
    controllerUrl: 'http://127.0.0.1:9090',
    allowLiveMutation: false
  };

  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--controller' && args[i + 1]) options.controllerUrl = args[++i];
    if (args[i] === '--allow-live-mutation') options.allowLiveMutation = true;
  }

  return options;
}

function fetchJson(url) {
  return new Promise((resolve, reject) => {
    http.get(url, (res) => {
      let data = '';
      res.on('data', d => data += d);
      res.on('end', () => {
        try { resolve(JSON.parse(data)); } catch (e) { reject(e); }
      });
    }).on('error', reject);
  });
}

function patchConfig(controllerUrl, payload) {
  return new Promise((resolve, reject) => {
    const url = new URL('/configs', controllerUrl);
    const body = JSON.stringify(payload);
    const req = http.request(url.toString(), {
      method: 'PATCH',
      headers: {
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(body)
      }
    }, (res) => {
      let data = '';
      res.on('data', d => data += d);
      res.on('end', () => {
        if (res.statusCode === 204 || res.statusCode === 200) {
          resolve(true);
        } else {
          reject(new Error(`PATCH failed: HTTP ${res.statusCode} ${data}`));
        }
      });
    });
    req.on('error', reject);
    req.write(body);
    req.end();
  });
}

async function main() {
  const { controllerUrl, allowLiveMutation } = parseArgs();

  console.log('================================================================');
  console.log('STAGE R4: BASIC CONFIGURATION RUNTIME PATCH EXPERIMENT');
  console.log('================================================================');
  console.log(`Controller URL    : ${controllerUrl}`);
  console.log(`Live Mutation     : ${allowLiveMutation ? 'ENABLED (LIVE MUTATION ALLOWED)' : 'DISABLED (SAFE DRY-RUN ONLY)'}`);

  // 1. 获取当前状态
  console.log('\n[STEP 1] Fetching pre-patch connections & global counters...');
  const preConns = await fetchJson(`${controllerUrl}/connections`);
  const preConfigs = await fetchJson(`${controllerUrl}/configs`);

  const preUploadTotal = preConns.uploadTotal;
  const preDownloadTotal = preConns.downloadTotal;
  const preConnIds = (preConns.connections || []).map(c => c.id);

  console.log(`Pre-patch Counters  : Up ${preUploadTotal.toLocaleString()} B, Down ${preDownloadTotal.toLocaleString()} B`);
  console.log(`Pre-patch Conns     : ${preConnIds.length} active connections`);
  console.log(`Current Mode        : ${preConfigs.mode || 'rule'}`);

  if (!allowLiveMutation) {
    console.log('\n[DRY-RUN / PREFLIGHT NOTICE]');
    console.log(`  --allow-live-mutation was NOT specified.`);
    console.log(`  Dry-run preflight check passed. Controller is responsive at ${controllerUrl}.`);
    console.log(`  Skipping live PATCH mutation. Exiting safely.\n`);
    return;
  }

  // 2. 触发配置更新 (PATCH /configs)
  console.log('\n[STEP 2] Triggering config update via PATCH /configs...');
  await patchConfig(controllerUrl, { mode: preConfigs.mode || 'rule' });
  console.log('[PATCH APPLIED] Waiting 2 seconds for kernel state update...');
  await new Promise(r => setTimeout(r, 2000));

  // 3. 获取更新后状态
  console.log('\n[STEP 3] Fetching post-patch connections & global counters...');
  const postConns = await fetchJson(`${controllerUrl}/connections`);
  const postUploadTotal = postConns.uploadTotal;
  const postDownloadTotal = postConns.downloadTotal;
  const postConnIds = (postConns.connections || []).map(c => c.id);

  console.log(`Post-patch Counters : Up ${postUploadTotal.toLocaleString()} B, Down ${postDownloadTotal.toLocaleString()} B`);
  console.log(`Post-patch Conns    : ${postConnIds.length} active connections`);

  // 4. 分析
  const isUploadCounterReset = postUploadTotal < preUploadTotal;
  const isDownloadCounterReset = postDownloadTotal < preDownloadTotal;

  const preSet = new Set(preConnIds);
  const survivingIds = postConnIds.filter(id => preSet.has(id));
  const newIds = postConnIds.filter(id => !preSet.has(id));
  const closedIds = preConnIds.filter(id => !new Set(postConnIds).has(id));

  const report = {
    experimentType: 'RUNTIME_CONFIG_PATCH',
    apiEndpoint: 'PATCH /configs',
    prePatch: {
      uploadTotal: preUploadTotal,
      downloadTotal: preDownloadTotal,
      activeConnectionsCount: preConnIds.length
    },
    postPatch: {
      uploadTotal: postUploadTotal,
      downloadTotal: postDownloadTotal,
      activeConnectionsCount: postConnIds.length
    },
    counterBehavior: {
      isUploadCounterReset,
      isDownloadCounterReset,
      uploadDelta: postUploadTotal - preUploadTotal,
      downloadDelta: postDownloadTotal - preDownloadTotal
    },
    connectionLifecycle: {
      survivingConnectionsCount: survivingIds.length,
      closedConnectionsCount: closedIds.length,
      newConnectionsCount: newIds.length
    }
  };

  const outDir = path.join(projectRoot, 'tmp/discovery/work-package-b1');
  fs.mkdirSync(outDir, { recursive: true });
  const outPath = path.join(outDir, 'config-patch-report.json');
  fs.writeFileSync(outPath, JSON.stringify(report, null, 2));

  console.log('\n----------------------------------------------------------------');
  console.log('PATCH ANALYSIS SUMMARY:');
  console.log(`  Counter Reset Occurred? : ${isUploadCounterReset || isDownloadCounterReset ? 'YES [RESET!]' : 'NO [MONOTONIC CONTINUOUS]'}`);
  console.log(`  Upload Total Delta      : +${(postUploadTotal - preUploadTotal).toLocaleString()} Bytes`);
  console.log(`  Download Total Delta    : +${(postDownloadTotal - preDownloadTotal).toLocaleString()} Bytes`);
  console.log(`  Surviving Connections   : ${survivingIds.length} / ${preConnIds.length} retained their ID`);
  console.log(`  Closed on Patch         : ${closedIds.length} closed`);
  console.log(`  New after Patch         : ${newIds.length} new`);
  console.log('================================================================\n');
}

main().catch(err => {
  console.error('[FATAL]', err);
  process.exit(1);
});
