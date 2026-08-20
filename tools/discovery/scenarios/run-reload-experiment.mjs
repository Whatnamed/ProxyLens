/**
 * run-reload-experiment.mjs
 * 
 * Stage B2: Config Reload / Soft Restart & Kernel Lifecycle Experiment
 * 
 * 流程:
 * 1. 记录重载前的全局计数器与当前活跃连接列表 (Capture pre-reload baseline)
 * 2. 通过 Controller PUT /configs 接口触发配置重新加载 (Reload)
 * 3. 记录重载后的全局计数器与活跃连接列表
 * 4. 分析：
 *    - uploadTotal / downloadTotal 是否发生回退或归零 (Counter Reset)?
 *    - 已有连接 ID 是否全部失效 / 保持 / 重新建立?
 */

import http from 'http';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '../../..');

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
          reject(new Error(`Reload failed: HTTP ${res.statusCode} ${data}`));
        }
      });
    });
    req.on('error', reject);
    req.write(body);
    req.end();
  });
}

async function main() {
  const controllerUrl = 'http://127.0.0.1:9090';

  console.log('================================================================');
  console.log('STAGE B2: CONFIG RELOAD & COUNTER RESET EXPERIMENT');
  console.log('================================================================');

  // 1. 获取当前状态
  console.log('[STEP 1] Fetching pre-reload connections & global counters...');
  const preConns = await fetchJson(`${controllerUrl}/connections`);
  const preConfigs = await fetchJson(`${controllerUrl}/configs`);

  const preUploadTotal = preConns.uploadTotal;
  const preDownloadTotal = preConns.downloadTotal;
  const preConnIds = (preConns.connections || []).map(c => c.id);

  console.log(`Pre-reload Counters : Up ${preUploadTotal.toLocaleString()} B, Down ${preDownloadTotal.toLocaleString()} B`);
  console.log(`Pre-reload Conns    : ${preConnIds.length} active connections`);

  // 2. 触发配置重新加载 (PATCH /configs)
  console.log('\n[STEP 2] Triggering config reload via PATCH /configs...');
  await patchConfig(controllerUrl, { mode: preConfigs.mode || 'rule' });
  console.log('[RELOAD TRIGGERED] Waiting 2 seconds for kernel state update...');
  await new Promise(r => setTimeout(r, 2000));

  // 3. 获取重载后状态
  console.log('\n[STEP 3] Fetching post-reload connections & global counters...');
  const postConns = await fetchJson(`${controllerUrl}/connections`);
  const postUploadTotal = postConns.uploadTotal;
  const postDownloadTotal = postConns.downloadTotal;
  const postConnIds = (postConns.connections || []).map(c => c.id);

  console.log(`Post-reload Counters: Up ${postUploadTotal.toLocaleString()} B, Down ${postDownloadTotal.toLocaleString()} B`);
  console.log(`Post-reload Conns   : ${postConnIds.length} active connections`);

  // 4. 分析
  const isUploadCounterReset = postUploadTotal < preUploadTotal;
  const isDownloadCounterReset = postDownloadTotal < preDownloadTotal;

  const preSet = new Set(preConnIds);
  const survivingIds = postConnIds.filter(id => preSet.has(id));
  const newIds = postConnIds.filter(id => !preSet.has(id));
  const closedIds = preConnIds.filter(id => !new Set(postConnIds).has(id));

  const report = {
    preReload: {
      uploadTotal: preUploadTotal,
      downloadTotal: preDownloadTotal,
      activeConnectionsCount: preConnIds.length
    },
    postReload: {
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

  const outPath = path.join(projectRoot, 'tmp/discovery/work-package-b/reload-report.json');
  fs.writeFileSync(outPath, JSON.stringify(report, null, 2));

  console.log('\n----------------------------------------------------------------');
  console.log('RELOAD ANALYSIS SUMMARY:');
  console.log(`  Counter Reset Occurred? : ${isUploadCounterReset || isDownloadCounterReset ? 'YES [RESET!]' : 'NO [MONOTONIC CONTINUOUS]'}`);
  console.log(`  Upload Total Delta      : +${(postUploadTotal - preUploadTotal).toLocaleString()} Bytes`);
  console.log(`  Download Total Delta    : +${(postDownloadTotal - preDownloadTotal).toLocaleString()} Bytes`);
  console.log(`  Surviving Connections   : ${survivingIds.length} / ${preConnIds.length} retained their ID`);
  console.log(`  Closed on Reload        : ${closedIds.length} closed`);
  console.log(`  New after Reload        : ${newIds.length} new`);
  console.log('================================================================\n');
}

main().catch(err => {
  console.error('[FATAL]', err);
  process.exit(1);
});
