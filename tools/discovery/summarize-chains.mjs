/**
 * summarize-chains.mjs
 * 
 * ProxyLens Static Routing Evidence Inventory Tool
 * 
 * 盘点已有样本及会话中的所有 routing 模式：
 * - rule & rulePayload
 * - chains 数组内容与顺序 (Hop 0 -> Hop N)
 * - providerChains
 * - 进程、目标域名/IP 与路由链的对应关系
 * - 应用连接 vs 中继连接模式
 * 
 * 用法:
 *   node tools/discovery/summarize-chains.mjs [scanDir] [--json]
 */

import fs from 'fs';
import path from 'path';
import readline from 'readline';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '../..');

async function parseNdjson(filePath) {
  if (!fs.existsSync(filePath)) return [];
  const lines = [];
  const rl = readline.createInterface({
    input: fs.createReadStream(filePath),
    crlfDelay: Infinity
  });
  for await (const line of rl) {
    if (line.trim()) {
      try {
        lines.push(JSON.parse(line));
      } catch (err) {}
    }
  }
  return lines;
}

function findConnectionsFiles(dir) {
  const results = [];
  function recurse(current) {
    if (!fs.existsSync(current)) return;
    const entries = fs.readdirSync(current, { withFileTypes: true });
    for (const e of entries) {
      const fullPath = path.join(current, e.name);
      if (e.isDirectory()) {
        recurse(fullPath);
      } else if (e.isFile() && e.name === 'connections.ndjson') {
        results.push(fullPath);
      }
    }
  }
  recurse(dir);
  return results;
}

export async function summarizeRoutingEvidence(scanDir) {
  const connFiles = findConnectionsFiles(scanDir);
  const routingPatterns = new Map(); // key -> { rule, rulePayload, chains, providerChains, sampleProcesses, sampleHosts, count, sampleConnectionIds }

  for (const file of connFiles) {
    const frames = await parseNdjson(file);
    for (const frame of frames) {
      const frameData = frame.frame || frame.payload || {};
      const connections = frameData.connections || [];

      for (const c of connections) {
        if (!c.id) continue;
        const rule = c.rule || '';
        const rulePayload = c.rulePayload || '';
        const chains = Array.isArray(c.chains) ? c.chains : [];
        const providerChains = Array.isArray(c.providerChains) ? c.providerChains : [];
        const processName = c.metadata?.process || '<empty>';
        const host = c.metadata?.host || c.metadata?.destinationIP || '<empty>';

        const chainsKey = chains.join(' -> ');
        const providerKey = providerChains.join(' -> ');
        const key = `${rule}|${rulePayload}|${chainsKey}|${providerKey}`;

        if (!routingPatterns.has(key)) {
          routingPatterns.set(key, {
            rule,
            rulePayload,
            chains,
            chainsDisplay: chainsKey || '<empty>',
            providerChains,
            providerChainsDisplay: providerKey || '<empty>',
            sampleProcesses: new Set(),
            sampleHosts: new Set(),
            count: 0,
            sampleId: c.id
          });
        }

        const pattern = routingPatterns.get(key);
        pattern.count += 1;
        if (pattern.sampleProcesses.size < 5) pattern.sampleProcesses.add(processName);
        if (pattern.sampleHosts.size < 5) pattern.sampleHosts.add(host);
      }
    }
  }

  // 格式化输出
  const patternsList = Array.from(routingPatterns.values()).map(p => ({
    ...p,
    sampleProcesses: Array.from(p.sampleProcesses),
    sampleHosts: Array.from(p.sampleHosts)
  }));

  // 按出现次数降序排序
  patternsList.sort((a, b) => b.count - a.count);

  return {
    totalFilesScanned: connFiles.length,
    distinctPatternsCount: patternsList.length,
    patterns: patternsList
  };
}

// CLI 执行
if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const targetDir = process.argv[2] ? path.resolve(process.argv[2]) : path.join(projectRoot, 'tmp/discovery');
  const isJson = process.argv.includes('--json');

  summarizeRoutingEvidence(targetDir)
    .then(result => {
      if (isJson) {
        console.log(JSON.stringify(result, null, 2));
      } else {
        console.log('================================================================');
        console.log('PROXYLENS STATIC ROUTING EVIDENCE INVENTORY');
        console.log('================================================================');
        console.log(`Files Scanned        : ${result.totalFilesScanned}`);
        console.log(`Distinct Patterns    : ${result.distinctPatternsCount}`);
        console.log('----------------------------------------------------------------');
        console.log('IDENTIFIED ROUTING & CHAIN PATTERNS:');
        console.log('----------------------------------------------------------------');

        result.patterns.forEach((p, idx) => {
          console.log(`[Pattern #${idx + 1}] Count: ${p.count} frames`);
          console.log(`  Rule             : ${p.rule || '<empty>'} (Payload: "${p.rulePayload}")`);
          console.log(`  Chains (Hops)    : ${p.chainsDisplay}`);
          console.log(`  Provider Chains  : ${p.providerChainsDisplay}`);
          console.log(`  Sample Processes : ${p.sampleProcesses.join(', ')}`);
          console.log(`  Sample Hosts     : ${p.sampleHosts.join(', ')}`);
          console.log('----------------------------------------------------------------');
        });
      }
    })
    .catch(err => {
      console.error(err);
      process.exit(1);
    });
}
