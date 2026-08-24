/**
 * switch-node.mjs
 * 
 * ProxyLens Safe Controlled Node Switching Tool
 * 
 * 安全特性:
 * 1. 默认仅进行 Preflight / Dry-run，必须显式传入 --allow-live-mutation 才允许执行变更
 * 2. 变更后二次 GET 验证 (Verify switch)
 * 3. Best-effort 回滚保护 (SIGINT, SIGTERM, uncaughtException, unhandledRejection, finally)
 * 4. 回滚后二次 GET 验证 (Verify rollback)，若失败则 fail closed (exit 1, rollbackVerified: false)
 * 
 * 用法:
 *   node tools/discovery/scenarios/switch-node.mjs --group <groupName> --target <targetNode> [--allow-live-mutation] [--duration <sec>]
 */

import http from 'http';
import { fileURLToPath } from 'url';
import path from 'path';

export function getProxyGroup(controllerUrl, groupName) {
  return new Promise((resolve, reject) => {
    const url = new URL(`/proxies/${encodeURIComponent(groupName)}`, controllerUrl);
    http.get(url.toString(), (res) => {
      let data = '';
      res.on('data', d => data += d);
      res.on('end', () => {
        if (res.statusCode === 200) {
          try {
            resolve(JSON.parse(data));
          } catch (e) {
            reject(e);
          }
        } else {
          reject(new Error(`Failed to get proxy group ${groupName}: HTTP ${res.statusCode} ${data}`));
        }
      });
    }).on('error', reject);
  });
}

export function setProxyGroupNode(controllerUrl, groupName, nodeName) {
  return new Promise((resolve, reject) => {
    const url = new URL(`/proxies/${encodeURIComponent(groupName)}`, controllerUrl);
    const body = JSON.stringify({ name: nodeName });
    const req = http.request(url.toString(), {
      method: 'PUT',
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
          reject(new Error(`Failed to switch group ${groupName} to ${nodeName}: HTTP ${res.statusCode} ${data}`));
        }
      });
    });
    req.on('error', reject);
    req.write(body);
    req.end();
  });
}

