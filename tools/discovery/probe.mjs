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

const PROBE_VERSION = '0.3.1-discovery';

// 命令行参数解析
function parseArgs() {
  const args = process.argv.slice(2);
  const options = {
    controller: process.env.MIHOMO_CONTROLLER || 'http://127.0.0.1:9090',
    secret: process.env.MIHOMO_SECRET || process.env.PROBE_SECRET || '',
    output: '',
    duration: 0, // 0 表示持续运行直到 Ctrl+C
    connectionsInterval: null, // 毫秒数，例如 500 或 250
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
    } else if (arg === '--connections-interval' || arg === '-i') {
      options.connectionsInterval = parseInt(args[++i], 10) || null;
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
  -c, --controller <URL>            Mihomo External Controller 地址 (默认: http://127.0.0.1:9090)
  -s, --secret <SECRET>             Controller Secret (注意: 优先推荐使用 MIHOMO_SECRET 环境变量以防泄漏)
  -o, --output <DIR>                输出目录 (默认: tmp/discovery/<timestamp>)
  -d, --duration <SEC>              采集持续时间 (秒)，0 表示持续运行直到 Ctrl+C (默认: 0)
  -i, --connections-interval <MS>   /connections 请求采样间隔 (毫秒)，例如 500 或 250 (仅用于研究评测)
  -h, --help                        显示帮助信息

Security Note:
  使用 CLI 命令行参数 -s 可能会将 Secret 暴露于系统进程表及 Shell 历史记录中。
  推荐方式: $env:MIHOMO_SECRET="your_secret"; node tools/discovery/probe.mjs
`);
}

// 规范化 URL 与 WebSocket 地址
function buildWsUrl(controllerUrl, endpoint, secret, extraParams = {}) {
  const parsed = new URL(controllerUrl);
  const wsProto = parsed.protocol === 'https:' ? 'wss:' : 'ws:';
  const cleanPath = endpoint.startsWith('/') ? endpoint : '/' + endpoint;
  const wsUrl = new URL(`${wsProto}//${parsed.host}${cleanPath}`);
  if (secret) {
    wsUrl.searchParams.set('token', secret);
  }
  for (const [k, v] of Object.entries(extraParams)) {
    if (v !== null && v !== undefined) {
      wsUrl.searchParams.set(k, String(v));
    }
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

// 等待单个 WritableStream 完成应用层写缓冲 flush (finish/close)
function waitStreamFinish(stream) {
  return new Promise((resolve) => {
    if (!stream || stream.destroyed || stream.closed) {
      return resolve();
    }
    let finished = false;
    const onDone = () => {
      if (!finished) {
        finished = true;
        resolve();
      }
    };
    stream.end(onDone);
    stream.once('finish', onDone);
    stream.once('close', onDone);
    stream.once('error', onDone);
  });
}

async function main() {
  const options = parseArgs();
  const startTime = new Date();
  const sessionId = startTime.toISOString().replace(/[:.]/g, '-');

  const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
  const outputDir = options.output
    ? path.resolve(options.output)
    : path.join(repoRoot, 'tmp', 'discovery', sessionId);

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

  // 状态与 Evidence Quality 追踪建模
  const sessionEvidence = {
    preflight: {
      status: 'pending',
      httpStatus: null,
      mihomoVersion: null,
      error: null,
    },
    channels: {
      connections: {
        opened: false,
        closedUnexpectedly: false,
        framesReceived: 0,
        parseErrors: 0,
        writeErrors: 0,
      },
      traffic: {
        opened: false,
        closedUnexpectedly: false,
        framesReceived: 0,
        parseErrors: 0,
        writeErrors: 0,
      },
      events: {
        writeErrors: 0,
      },
    },
    flush: {
      completed: false,
      timedOut: false,
      error: null,
    },
    eventsCount: 0,
    fatalErrors: [],
  };

  connStream.on('error', (err) => {
    sessionEvidence.channels.connections.writeErrors++;
    console.error(`[STREAM_ERR] connections.ndjson write error: ${err.message}`);
  });

  trafficStream.on('error', (err) => {
    sessionEvidence.channels.traffic.writeErrors++;
    console.error(`[STREAM_ERR] traffic.ndjson write error: ${err.message}`);
  });

  eventStream.on('error', (err) => {
    sessionEvidence.channels.events.writeErrors++;
    console.error(`[STREAM_ERR] events.ndjson write error: ${err.message}`);
  });

  let isShuttingDown = false;

  function logEvent(type, message, details = null) {
    if (isShuttingDown && (type === 'ws_close' || type === 'ws_error')) {
      return; // 忽略 shutdown 触发的正常 WS 关闭与残留事件，避免 write-after-end
    }
    sessionEvidence.eventsCount++;
    const evt = {
      timestamp: new Date().toISOString(),
      type,
      message,
      details,
    };
    try {
      if (!eventStream.destroyed && !eventStream.closed) {
        eventStream.write(JSON.stringify(evt) + '\n');
      }
    } catch {}
    console.log(`[${evt.timestamp}] [${type}] ${message}`);
  }

  logEvent('probe_start', `Starting Discovery Probe v${PROBE_VERSION}`, {
    outputDir,
    controller: sanitizeUrl(options.controller),
    hasSecret: !!options.secret,
    durationSec: options.duration,
  });

  let wsConn = null;
  let wsTraffic = null;
  let durationTimer = null;
  let shutdownPromise = null;

  async function performShutdown(reason = 'completed', errorMessage = null) {
    if (isShuttingDown) {
      return shutdownPromise;
    }
    isShuttingDown = true;

    shutdownPromise = (async () => {
      if (durationTimer) {
        clearTimeout(durationTimer);
        durationTimer = null;
      }

      logEvent('probe_shutdown', `Shutting down probe (reason=${reason})`, {
        errorMessage,
      });

      // 1. 关闭 WebSocket
      if (wsConn) {
        try {
          if (wsConn.readyState === WebSocket.OPEN || wsConn.readyState === WebSocket.CONNECTING) {
            wsConn.close(1000, 'Probe shutdown');
          }
        } catch {}
      }
      if (wsTraffic) {
        try {
          if (wsTraffic.readyState === WebSocket.OPEN || wsTraffic.readyState === WebSocket.CONNECTING) {
            wsTraffic.close(1000, 'Probe shutdown');
          }
        } catch {}
      }

      // 2. 异步等待所有文件流完成应用层写缓冲 flush (Writable stream finish/close)
      const flushTimeoutMs = 3000;
      let flushTimedOut = false;
      let flushTimer = null;

      const streamsPromise = Promise.all([
        waitStreamFinish(connStream),
        waitStreamFinish(trafficStream),
        waitStreamFinish(eventStream),
      ]).then(() => {
        if (!flushTimedOut) {
          sessionEvidence.flush.completed = true;
        }
      }).catch((err) => {
        sessionEvidence.flush.error = err.message;
      });

      const timeoutPromise = new Promise((resolve) => {
        flushTimer = setTimeout(() => {
          if (!sessionEvidence.flush.completed) {
            flushTimedOut = true;
            sessionEvidence.flush.timedOut = true;
          }
          resolve();
        }, flushTimeoutMs);
      });

      await Promise.race([streamsPromise, timeoutPromise]);
      if (flushTimer) {
        clearTimeout(flushTimer);
        flushTimer = null;
      }

      // 3. 计算 Evidence Quality
      const issues = [];
      if (sessionEvidence.preflight.status !== 'success') {
        issues.push(`Preflight failed: ${sessionEvidence.preflight.error || 'unknown error'}`);
      }
      if (!sessionEvidence.channels.connections.opened) {
        issues.push('Connections WebSocket channel was never opened');
      }
      if (!sessionEvidence.channels.traffic.opened) {
        issues.push('Traffic WebSocket channel was never opened');
      }
      if (sessionEvidence.channels.connections.framesReceived === 0) {
        issues.push('Connections channel received 0 frames');
      }
      if (sessionEvidence.channels.traffic.framesReceived === 0) {
        issues.push('Traffic channel received 0 frames');
      }
      if (sessionEvidence.channels.connections.closedUnexpectedly) {
        issues.push('Connections WebSocket closed unexpectedly during session');
      }
      if (sessionEvidence.channels.traffic.closedUnexpectedly) {
        issues.push('Traffic WebSocket closed unexpectedly during session');
      }
      if (sessionEvidence.channels.connections.parseErrors > 0) {
        issues.push(`Connections channel encountered ${sessionEvidence.channels.connections.parseErrors} JSON parse errors`);
      }
      if (sessionEvidence.channels.traffic.parseErrors > 0) {
        issues.push(`Traffic channel encountered ${sessionEvidence.channels.traffic.parseErrors} JSON parse errors`);
      }
      if (sessionEvidence.channels.connections.writeErrors > 0) {
        issues.push(`Connections file stream encountered ${sessionEvidence.channels.connections.writeErrors} write errors`);
      }
      if (sessionEvidence.channels.traffic.writeErrors > 0) {
        issues.push(`Traffic file stream encountered ${sessionEvidence.channels.traffic.writeErrors} write errors`);
      }
      if (sessionEvidence.channels.events.writeErrors > 0) {
        issues.push(`Events file stream encountered ${sessionEvidence.channels.events.writeErrors} write errors`);
      }
      if (sessionEvidence.flush.timedOut) {
        issues.push(`File write stream flush timed out after ${flushTimeoutMs}ms`);
      }
      if (sessionEvidence.fatalErrors.length > 0) {
        issues.push(`Encountered ${sessionEvidence.fatalErrors.length} fatal/unhandled errors`);
      }

      const isHealthySession = issues.length === 0;

      // 4. 写出最终 manifest.json
      const endTime = new Date();
      const manifest = {
        probeVersion: PROBE_VERSION,
        sessionId,
        startTime: startTime.toISOString(),
        endTime: endTime.toISOString(),
        durationSeconds: Math.round((endTime.getTime() - startTime.getTime()) / 1000),
        controllerUrl: sanitizeUrl(options.controller),
        hasSecret: !!options.secret,
        requestedConnectionsIntervalMs: options.connectionsInterval,
        status: reason,
        errorMessage,
        evidenceQuality: {
          isHealthySession,
          issues,
        },
        preflight: sessionEvidence.preflight,
        channels: sessionEvidence.channels,
        flush: sessionEvidence.flush,
        eventsCount: sessionEvidence.eventsCount,
        fatalErrors: sessionEvidence.fatalErrors,
        files: {
          manifest: 'manifest.json',
          connections: 'connections.ndjson',
          traffic: 'traffic.ndjson',
          events: 'events.ndjson',
        },
      };

      try {
        fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2), 'utf8');
      } catch (err) {
        console.error(`[ERROR] Failed to write manifest.json: ${err.message}`);
      }

      console.log(`\n========================================`);
      console.log(`Probe 采集结束 (reason=${reason})`);
      console.log(`健康会话 (Healthy Session): ${isHealthySession ? 'YES' : 'NO'}`);
      if (!isHealthySession) {
        console.log(`存在问题 (Issues):`);
        issues.forEach((iss) => console.log(`  - ${iss}`));
      }
      console.log(`总抓取: ${sessionEvidence.channels.connections.framesReceived} 帧 Connections, ${sessionEvidence.channels.traffic.framesReceived} 帧 Traffic`);
      console.log(`样本保存目录: ${outputDir}`);
      console.log(`========================================\n`);

      // Exit Code 规则：只有完全 Healthy 且非显式失败才为 0；Unhealthy / Failed 一律非 0 (1)
      const finalExitCode = isHealthySession && reason !== 'failed' ? 0 : 1;
      process.exitCode = finalExitCode;
      process.exit(finalExitCode);
    })();

    return shutdownPromise;
  }

  // 捕获系统信号与未捕获异常
  process.once('SIGINT', () => {
    console.log('\n[INFO] 接收到中断信号 (SIGINT/Ctrl+C)，正在安全 flush 并退出...');
    performShutdown('interrupted_by_user');
  });

  process.once('SIGTERM', () => {
    performShutdown('terminated');
  });

  process.on('uncaughtException', (err) => {
    const msg = err instanceof Error ? err.message : String(err);
    try {
      logEvent('uncaught_exception', msg, { stack: err?.stack });
      sessionEvidence.fatalErrors.push(`uncaughtException: ${msg}`);
    } catch {}
    performShutdown('failed', msg);
  });

  process.on('unhandledRejection', (reason) => {
    const msg = reason instanceof Error ? reason.message : String(reason);
    try {
      logEvent('unhandled_rejection', msg);
      sessionEvidence.fatalErrors.push(`unhandledRejection: ${msg}`);
    } catch {}
    performShutdown('failed', msg);
  });

  // 1. Preflight: 检查 /version
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

    sessionEvidence.preflight.httpStatus = res.status;

    if (!res.ok) {
      const body = await res.text().catch(() => '');
      throw new Error(`Controller returned HTTP ${res.status}: ${body.slice(0, 100)}`);
    }

    const versionData = await res.json();
    sessionEvidence.preflight.status = 'success';
    sessionEvidence.preflight.mihomoVersion = versionData;
    logEvent('preflight_success', `Connected to Mihomo successfully`, versionData);
  } catch (err) {
    sessionEvidence.preflight.status = 'failed';
    sessionEvidence.preflight.error = err.message;
    logEvent('preflight_failed', `Failed to connect to External Controller: ${err.message}`);
    console.error(`\n[ERROR] 无法连接到 External Controller: ${err.message}`);
    console.error(`请确认 Mihomo / FLClash 是否运行，且已在配置中开启 external-controller 端口与正确的 secret。\n`);
    
    await performShutdown('failed', err.message);
    return;
  }

  // 2. 建立 WebSocket 采集连接
  const wsConnectionsUrl = buildWsUrl(options.controller, '/connections', options.secret, {
    interval: options.connectionsInterval,
  });
  const wsTrafficUrl = buildWsUrl(options.controller, '/traffic', options.secret);

  logEvent('ws_connecting', `Connecting to /connections and /traffic WebSockets`);

  // Connections WS
  try {
    wsConn = new WebSocket(wsConnectionsUrl);
    wsConn.onopen = () => {
      sessionEvidence.channels.connections.opened = true;
      logEvent('ws_open', 'WebSocket /connections connected');
    };
    wsConn.onmessage = (event) => {
      if (isShuttingDown) return;
      sessionEvidence.channels.connections.framesReceived++;
      const receivedAt = new Date().toISOString();
      let payload;
      try {
        payload = JSON.parse(event.data);
      } catch {
        sessionEvidence.channels.connections.parseErrors++;
        payload = { raw: event.data, error: 'JSON_PARSE_ERROR' };
      }
      connStream.write(JSON.stringify({ receivedAt, frame: payload }) + '\n');
    };
    wsConn.onerror = (err) => {
      if (isShuttingDown) return;
      logEvent('ws_error', `WebSocket /connections error: ${err.message || 'unknown error'}`);
    };
    wsConn.onclose = (event) => {
      if (isShuttingDown) return;
      logEvent('ws_close', `WebSocket /connections closed (code=${event.code}, reason=${event.reason || 'none'})`);
      sessionEvidence.channels.connections.closedUnexpectedly = true;
      logEvent('warn', 'WebSocket /connections unexpectedly closed by server');
    };
  } catch (e) {
    logEvent('ws_create_error', `Failed to initialize /connections WS: ${e.message}`);
  }

  // Traffic WS
  try {
    wsTraffic = new WebSocket(wsTrafficUrl);
    wsTraffic.onopen = () => {
      sessionEvidence.channels.traffic.opened = true;
      logEvent('ws_open', 'WebSocket /traffic connected');
    };
    wsTraffic.onmessage = (event) => {
      if (isShuttingDown) return;
      sessionEvidence.channels.traffic.framesReceived++;
      const receivedAt = new Date().toISOString();
      let payload;
      try {
        payload = JSON.parse(event.data);
      } catch {
        sessionEvidence.channels.traffic.parseErrors++;
        payload = { raw: event.data, error: 'JSON_PARSE_ERROR' };
      }
      trafficStream.write(JSON.stringify({ receivedAt, frame: payload }) + '\n');
    };
    wsTraffic.onerror = (err) => {
      if (isShuttingDown) return;
      logEvent('ws_error', `WebSocket /traffic error: ${err.message || 'unknown error'}`);
    };
    wsTraffic.onclose = (event) => {
      if (isShuttingDown) return;
      logEvent('ws_close', `WebSocket /traffic closed (code=${event.code}, reason=${event.reason || 'none'})`);
      sessionEvidence.channels.traffic.closedUnexpectedly = true;
    };
  } catch (e) {
    logEvent('ws_create_error', `Failed to initialize /traffic WS: ${e.message}`);
  }

  if (options.duration > 0) {
    logEvent('timer_set', `Probe will automatically stop after ${options.duration} seconds`);
    durationTimer = setTimeout(() => {
      performShutdown('duration_elapsed');
    }, options.duration * 1000);
  }
}

main().catch((err) => {
  console.error('[FATAL]', err);
  process.exit(1);
});
