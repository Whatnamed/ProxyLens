/**
 * switch-node.mjs
 * 
 * ProxyLens Safe Controlled Node Switching & Dynamic Routing Validation Tool
 * 
 * 功能:
 * 1. 查询策略组当前选中节点
 * 2. 切换策略组到指定目标节点
 * 3. 具备自动回滚 (Auto-Rollback) 机制，实验结束后或异常退出时 100% 恢复初始节点
 * 
 * 用法:
 *   node tools/discovery/scenarios/switch-node.mjs --group <groupName> --target <targetNode> [--duration <sec>]
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
    return { originalNode, switchedNode: targetNode };
  }

  let rollbackDone = false;
  const rollback = async () => {
    if (rollbackDone) return;
    rollbackDone = true;
    try {
      console.log(`\n[SWITCH-GUARD] Performing rollback: switching [${groupName}] back to [${originalNode}]...`);
      await setProxyGroupNode(controllerUrl, groupName, originalNode);
      console.log(`[SWITCH-GUARD] Rollback successfully completed.`);
    } catch (err) {
      console.error(`[FATAL ROLLBACK ERROR] Failed to restore node for ${groupName}:`, err.message);
    }
  };

  process.on('SIGINT', async () => {
    await rollback();
    process.exit(130);
  });
  process.on('uncaughtException', async (err) => {
    console.error('[UNCAUGHT EXCEPTION]', err);
    await rollback();
    process.exit(1);
  });

  try {
    console.log(`[SWITCH-GUARD] Switching [${groupName}] from [${originalNode}] -> [${targetNode}]...`);
    await setProxyGroupNode(controllerUrl, groupName, targetNode);
    console.log(`[SWITCH-GUARD] Switched successfully.`);

    if (typeof onSwitched === 'function') {
      await onSwitched();
    } else {
      console.log(`[SWITCH-GUARD] Holding switch for ${durationSec} seconds...`);
      await new Promise(r => setTimeout(r, durationSec * 1000));
    }
  } finally {
    await rollback();
  }

  return { originalNode, switchedNode: targetNode };
}

// CLI 执行
if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const args = process.argv.slice(2);
  let groupName = null;
  let targetNode = null;
  let durationSec = 5;

  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--group' && args[i + 1]) groupName = args[++i];
    if (args[i] === '--target' && args[i + 1]) targetNode = args[++i];
    if (args[i] === '--duration' && args[i + 1]) durationSec = parseInt(args[++i], 10);
  }

  if (!groupName || !targetNode) {
    console.log('Usage: node tools/discovery/scenarios/switch-node.mjs --group <groupName> --target <targetNode> [--duration <sec>]');
    process.exit(1);
  }

  runControlledSwitch({ groupName, targetNode, durationSec })
    .then(() => {
      console.log('[DONE] Controlled switch cycle completed.');
    })
    .catch(err => {
      console.error('[ERROR]', err.message);
      process.exit(1);
    });
}
