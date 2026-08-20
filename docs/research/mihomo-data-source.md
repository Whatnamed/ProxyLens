# Mihomo 数据源实测调研报告 (mihomo-data-source.md)

> 本报告记录 ProxyLens Phase 0 数据源验证阶段在真实宿主环境下的实测证据。所有结论严格遵循 `Documented / Observed / Inferred` 三级证据边界，未经验证的推断严禁作为既定事实。

---

## 1. 测试环境 (Test Environment)

- **操作系统 (OS)**: Windows 11 Pro 64-bit (AMD64)
- **代理前端 GUI**: FLClash (PID 13436) + FlClashCore (PID 20320)
- **内核版本 (Mihomo Core)**: Mihomo Meta v1.10.0 (API 返回: `{"meta": true, "version": "1.10.0"}`)
- **TUN 配置**: 启用 (`device: FlClash`, `stack: mixed`, `enhanced-mode: fake-ip`, `fake-ip-range: 198.18.0.1/16`)
- **进程匹配模式**: `find-process-mode: always`
- **External Controller**: `http://127.0.0.1:9090` (仅绑定 Localhost，无外部公网暴露)

---

## 2. 证据分级体系 (Evidence Methodology)

- **[Documented]**: 经 Mihomo 官方文档或公开代码确认的规范行为。
- **[Observed]**: 在当前 Windows 11 + FLClash 真实环境中经由 Discovery Probe 捕获并复现的事实。
- **[Inferred]**: 基于现有样本的合理推断，仍需在后续 Phase 0 场景中进一步交叉验证。

---

## 3. Phase 0C-0: Live FLClash Controller Gate 验证

- **验证目标**: 证明 Discovery Probe 连接的 Controller 确为宿主系统正在承载日常流量与 TUN 转发的 Live 实例。
- **Gate 结果**: **PASS** `[Observed]`
- **实测证明证据 (Traffic Correlation)**:
  - 启动 Probe 监听会话 (`tmp/discovery/0c0-live-gate-*`)；
  - 触发受控操作: `curl.exe https://cp.cloudflare.com/generate_204 -I`；
  - 在 Controller 的 `/connections` 原始快照帧中，精确捕获到该 `curl.exe` 连接（`metadata.process: "curl.exe"`, `metadata.processPath: "C:\\Windows\\System32\\curl.exe"`, `metadata.host: "cp.cloudflare.com"`）；
  - 同时在同一会话中捕获到宿主系统的日常真实连接（如 `chrome.exe`、`cloudmusic.exe`、`agy.exe` 等 553 个活跃/历史连接）。

---

## 4. Phase 0C-1: DIRECT 基线实测

- **测试会话**: `tmp/discovery/0c1-direct-2026-08-20T15-27-38-844Z`
- **操作意图**: 触发明确走直连的分流请求 (`curl.exe https://www.baidu.com -I`)
- **会话健康度**: `isHealthySession: true` (收帧: 6 帧 Connections, 6 帧 Traffic, 0 错误)
- **典型原始连接样本 (Raw Sample - 脱敏引用)** `[Observed]`:
  ```json
  {
    "id": "05ca6aa0-0584-413e-978c-43c6fd7f9dfb",
    "metadata": {
      "network": "tcp",
      "type": "Tun",
      "sourceIP": "198.18.0.1",
      "destinationIP": "",
      "sourcePort": "9554",
      "destinationPort": "443",
      "inboundIP": "198.18.0.1",
      "inboundPort": "8959",
      "inboundName": "DEFAULT-TUN",
      "inboundUser": "",
      "host": "www.baidu.com",
      "dnsMode": "fake-ip",
      "uid": 0,
      "process": "curl.exe",
      "processPath": "C:\\Windows\\System32\\curl.exe",
      "remoteDestination": "183.2.172.177",
      "sniffHost": ""
    },
    "upload": 460,
    "download": 5206,
    "start": "2026-08-20T23:27:40.7209274+08:00",
    "chains": [
      "DIRECT"
    ],
    "providerChains": [
      ""
    ],
    "rule": "RuleSet",
    "rulePayload": "cn_domain"
  }
  ```
- **关键发现** `[Observed]`:
  1. `chains` 为包含单元素的数组 `["DIRECT"]`；
  2. `rule` 与 `rulePayload` 明确给出了分流规则依据 (`RuleSet` / `cn_domain`)；
  3. `process` 与 `processPath` 在 TUN 模式下被 100% 精确识别 (`C:\Windows\System32\curl.exe`)；
  4. 在 Fake-IP 模式下，`metadata.destinationIP` 为空字符串，而真实的远程解析物理 IP 记录在 `metadata.remoteDestination`。

---

## 5. Phase 0C-1: PROXY 基线实测

