#!/usr/bin/env node

/**
 * ProxyLens Phase 0 — Controlled NTP / UDP Trigger
 * 
 * 最小化受控 NTP 测试流量触发脚本。
 * 基于 Node.js 原生 dgram 发送标准 48 字节 NTP 请求并接收回复。
 * 仅用于产生受控测试样本，不修改任何系统配置。
 */

import dgram from 'node:dgram';

const host = process.argv[2] || 'ntp.aliyun.com';
const port = 123;

console.log(`[NTP_TRIGGER] Sending standard NTP client packet to ${host}:${port}...`);

const client = dgram.createSocket('udp4');

// 构造标准 48 字节 NTP 客户端请求报文 (LI=0, VN=4, Mode=3 -> 0x23)
const packet = Buffer.alloc(48);
packet[0] = 0x23;

const timeout = setTimeout(() => {
  console.log('[NTP_TRIGGER] Timeout waiting for NTP response');
  client.close();
  process.exit(0);
}, 3000);

client.send(packet, 0, packet.length, port, host, (err) => {
  if (err) {
    console.error(`[NTP_TRIGGER] Send error: ${err.message}`);
    clearTimeout(timeout);
    client.close();
    process.exit(1);
  }
  console.log(`[NTP_TRIGGER] NTP request packet sent successfully (${packet.length} bytes)`);
});

client.on('message', (msg, rinfo) => {
  clearTimeout(timeout);
  console.log(`[NTP_TRIGGER] Received NTP response from ${rinfo.address}:${rinfo.port} (${msg.length} bytes)`);
  client.close();
  process.exit(0);
});

client.on('error', (err) => {
  console.error(`[NTP_TRIGGER] Socket error: ${err.message}`);
  clearTimeout(timeout);
  client.close();
  process.exit(1);
});
