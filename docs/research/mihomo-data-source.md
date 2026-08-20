# Mihomo 数据源实测调研报告 (mihomo-data-source.md)

> 本报告记录 ProxyLens Phase 0 数据源验证阶段在真实宿主环境下的实测证据。所有结论严格遵循 `Documented / Observed / Inferred` 三级证据边界，未经验证的推断严禁作为既定事实。

---

## 1. 测试环境与版本溯源 (Test Environment & Version Provenance)

- **操作系统 (OS)**: Windows 11 Pro 64-bit (AMD64)
- **Live 运行时实例 (Live FLClash Core)** `[Observed]`:
  - 前端 GUI: FLClash (PID 13436)
  - 核心进程: FlClashCore (PID 20320)
  - External Controller: `http://127.0.0.1:9090` (Localhost 绑定)
  - 内核版本: **Mihomo Meta v1.10.0** (GET `/version` 返回: `{"meta": true, "version": "1.10.0"}`)
  - 这是当前系统中真正承载日常用户 TUN 与代理流量的活跃实例。
- **独立测试实例 (Standalone Test Instance - Historical)**:
  - 早期在 `E:\v2rayN\...\mihomo.exe` 运行的独立测试进程为 v1.19.12，仅用于探针离线兼容性冒烟测试，不参与 Live TUN 流量审计。
- **TUN 配置**: 启用 (`device: FlClash`, `stack: mixed`, `enhanced-mode: fake-ip`, `fake-ip-range: 198.18.0.1/16`)
- **进程匹配模式**: `find-process-mode: always`

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
  - 同时在同一会话中捕获到宿主系统的日常真实连接（如 `chrome.exe`、`cloudmusic.exe`、`agy.exe` 等 553 个活跃连接）。

---

## 4. Phase 0C-1: DIRECT / PROXY 基线实测

### 4.1 DIRECT 基线样本

- **测试会话**: `tmp/discovery/0c1-direct-2026-08-20T15-27-38-844Z`
- **操作意图**: 触发明确走直连的分流请求 (`curl.exe https://www.baidu.com -I`)
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
  1. `chains` 为单元素数组 `["DIRECT"]`；
  2. `rule` 与 `rulePayload` 明确给出了分流规则依据 (`RuleSet` / `cn_domain`)；
  3. `process` 与 `processPath` 在 TUN 模式下精确识别 (`C:\Windows\System32\curl.exe`)；
  4. 在 Fake-IP 模式下，`metadata.destinationIP` 为空字符串，真实的物理解析 IP 记录在 `metadata.remoteDestination`。

### 4.2 PROXY 基线样本

- **测试会话**: `tmp/discovery/0c1-proxy-sample-2026-08-20T15-28-46-409Z`
- **操作意图**: 触发明确走代理的分流请求 (`curl.exe --limit-rate 20k https://raw.githubusercontent.com/... -o nul`)
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
  3. `rule` 为 `"DomainSuffix"`，`rulePayload` 为 `"githubusercontent.com"`；
  4. 该连接 ID 在其被观察的连续 3 个快照帧中保持稳定，其 `upload` / `download` 在观察期内保持单调递增。

---

## 5. Phase 0C-2: UDP / NTP / QUIC 协议归因实测

### 5.1 C2-A 空闲后台 UDP (Idle Background UDP)

- **测试会话**: `tmp/discovery/0c2-idle-udp-2026-08-20T15-37-24-042Z` (30 秒空闲观测)
- **统计结果 (UDP 过滤分析, 108 个独立连接 ID)** `[Observed]`:
  - `metadata.process`: **100.0%** (108 / 108)
  - `metadata.processPath`: **100.0%** (108 / 108)
  - `metadata.destinationIP`: **100.0%** (108 / 108)
  - `metadata.destinationPort`: **100.0%** (108 / 108)
  - `rule` & `rulePayload`: **100.0%** (108 / 108)
  - `chains`: **100.0%** (107 次 `["DIRECT"]`，1 次 `["🇭🇰 香港W06 | x0.8", "入口选择"]`)