export async function runControlledSwitch({
  controllerUrl = 'http://127.0.0.1:9090',
  groupName,
  targetNode,
  durationSec = 5,
  allowLiveMutation = false,
  onSwitched = null
}) {
  const initialInfo = await getProxyGroup(controllerUrl, groupName);
  const originalNode = initialInfo.now;

  console.log(`[SWITCH-GUARD] Group: [${groupName}] | Current Node: [${originalNode}]`);

  if (!initialInfo.all?.includes(targetNode)) {
    throw new Error(`Target node "${targetNode}" is not a valid option for group "${groupName}". Valid options: ${initialInfo.all?.join(', ')}`);
  }

  if (originalNode === targetNode) {
    console.log(`[SWITCH-GUARD] Target node is same as original node. No switch needed.`);
    return { originalNode, switchedNode: targetNode, rollbackVerified: true };
  }

  if (!allowLiveMutation) {
    console.log(`[DRY-RUN] Live mutation not enabled (--allow-live-mutation not specified). Skipping actual PUT.`);
    return { originalNode, targetNode, dryRun: true, rollbackVerified: true };
  }

  let rollbackDone = false;
  let rollbackVerified = false;
  let switchRequestedAt = null;
  let switchVerifiedAt = null;
  let rollbackRequestedAt = null;
  let rollbackVerifiedAt = null;

  const performRollback = async () => {
    if (rollbackDone) return;
    rollbackDone = true;
    rollbackRequestedAt = new Date().toISOString();
    try {
      console.log(`\n[SWITCH-GUARD] Performing best-effort rollback: switching [${groupName}] back to [${originalNode}]...`);
      await setProxyGroupNode(controllerUrl, groupName, originalNode);
      
      // 二次 GET 验证回滚结果
      const postRollbackInfo = await getProxyGroup(controllerUrl, groupName);
      rollbackVerifiedAt = new Date().toISOString();
      if (postRollbackInfo.now === originalNode) {
        rollbackVerified = true;
        console.log(`[SWITCH-GUARD] Rollback successfully verified: [${groupName}] is restored to [${originalNode}].`);
      } else {
        rollbackVerified = false;
        console.error(`[FATAL ROLLBACK MISMATCH] Group [${groupName}] expected [${originalNode}], but GET returned [${postRollbackInfo.now}]!`);
      }
    } catch (err) {
      rollbackVerifiedAt = new Date().toISOString();
      rollbackVerified = false;
      console.error(`[FATAL ROLLBACK ERROR] Failed to restore node for ${groupName}:`, err.message);
    }
  };

  const cleanupAndExit = async (code) => {
    await performRollback();
    if (!rollbackVerified) {
      console.error(`[CRITICAL] Rollback verification failed! Manual inspection required.`);
      process.exit(1);
    }
    process.exit(code);
  };

  process.once('SIGINT', () => cleanupAndExit(130));
  process.once('SIGTERM', () => cleanupAndExit(143));
  process.on('uncaughtException', async (err) => {
    console.error('[UNCAUGHT EXCEPTION IN SWITCH GUARD]', err);
    await cleanupAndExit(1);
  });
  process.on('unhandledRejection', async (reason) => {
    console.error('[UNHANDLED REJECTION IN SWITCH GUARD]', reason);
    await cleanupAndExit(1);
  });

  try {
    switchRequestedAt = new Date().toISOString();
    console.log(`[SWITCH-GUARD] Switching [${groupName}] from [${originalNode}] -> [${targetNode}]...`);
    await setProxyGroupNode(controllerUrl, groupName, targetNode);

    // 二次 GET 验证切换生效
    const verifyInfo = await getProxyGroup(controllerUrl, groupName);
    switchVerifiedAt = new Date().toISOString();
    if (verifyInfo.now !== targetNode) {
      throw new Error(`Switch verification failed: Expected [${targetNode}], but GET returned [${verifyInfo.now}]`);
    }
    console.log(`[SWITCH VERIFIED] Group [${groupName}] is confirmed active on [${targetNode}].`);

    if (typeof onSwitched === 'function') {
      await onSwitched();
    } else {
      console.log(`[SWITCH-GUARD] Holding switch for ${durationSec} seconds...`);
      await new Promise(r => setTimeout(r, durationSec * 1000));
    }
  } finally {
    await performRollback();
  }

  if (!rollbackVerified) {
    throw new Error(`Rollback verification failed: Group [${groupName}] was not verified restored to [${originalNode}]`);
  }

  return {
    originalNode,
    switchedNode: targetNode,
    rollbackVerified: true,
    switchRequestedAt,
    switchVerifiedAt,
    rollbackRequestedAt,
    rollbackVerifiedAt
  };
}

// CLI 执行
if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const args = process.argv.slice(2);
  let groupName = null;
  let targetNode = null;
  let durationSec = 5;
  let allowLiveMutation = false;

  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--group' && args[i + 1]) groupName = args[++i];
    if (args[i] === '--target' && args[i + 1]) targetNode = args[++i];
    if (args[i] === '--duration' && args[i + 1]) durationSec = parseInt(args[++i], 10);
    if (args[i] === '--allow-live-mutation') allowLiveMutation = true;
  }

  if (!groupName || !targetNode) {
    console.log('Usage: node tools/discovery/scenarios/switch-node.mjs --group <groupName> --target <targetNode> [--allow-live-mutation] [--duration <sec>]');
    process.exit(1);
  }

  runControlledSwitch({ groupName, targetNode, durationSec, allowLiveMutation })
    .then((res) => {
      console.log('[DONE] Controlled switch cycle completed.', res);
      if (!res.rollbackVerified) process.exit(1);
    })
    .catch(err => {
      console.error('[ERROR]', err.message);
      process.exit(1);
    });
}
