#!/usr/bin/env node

/**
 * ProxyLens Phase 0 — Mihomo Discovery Probe
 * 
 * 一个最小、一次性研究用途、只读的探针脚本。
 * 零第三方依赖，基于 Node.js 22+ 原生 WebSocket 与 fetch API 实现。
 * 
 * 严格只读，不修改任何 Mihomo 状态与配置。
 */

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const PROBE_VERSION = '0.1.0-discovery';

// 命令行参数解析
function parseArgs() {
  const args = process.argv.slice(2);
  const options = {
    controller: process.env.MIHOMO_CONTROLLER || 'http://127.0.0.1:9090',
    secret: process.env.MIHOMO_SECRET || process.env.PROBE_SECRET || '',
    output: '',
    duration: 0, // 0 表示无限期运行直到 Ctrl+C
  };

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === '--controller' || arg === '-c') {
      options.controller = args[++i];
    } else if (arg === '--secret' || arg === '-s') {
      options.secret = args[++i];
    } else if (arg === '--output' || arg === '-o') {
      options.output = args[++i];
    } else if (arg === '--duration' || arg === '-d') {
      options.duration = parseInt(args[++i], 10) || 0;
    } else if (arg === '--help' || arg === '-h') {
      printHelp();
      process.exit(0);
    }
  }

  return options;
}

function printHelp() {
  console.log(`
ProxyLens Phase 0 — Mihomo Discovery Probe (v${PROBE_VERSION})

Usage:
  node tools/discovery/probe.mjs [options]

Options:
  -c, --controller <URL>   Mihomo External Controller 地址 (默认: http://127.0.0.1:9090)
  -s, --secret <SECRET>    Controller Secret 鉴权凭据 (也可通过 MIHOMO_SECRET 环境变量提供)
  -o, --output <DIR>       输出目录 (默认: tmp/discovery/<timestamp>)
  -d, --duration <SEC>     采集持续时间 (秒)，0 表示持续运行直到 Ctrl+C (默认: 0)
  -h, --help               显示帮助信息

Examples:
  node tools/discovery/probe.mjs --controller http://127.0.0.1:9090
  node tools/discovery/probe.mjs -c http://127.0.0.1:9097 -s mysecret --duration 60
`);
}

// 规范化 URL 与 WebSocket 地址
function buildWsUrl(controllerUrl, endpoint, secret) {
  const parsed = new URL(controllerUrl);
  const wsProto = parsed.protocol === 'https:' ? 'wss:' : 'ws:';
  const cleanPath = endpoint.startsWith('/') ? endpoint : '/' + endpoint;
  const wsUrl = new URL(`${wsProto}//${parsed.host}${cleanPath}`);
  if (secret) {
    wsUrl.searchParams.set('token', secret);
  }
  return wsUrl.toString();
}

function sanitizeUrl(urlStr) {
  try {
    const u = new URL(urlStr);
    if (u.searchParams.has('token')) {
      u.searchParams.set('token', '***REDACTED***');
    }
    return u.toString();
  } catch {
    return urlStr;
  }
}

