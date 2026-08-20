#!/usr/bin/env node

/**
 * ProxyLens Phase 0 — Short Request Burst Ground Truth Generator
 * 
 * 产生独立短 TCP/TLS HTTP(S) 请求，并精确记录应用层网络套接字事件作为 Ground Truth。
 * 纯 Node.js 原生标准库，绝不复用 socket (No Keep-Alive)，不保存响应 Body。
 */

import http from 'node:http';
import https from 'node:https';
import fs from 'node:fs';
import path from 'node:path';

function printHelp() {
  console.log(`
Usage:
  node tools/discovery/scenarios/short-request-burst.mjs [options]

Options:
  --url <URL>            目标请求 URL (必填)
  --count <N>            请求总数 (默认: 10)
  --spacing-ms <N>       请求间隔毫秒 (默认: 100)
  --output <file>        Ground Truth NDJSON 输出路径 (默认: stdout)
  --method <HEAD|GET>    HTTP 请求方法 (默认: HEAD)
  -h, --help             显示帮助信息

Example:
  node tools/discovery/scenarios/short-request-burst.mjs --url https://www.baidu.com --count 5 --output tmp/gt.ndjson
`);
}

function parseArgs() {
  const args = process.argv.slice(2);
  const options = {
    url: '',
    count: 10,
    spacingMs: 100,
    output: '',
    method: 'HEAD',
  };

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === '--url') {
      options.url = args[++i];
    } else if (arg === '--count') {
      options.count = parseInt(args[++i], 10) || 10;
    } else if (arg === '--spacing-ms') {
      options.spacingMs = parseInt(args[++i], 10) || 100;
    } else if (arg === '--output') {
      options.output = args[++i];
    } else if (arg === '--method') {
      options.method = args[++i]?.toUpperCase() || 'HEAD';
    } else if (arg === '-h' || arg === '--help') {
      printHelp();
      process.exit(0);
    }
  }

  return options;
}

function runSingleRequest(requestId, urlStr, method) {
  return new Promise((resolve) => {
    const parsedUrl = new URL(urlStr);
    const isHttps = parsedUrl.protocol === 'https:';
    const httpLib = isHttps ? https : http;

    const requestedAt = new Date().toISOString();
    let socketAssignedAt = null;
    let connectAt = null;
    let secureConnectAt = null;
    let responseAt = null;
    let completedAt = null;

    let localAddress = null;
    let localPort = null;
    let remoteAddress = null;
    let remotePort = null;

    let httpStatus = null;
    let success = false;
    let errorMessage = null;

    // 禁用连接复用，保证每个请求独立 socket
    const agent = new httpLib.Agent({ keepAlive: false, maxSockets: Infinity });

    const reqOptions = {
      protocol: parsedUrl.protocol,
      hostname: parsedUrl.hostname,
      port: parsedUrl.port || (isHttps ? 443 : 80),
      path: parsedUrl.pathname + parsedUrl.search,
      method,
      headers: {
        'Connection': 'close',
        'User-Agent': 'ProxyLens-Discovery/0.3',
      },
      agent,
      timeout: 10000,
    };

    const startTime = Date.now();

    const req = httpLib.request(reqOptions, (res) => {
      responseAt = new Date().toISOString();
      httpStatus = res.statusCode;
      
      // 只 drain 丢弃正文，绝不保存敏感 payload
      res.on('data', () => {});
      res.on('end', () => {
        completedAt = new Date().toISOString();
        success = true;
        finish();
      });
    });

    req.on('socket', (socket) => {
      socketAssignedAt = new Date().toISOString();

      const onConnect = () => {
        connectAt = new Date().toISOString();
        localAddress = socket.localAddress;
        localPort = socket.localPort;
        remoteAddress = socket.remoteAddress;
        remotePort = socket.remotePort;
      };

      if (socket.connecting) {
        socket.once('connect', onConnect);
      } else {
        onConnect();
      }

      if (isHttps) {
        socket.once('secureConnect', () => {
          secureConnectAt = new Date().toISOString();
          localAddress = socket.localAddress;
          localPort = socket.localPort;
          remoteAddress = socket.remoteAddress;
          remotePort = socket.remotePort;
        });
      }
    });

    req.on('error', (err) => {
      completedAt = new Date().toISOString();
      errorMessage = err.message;
      success = false;
      finish();
    });

    req.on('timeout', () => {
      req.destroy(new Error('REQUEST_TIMEOUT'));
    });

    let finished = false;
    function finish() {
      if (finished) return;
      finished = true;
      const durationMs = Date.now() - startTime;
      agent.destroy();

      const record = {
        requestId,
        targetUrl: urlStr,
        method,
        requestedAt,
        socketAssignedAt,
        connectAt,
        secureConnectAt,
        responseAt,
        completedAt,
        durationMs,
        localAddress,
        localPort,
        remoteAddress,
        remotePort,
        httpStatus,
        success,
        error: errorMessage,
        eligibleConnected: localPort !== null && (connectAt !== null || secureConnectAt !== null),
      };

      resolve(record);
    }

    req.end();
  });
}

async function main() {
  const options = parseArgs();
  if (!options.url) {
    console.error('[ERROR] 必须指定 --url 参数');
    printHelp();
    process.exit(1);
  }

  let outStream = process.stdout;
  if (options.output) {
    const dir = path.dirname(path.resolve(options.output));
    if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true });
    outStream = fs.createWriteStream(path.resolve(options.output), { encoding: 'utf8' });
  }

  console.log(`[BURST] Starting ${options.count} requests to ${options.url} (spacing: ${options.spacingMs}ms)...`);

  const stats = {
    attempted: 0,
    socketAssigned: 0,
    eligibleConnected: 0,
    responseReceived: 0,
    completedSuccessfully: 0,
    failedBeforeConnect: 0,
    failedAfterConnect: 0,
  };

  const records = [];

  for (let i = 0; i < options.count; i++) {
    stats.attempted++;
    const record = await runSingleRequest(i + 1, options.url, options.method);
    records.push(record);

    if (record.socketAssignedAt) stats.socketAssigned++;
    if (record.eligibleConnected) stats.eligibleConnected++;
    if (record.responseAt) stats.responseReceived++;
    if (record.success) stats.completedSuccessfully++;
    if (!record.eligibleConnected && !record.success) stats.failedBeforeConnect++;
    if (record.eligibleConnected && !record.success) stats.failedAfterConnect++;

    const line = JSON.stringify(record) + '\n';
    if (options.output) {
      outStream.write(line);
    }

    if (i < options.count - 1 && options.spacingMs > 0) {
      await new Promise((r) => setTimeout(r, options.spacingMs));
    }
  }

  if (options.output) {
    await new Promise((resolve) => outStream.end(resolve));
    console.log(`[BURST] Ground Truth saved to: ${options.output}`);
  }

  console.log(`\n--- Ground Truth Run Summary ---`);
  console.log(`Target URL                 : ${options.url}`);
  console.log(`Attempted Requests         : ${stats.attempted}`);
  console.log(`Socket Assigned            : ${stats.socketAssigned}`);
  console.log(`Eligible Connected (TCP)   : ${stats.eligibleConnected} (Primary Denominator)`);
  console.log(`Responses Received         : ${stats.responseReceived}`);
  console.log(`Completed Successfully     : ${stats.completedSuccessfully} (Secondary Denominator)`);
  console.log(`Failed Before Connect      : ${stats.failedBeforeConnect}`);
  console.log(`Failed After Connect       : ${stats.failedAfterConnect}`);
  console.log(`--------------------------------\n`);
}

main().catch((err) => {
  console.error('[FATAL]', err);
  process.exit(1);
});