- **关键发现** `[Observed]`:
  - 在空闲状态下，后台应用（如 `cloudmusic.exe`、`syncthing.exe`）发起的后台 UDP 探测（STUN、P2P 端口 6940/6939/20494/3478 等）在 Windows TUN 下均被**完整归因到进程名与绝对路径**。

### 5.2 C2-B 受控 NTP / UDP 123 实验 (Controlled NTP)

- **测试会话**: `tmp/discovery/0c2-controlled-ntp-2026-08-20T15-38-17-320Z`
- **操作意图**: 通过受控测试脚本 (`tools/discovery/scenarios/ntp-trigger.mjs`) 向公开 NTP 服务器 (`ntp.aliyun.com:123`) 发送单次标准 48 字节 NTP 请求并接收 48 字节响应。
- **典型原始连接样本 (Raw Sample - 脱敏引用)** `[Observed]`:
  ```json
  {
    "id": "a2968214-ae40-483b-a456-b3a6e950d102",
    "metadata": {
      "network": "udp",
      "type": "Tun",
      "sourceIP": "198.18.0.1",
      "destinationIP": "203.107.6.88",
      "sourcePort": "62614",
      "destinationPort": "123",
      "inboundIP": "",
      "inboundPort": "0",
      "inboundName": "DEFAULT-TUN",
      "inboundUser": "",
      "host": "ntp.aliyun.com",
      "dnsMode": "redir-host",
      "uid": 0,
      "process": "node.exe",
      "processPath": "D:\\Node.js\\Node.js\\node.exe",
      "remoteDestination": "",
      "sniffHost": ""
    },
    "upload": 48,
    "download": 48,
    "start": "2026-08-20T23:38:19.0193841+08:00",
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
  1. **精确归因**: 进程 (`D:\Node.js\Node.js\node.exe`)、目标域名 (`ntp.aliyun.com`)、目标 IP (`203.107.6.88:123`)、规则 (`RuleSet / cn_domain`) 与出站方式 (`["DIRECT"]`) 100% 完整。
  2. **精确流量**: `upload: 48` 字节，`download: 48` 字节，与实际发出的 NTP 报文大小精确相符。
  3. **UDP Pseudo-Connection 留存现象**: 虽然应用层 UDP Socket 在收到响应后立即 `client.close()` 并退出，但该 UDP Flow 连接 ID 在 Mihomo 内核活跃连接表中持续留存了超过 6 秒（跨越 6 个以上的连续快照帧）。表明 Mihomo 内部维护了基于空闲超时的 UDP Pseudo-connection 生命周期。

### 5.3 C2-C QUIC / HTTP3 实验 (QUIC / UDP 443)

- **实测状态**: **Not Observed / Environment could not reliably trigger** `[Observed]`
- **实测事实与原因**:
  - Windows 系统内置 `curl.exe` (v8.21.0) 未编译 HTTP/3 / QUIC 协议支持标志；
  - 在当前捕获的所有会话中，未出现 `metadata.network === "udp"` 且 `metadata.destinationPort === "443"` 的网络连接；
  - 严格保持证据边界，不伪造结论，留待具备 HTTP/3 客户端环境时再次验证。

---

## 6. 协议差异与字段对照表 (Protocol Comparison Table)

| 字段名称 (Field) | TCP 基线 (`curl` / HTTPS) | UDP / NTP (`ntp.aliyun.com:123`) | UDP / 443 (QUIC) | 观察说明 `[Observed]` |
| :--- | :--- | :--- | :--- | :--- |
| `metadata.process` | `curl.exe` / `chrome.exe` | `node.exe` | *Not Observed* | TCP 与 UDP 均具备良好的进程识别能力。 |
| `metadata.processPath`| 完整绝对路径 | 完整绝对路径 | *Not Observed* | 路径信息完整。 |
| `metadata.host` | 存在 (如 `www.baidu.com`) | 存在 (`ntp.aliyun.com`) | *Not Observed* | 在域名请求下均正常填入 host。 |
| `metadata.destinationIP`| `""` (Fake-IP 模式) | 填入 IP (`203.107.6.88`) | *Not Observed* | Fake-IP 下 TCP 请求 destinationIP 为空，NTP 则直接填入了解析后的物理 IP。 |
| `metadata.remoteDestination`| 填入物理 IP (`183.2.172.177`) | `""` (空字符串) | *Not Observed* | TCP 模式通过 remoteDestination 表达真实远端 IP，NTP 直接记录在 destinationIP。 |
| `metadata.destinationPort` | `443` | `123` | *Not Observed* | 均完整呈现。 |
| `rule` | `RuleSet` / `DomainSuffix` | `RuleSet` | *Not Observed* | 分流规则类型表达一致。 |
| `rulePayload` | `cn_domain` / 后缀名 | `cn_domain` | *Not Observed* | 规则匹配内容表达一致。 |
| `chains` | `["DIRECT"]` 或 `[出站节点, 策略组...]` | `["DIRECT"]` | *Not Observed* | 分流链路结构一致。 |
| `upload` / `download` | 累积字节数 | `48 / 48` 字节 | *Not Observed* | 均可准确记录传输字节数。 |

---

## 7. 核心问题回答 (Core Questions Answered)

1. **Windows TUN 下 UDP 连接是否通常具有 process / processPath？**
   - **是** `[Observed]`。在空闲 UDP 样本（108 个连接）与受控 NTP 样本中，`process` 与 `processPath` 的覆盖率均为 **100.0%**。
2. **NTP / UDP 123 是否能够完整归因？**
   - **能** `[Observed]`。进程、目标域名、解析 IP、端口 123、分流规则与 Chains 均完整可追溯。
3. **UDP 连接中 host 与 remoteDestination 的表现是什么？**
   - **表现差异** `[Observed]`：TCP 连接在 Fake-IP 下使用 `remoteDestination` 记录物理目标，`destinationIP` 为空；而受控 UDP NTP 连接中，`destinationIP` 直接记录物理目标 IP，`remoteDestination` 为空。两者呈现不同的字段填充模式。
4. **UDP upload / download 是否存在并随快照更新？**
   - **是** `[Observed]`。在受控 NTP 中精确记录为 `48B / 48B`；在持续存在的 UDP 流中单调不减。
5. **UDP pseudo-connection 在请求完成后会不会继续存在若干快照？**
   - **会** `[Observed]`。单次 48 字节 NTP 请求完成后，连接记录在后续 6 秒（6 帧以上）中持续处于快照表中，说明内核具备 UDP flow timeout 保持机制。
6. **当前环境是否成功观察到 UDP/443？**
   - **否 (Not Observed)** `[Observed]`。当前环境客户端未触发 QUIC/UDP:443 流量。
7. **如果观察到 UDP/443，证据是否足够称为 QUIC/HTTP3？**
   - **不足够直接等同** `[Inferred]`。仅凭端口 UDP/443 只能证明网络层为 UDP 443，必须结合应用层或客户端特征才能断言为 QUIC/HTTP3。
8. **UDP 与 TCP 的字段缺失模式是否有明显区别？**
   - **有** `[Observed]`。主要体现在 Fake-IP 模式下物理 IP 的存放位置（TCP 存放在 `remoteDestination`，UDP NTP 存放在 `destinationIP`）。

---

## 8. 开放问题与后续阶段 (Open Questions & Roadmap)

- [ ] **TODO (Phase 0C-3)**: 长连接 (Long-lived connections) 跨帧累积流量的单调性与增量计算边界；
- [ ] **TODO (Phase 0C-4)**: 链式代理 (Multi-hop / Relay) 与节点切换时的 `chains` 动态变化；
- [ ] **TODO (Phase 0C-5)**: Mihomo 内核重启与 Controller 断开时的连接 ID 状态机与计数器重置行为；
- [ ] **TODO (Phase 0C-6)**: `/traffic` 实时速率与 `/connections` 连接增量的一致性对账机制。