async function main() {
  const options = parseArgs();
  const startTime = new Date();
  const sessionId = startTime.toISOString().replace(/[:.]/g, '-');

  const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
  const outputDir = options.output
    ? path.resolve(options.output)
    : path.join(repoRoot, 'tmp', 'discovery', sessionId);

  // 确保输出目录存在
  if (!fs.existsSync(outputDir)) {
    fs.mkdirSync(outputDir, { recursive: true });
  }

  const manifestPath = path.join(outputDir, 'manifest.json');
  const connectionsPath = path.join(outputDir, 'connections.ndjson');
  const trafficPath = path.join(outputDir, 'traffic.ndjson');
  const eventsPath = path.join(outputDir, 'events.ndjson');

  const connStream = fs.createWriteStream(connectionsPath, { flags: 'a', encoding: 'utf8' });
  const trafficStream = fs.createWriteStream(trafficPath, { flags: 'a', encoding: 'utf8' });
  const eventStream = fs.createWriteStream(eventsPath, { flags: 'a', encoding: 'utf8' });

  const stats = {
    connectionsFrames: 0,
    trafficFrames: 0,
    eventsCount: 0,
  };

  function logEvent(type, message, details = null) {
    stats.eventsCount++;
    const evt = {
      timestamp: new Date().toISOString(),
      type,
      message,
      details,
    };
    eventStream.write(JSON.stringify(evt) + '\n');
    console.log(`[${evt.timestamp}] [${type}] ${message}`);
  }

  logEvent('probe_start', `Starting Discovery Probe v${PROBE_VERSION}`, {
    outputDir,
    controller: sanitizeUrl(options.controller),
    hasSecret: !!options.secret,
    durationSec: options.duration,
  });

  // 1. Preflight: 检查 /version
  let mihomoVersionInfo = null;
  try {
    const versionUrl = new URL('/version', options.controller);
    const headers = {};
    if (options.secret) {
      headers['Authorization'] = `Bearer ${options.secret}`;
    }

    logEvent('preflight_check', `Checking controller connectivity: ${sanitizeUrl(versionUrl.toString())}`);
    const res = await fetch(versionUrl.toString(), {
      headers,
      signal: AbortSignal.timeout(5000),
    });

    if (!res.ok) {
      const body = await res.text().catch(() => '');
      throw new Error(`Controller returned HTTP ${res.status}: ${body.slice(0, 100)}`);
    }

    mihomoVersionInfo = await res.json();
    logEvent('preflight_success', `Connected to Mihomo successfully`, mihomoVersionInfo);
  } catch (err) {
    logEvent('preflight_failed', `Failed to connect to External Controller: ${err.message}`);
    console.error(`\n[ERROR] 无法连接到 External Controller: ${err.message}`);
    console.error(`请确认 Mihomo / FLClash 是否运行，且已在配置中开启 external-controller 端口与正确的 secret。\n`);
    
    // 写入失败 manifest 后退出
    saveManifest('failed', err.message);
    cleanupStreams();
    process.exit(1);
  }

  // 2. 建立 WebSocket 采集连接
  const wsConnectionsUrl = buildWsUrl(options.controller, '/connections', options.secret);
  const wsTrafficUrl = buildWsUrl(options.controller, '/traffic', options.secret);

  let wsConn = null;
  let wsTraffic = null;
  let isShuttingDown = false;

  function initWebSockets() {
    logEvent('ws_connecting', `Connecting to /connections and /traffic WebSockets`);

    // Connections WS
    try {
      wsConn = new WebSocket(wsConnectionsUrl);
      wsConn.onopen = () => {
        logEvent('ws_open', 'WebSocket /connections connected');
      };
      wsConn.onmessage = (event) => {
        stats.connectionsFrames++;
        const receivedAt = new Date().toISOString();
        let payload;
        try {
          payload = JSON.parse(event.data);
        } catch {
          payload = { raw: event.data, error: 'JSON_PARSE_ERROR' };
        }
        connStream.write(JSON.stringify({ receivedAt, frame: payload }) + '\n');
      };
      wsConn.onerror = (err) => {
        logEvent('ws_error', `WebSocket /connections error: ${err.message || 'unknown error'}`);
      };
      wsConn.onclose = (event) => {
        logEvent('ws_close', `WebSocket /connections closed (code=${event.code}, reason=${event.reason || 'none'})`);
        if (!isShuttingDown) {
          logEvent('warn', 'WebSocket /connections unexpectedly closed by server');
        }
      };
    } catch (e) {
      logEvent('ws_create_error', `Failed to initialize /connections WS: ${e.message}`);
    }

    // Traffic WS
    try {
      wsTraffic = new WebSocket(wsTrafficUrl);
      wsTraffic.onopen = () => {
        logEvent('ws_open', 'WebSocket /traffic connected');
      };
      wsTraffic.onmessage = (event) => {
        stats.trafficFrames++;
        const receivedAt = new Date().toISOString();
        let payload;
        try {
          payload = JSON.parse(event.data);
        } catch {
          payload = { raw: event.data, error: 'JSON_PARSE_ERROR' };
        }
        trafficStream.write(JSON.stringify({ receivedAt, frame: payload }) + '\n');
      };
      wsTraffic.onerror = (err) => {
        logEvent('ws_error', `WebSocket /traffic error: ${err.message || 'unknown error'}`);
      };
      wsTraffic.onclose = (event) => {
        logEvent('ws_close', `WebSocket /traffic closed (code=${event.code}, reason=${event.reason || 'none'})`);
      };
    } catch (e) {
      logEvent('ws_create_error', `Failed to initialize /traffic WS: ${e.message}`);
    }
  }

  function saveManifest(status, errorMessage = null) {
    const manifest = {
      probeVersion: PROBE_VERSION,
      sessionId,
      startTime: startTime.toISOString(),
      endTime: new Date().toISOString(),
      durationSeconds: Math.round((Date.now() - startTime.getTime()) / 1000),
      controllerUrl: sanitizeUrl(options.controller),
      hasSecret: !!options.secret,
      mihomoVersion: mihomoVersionInfo,
      status,
      errorMessage,
      stats,
      files: {
        manifest: 'manifest.json',
        connections: 'connections.ndjson',
        traffic: 'traffic.ndjson',
        events: 'events.ndjson',
      },
    };
    fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2), 'utf8');
  }

  function cleanupStreams() {
    try { connStream.end(); } catch {}
    try { trafficStream.end(); } catch {}
    try { eventStream.end(); } catch {}
  }

  function shutdown(reason = 'completed') {
    if (isShuttingDown) return;
    isShuttingDown = true;
    logEvent('probe_shutdown', `Shutting down probe (reason=${reason})`, { stats });

    if (wsConn && wsConn.readyState === WebSocket.OPEN) {
      try { wsConn.close(1000, 'Probe shutdown'); } catch {}
    }
    if (wsTraffic && wsTraffic.readyState === WebSocket.OPEN) {
      try { wsTraffic.close(1000, 'Probe shutdown'); } catch {}
    }

    saveManifest(reason);
    cleanupStreams();

    console.log(`\n========================================`);
    console.log(`Probe 采集已结束 (${reason})`);
    console.log(`总抓取: ${stats.connectionsFrames} 帧 Connections, ${stats.trafficFrames} 帧 Traffic`);
    console.log(`样本保存目录: ${outputDir}`);
    console.log(`========================================\n`);

    process.exit(0);
  }

  // 信号与异常捕获
  process.on('SIGINT', () => {
    console.log('\n[INFO] 接收到中断信号 (SIGINT/Ctrl+C)，正在安全退出...');
    shutdown('interrupted_by_user');
  });

  process.on('SIGTERM', () => {
    shutdown('terminated');
  });

  process.on('uncaughtException', (err) => {
    logEvent('uncaught_exception', err.message, { stack: err.stack });
    shutdown('failed');
  });

  // 启动采集
  initWebSockets();

  if (options.duration > 0) {
    logEvent('timer_set', `Probe will automatically stop after ${options.duration} seconds`);
    setTimeout(() => {
      shutdown('duration_elapsed');
    }, options.duration * 1000);
  }
}

main().catch((err) => {
  console.error('[FATAL]', err);
  process.exit(1);
});