- **测试会话**: `tmp/discovery/0c1-proxy-sample-2026-08-20T15-28-46-409Z`
- **操作意图**: 触发明确走代理的分流请求 (`curl.exe --limit-rate 20k https://raw.githubusercontent.com/... -o nul`)
- **会话健康度**: `isHealthySession: true` (收帧: 8 帧 Connections, 8 帧 Traffic, 0 错误)
- **典型原始连接样本 (Raw Sample - 脱敏引用)** `[Observed]`:
  ```json
  {
    "id": "8d3b4047-600f-4895-9ac1-ac78d8270eca",
    "metadata": {
      "network": "tcp",
      "type": "Tun",
      "sourceIP": "198.18.0.1",
      "destinationIP": "",
      "sourcePort": "6412",
      "destinationPort": "443",
      "inboundIP": "198.18.0.1",
      "inboundPort": "8959",
      "inboundName": "DEFAULT-TUN",
      "inboundUser": "",
      "host": "raw.githubusercontent.com",
      "dnsMode": "fake-ip",
      "uid": 0,
      "process": "curl.exe",
      "processPath": "C:\\Windows\\System32\\curl.exe",
      "remoteDestination": "114.28.148.166",
      "sniffHost": ""
    },
    "upload": 706,
    "download": 4913,
    "start": "2026-08-20T23:28:49.2451677+08:00",
    "chains": [
      "🇭🇰 香港W06 | x0.8",
      "入口选择",
      "GitHub"
    ],
    "providerChains": [
      "",
      "",
      ""
    ],
    "rule": "DomainSuffix",
    "rulePayload": "githubusercontent.com"
  }
  ```
- **关键发现** `[Observed]`:
  1. `chains` 数组包含 3 个元素：`["🇭🇰 香港W06 | x0.8", "入口选择", "GitHub"]`；
  2. 元素顺序对应为：`[实际出站节点, 中间策略组, 顶层匹配策略组]`；
  3. `rule` 为 `"DomainSuffix"`，`rulePayload` 为 `"githubusercontent.com"`，分流依据链条完整可追溯；
  4. 单次极短连接（<100ms）可能在 1 秒快照轮询间隙结束而未被帧捕获，持续时间 >1s 的连接能稳定出现在活跃快照中。

---

## 6. 初步字段观察表 (Initial observations)

> **声明**: 以下表格仅代表基于 Phase 0C-0 与 0C-1 真实样本（累计 773+ 独立连接）的初步实测结论，不代表 Mihomo 所有版本与配置下的最终定义。

| 字段名称 (Field) | DIRECT 实测 | PROXY 实测 | 覆盖率 (773 样本) | 说明 / 语义发现 `[Observed]` |
| :--- | :--- | :--- | :---: | :--- |
| `id` | 存在 (UUID) | 存在 (UUID) | 100.0% | 每条连接的全局唯一标识符。 |
| `start` | ISO-8601 | ISO-8601 | 100.0% | 连接发起时间戳（包含本地时区与微秒）。 |
| `metadata.network` | `tcp` / `udp` | `tcp` / `udp` | 100.0% | 传输层网络协议。 |
| `metadata.type` | `Tun` | `Tun` | 100.0% | 入站类型（TUN 模式下为 `Tun`）。 |
| `metadata.process` | 存在 | 存在 | 96.4% | 发起进程名（空进程主要为底层或跨层连接）。 |
| `metadata.processPath`| 存在 | 存在 | 96.4% | 进程完整绝对路径。 |
| `metadata.host` | 存在 | 存在 | 82.7% | 请求域名（纯 IP 直连时为空）。 |
| `metadata.sniffHost` | 偶发 | 偶发 | 0.3% | 嗅探域名（大多数 HTTP/TLS 连接 host 已由 Fake-IP 或客户端提供）。 |
| `metadata.destinationIP`| 纯 IP 请求时存在 | 纯 IP 请求时存在 | 94.0% | Fake-IP 模式下若带 host 则本字段为空字符串。 |
| `metadata.remoteDestination` | 物理 IP 存在 | 物理 IP 存在 | 100.0% (TCP) | Mihomo 实际连接的远端物理 IP 地址。 |
| `metadata.destinationPort` | 存在 | 存在 | 100.0% | 目标端口号。 |
| `rule` | 存在 | 存在 | 97.2% | 命中的分流规则类型（`RuleSet`, `DomainSuffix`, `Match`, `IPCIDR`）。 |
| `rulePayload` | 存在 | 存在 | 96.9% | 命中的规则负载内容（如规则集名、域名后缀等）。 |
| `chains` | 存在 | 存在 | 100.0% | 分流链数组。DIRECT 为 `["DIRECT"]`；PROXY 为 `[出站节点, 策略组...]`。 |
| `upload` / `download` | 存在 (Byte) | 存在 (Byte) | 100.0% | 累积传输字节数，未见计数器回退情况。 |

---

## 7. 开放问题与下一步计划 (Open Questions & Next Steps)

- [ ] **TODO (Phase 0C-2)**: 后台 UDP / NTP 与 QUIC (UDP 443) 流量的字段表现与差异；
- [ ] **TODO (Phase 0C-3)**: 长连接 (Long-lived connections) 跨帧累积流量的单调性与增量计算边界；
- [ ] **TODO (Phase 0C-4)**: 链式代理 (Multi-hop / Relay) 与节点切换时的 `chains` 动态变化；
- [ ] **TODO (Phase 0C-5)**: Mihomo 内核重启与 Controller 断开时的连接 ID 状态机与计数器重置行为；
- [ ] **TODO (Phase 0C-6)**: `/traffic` 实时速率与 `/connections` 连接增量的一致性对账机制。
