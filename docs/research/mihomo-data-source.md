# Mihomo 数据源实测调研报告 (mihomo-data-source.md)

> 本报告记录 ProxyLens Phase 0 数据源验证阶段在真实宿主环境下的实测证据。所有结论严格遵循 `Documented / Observed / Inferred` 三级证据边界，未经验证的推断严禁作为既定事实。

---

## 1. 测试环境与版本溯源 (Test Environment & Version Provenance)

- **操作系统 (OS)**: Windows 11 Pro 64-bit (AMD64)
- **Live 运行时实例 (The Correlated Live FLClash Instance Under Test)** `[Observed]`:
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
  4. 该连接 ID 在其被观察的连续快照帧中保持稳定，其 `upload` / `download` 在观察期内表现为非递减计数器。

---

## 5. Phase 0C-2: UDP / NTP / QUIC 协议归因实测

### 5.1 C2-A 空闲后台 UDP (Idle Background UDP)

- **测试会话**: `tmp/discovery/0c2-idle-udp-2026-08-20T15-37-24-042Z` (30 秒空闲观测)
- **统计结果 (UDP 过滤分析, 108 个独立连接 ID)** `[Observed]`:
  - `metadata.process`: **100.0%** (108 / 108) (限定于本次测试样本)
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
  3. **UDP Pseudo-Connection 留存现象**: 虽然应用层 UDP Socket 在收到响应后立即 `client.close()` 并退出，但该 UDP Flow 连接 ID 在 Mihomo 内核活跃连接表中持续留存了超过 6 秒（跨越 6 个以上的连续快照帧）。`[Inferred]`: 该行为与 Mihomo 内核维护的基于空闲超时的 UDP flow retention 机制一致。

### 5.3 C2-C QUIC / HTTP3 实验 (QUIC / UDP 443)

- **实测状态**: **Not Observed / Environment could not reliably trigger** `[Observed]`
- **实测事实与原因**:
  - Windows 系统内置 `curl.exe` (v8.21.0) 未编译 HTTP/3 / QUIC 协议支持标志；
  - 在当前捕获的所有会话中，未出现 `metadata.network === "udp"` 且 `metadata.destinationPort === "443"` 的网络连接；
  - 严格保持证据边界，不伪造结论，留待具备 HTTP/3 客户端环境时再次验证。

---

## 6. Phase 0C-3: 连接计数器语义实测 (Connection Counter Semantics)

### 6.1 C3-A 稳态长连接连续演变 (Steady-State Connection)

- **测试会话**: `tmp/discovery/0c3a-steady-counter-2026-08-20T15-55-24-930Z`
- **操作意图**: 在 Probe 建立连续稳态监控后，启动受控限速长下载（`curl.exe` 下载 800,000 字节，耗时 18.8 秒）。
- **实测连接时间线 (`trace-connection.mjs` 提取 - 脱敏引用)** `[Observed]`:
  - **Connection ID**: `1e802c93-59ea-4dd0-b427-b9aef80d3b3e`
  - **Start Timestamp**: `2026-08-20T23:55:30.2706716+08:00` (跨越全部 18 帧完全稳定，0 漂移)
  - **时间线片段**:
    - `Frame #6 (15:55:31.082Z)`: Up `467 B`, Down `0 B` (连接刚建立，HTTP 请求发出)
    - `Frame #7 (15:55:32.082Z)`: Up `672 B` (+205), Down `20,198 B` (+20,198)
    - `Frame #8 (15:55:33.082Z)`: Up `672 B` (+0), Down `182,230 B` (+162,032)
    - `Frame #9 (15:55:34.083Z)`: Up `672 B` (+0), Down `476,150 B` (+293,920)
    - `Frame #10 (15:55:35.083Z)`: Up `672 B` (+0), Down `712,686 B` (+236,536)
    - `Frame #14 (15:55:39.083Z)`: Up `672 B` (+0), Down `807,291 B` (+94,605)
    - `Frame #23 (15:55:48.082Z)`: Up `696 B` (+24), Down `807,291 B` (+0) (最后可见快照观测到额外 +24B upload，具体协议语义未知)
- **统计与一致性验证** `[Observed]`:
  - **快照出现帧数**: 连续 18 帧（无中途消失重现）；
  - **First Counter**: `467 B / 0 B`；
  - **Last Counter**: `696 B / 807,291 B`；
  - **Observed Delta (Last - First)**: Upload `+229 B`，Download `+807,291 B`；
  - **Sum of Adjacent Deltas**: Upload `+229 B`，Download `+807,291 B`；
  - **算术一致性**: **EXACT MATCH (PASS)**；
  - **计数器递减检测**: **0 起 (严格单调非递减)**。
- **Connection Closure Tail 观察** `[Observed]`:
  - 下载完成后，连接在 Frame #23 观测到额外 +24B upload；
  - 在 Frame #24 及后续帧中，该 ID 直接从活跃连接快照表中消失；
  - **事实证明**: `/connections` 是当前活跃连接的快照，内核在连接结束时直接将其移出列表，**不存在显式的 closed 状态事件帧**。`last_observed` 并不等同于已证明的最终全量字节（存在 `possible_unobserved_tail`）。

---

### 6.2 C3-B 冷启动基线实测 (Cold-Start Baseline)

- **测试会话**: `tmp/discovery/0c3b-cold-start-2026-08-20T15-56-10-978Z`
- **操作意图**: 在 Probe 启动前 5.8 秒先行启动 1MB 长下载，验证 Probe 中途接入时的首帧表现。
- **实测时间关系与首帧数据 (`trace-connection.mjs` 提取)** `[Observed]`:
  - **Connection ID**: `e0b2eb58-90e4-4262-9c53-230096e2cd2d`
  - **Connection Start**: `2026-08-20T23:56:12.3111342+08:00` (UTC 15:56:12.311)
  - **Probe Session Start**: `2026-08-20T15:56:18.113Z`
  - **时间先后关系**: `connection.start` 明确早于 `probe.startTime` 约 **5.8 秒**；
  - **Probe Frame #0 (首帧) 观察值**:
    - `firstUpload`: `673 B`
    - `firstDownload`: **`743,565 B` (~726 KB)**
  - **Probe 监控期间实际增量 (Frame #0 -> Frame #15)**:
    - Last Download: `1,007,555 B`
    - 实际监控期增量: `1,007,555 - 743,565 = 263,990 B` (~257 KB)。
- **关键发现与架构意义** `[Observed]` / `[Inferred]`:
  - 当采集器中途接入或启动时，第一帧中已存在的连接所携带的 `download` 字段**包含了采集器启动前已经产生的历史数据**；因此首帧观察计数器必须作为 baseline 记录，不得作为已监控期间的增量流量；
  - 若像 MetaCubeXD 那样对所有首次看到的 ID 直接采用 `delta = current_value`，则冷启动第一秒将瞬间产生高达 743KB 的虚假当期增量流量；
  - 这确立了采集器状态机必须严格区分 **Session Bootstrap** 与 **Steady-State New Connection**。

---

### 6.3 首次观测语义对照表 (First-Observation Semantics Table)

| 场景分类 (Scenario) | 采集器前置状态 | 首帧观测计数器 (`firstDownload`) | 时间归属语义 `[Observed / Inferred]` |
| :--- | :--- | :--- | :--- |
| **Steady-State New ID** | 已建立连续健康监控，上一帧不存在 | 通常为 `0 B` 或极小握手值 (`< 50KB`) | 流量发生于上一快照与当前快照之间，**可作为当期监控流量计入**。 |
| **Session Bootstrap ID** | 采集器刚启动，首帧已存在 | 包含历史累计值 (`> 0 B`，本例为 743KB) | 包含启动前的历史流量，**必须作为 baseline 记录，不得计入当期增量**。 |
| **After Gap / Reconnect**| 采集器断线重连或监控中断后 | 包含中断期间的累积量 | `[Inferred]` 必须采用类似 Bootstrap 的基线处理，不可直接作为单帧瞬时增量。 |

---

### 6.4 Phase 1 临时采集状态机规约 (Provisional Collector State Machine)

1. **会话冷启动 (Session Bootstrap)**:
   - 第一帧收到的全部已有连接标记为 `preexisting_at_session_start`；
   - 记录其元数据及 `baseline_upload = upload`, `baseline_download = download`；
   - 本周期增量记为 `0`，后续快照按 `current - last` 计算增量。
2. **稳态新连接 (Steady-State New Connection)**:
   - 在连续监控中新出现的 ID，以 `last_upload = 0`, `last_download = 0` 初始化；
   - 首帧增量 `delta = current_value` 计入当前监控周期。
3. **连接移出快照 (Disappeared from Snapshot)**:
   - 当上一帧存在的 ID 在当前帧消失时，将其标记为 `disappeared_from_snapshot`；
   - 保存 `last_observed_upload / last_observed_download`；
   - 明确 `last_observed != proven final`，标记 `possible_unobserved_tail`。

---

## 7. 核心问题解答 (Phase 0C-3 Core Questions Answered)

1. **upload / download 是否表现为 per-connection cumulative counters？**
   - **是** `[Observed]`。在跨越 18 帧的长连接中，数值单调非递减，且相邻差值之和严格等于末次减初次计数。
2. **同一个 ID 的 start 是否稳定？**
   - **是** `[Observed]`。跨全部快照帧，`start` 时间戳保持精确一致，未发生任何突变或漂移。
3. **稳态新 ID 第一次出现时 counter 是否可能已经 > 0？**
   - **可能** `[Observed]`。在 Frame #6 连接刚建立时，`upload` 已有 `467 B`（HTTP 请求报文大小），因为连接创建发生在两次快照轮询之间。
4. **冷启动 pre-existing ID 第一帧是否包含 Probe 启动前的 bytes？**
   - **是** `[Observed]`。在 C3-B 实测中，首帧即包含启动前产生的 `743,565 B` 历史下载量。
5. **MetaCubeXD “所有 first-seen 都 current-from-zero” 为什么会在 bootstrap 时产生时间归属错误？**
   - **因为混淆了连接累计值与监控期增量** `[Observed]`。冷启动时将历史累积量（如 743KB）当成当期增量，直接造成历史流量虚增和时间轴归属错误。
6. **connection 消失前是否存在已验证的 final closed snapshot？**
   - **否** `[Observed]`。`/connections` 为活跃表，连接结束由内核直接移出，无额外 closed 事件。
7. **我们现在能确定的 first-observation state machine 是什么？**
   - **区分 Bootstrap 与 Steady-State**（详见 6.4 节）。
8. **哪些部分必须留给后续验证？**
   - 短连接采样盲区量化留待 C3-C；重连 Gap 状态恢复留待 C5；与 `/traffic` 总量对账留待 C6。

---

---

---

## 8. Phase 0C-3C: 快照轮询周期与短连接捕获率实测 (Cadence & Capture-Rate Matrix)

### 8.1 采样间隔与 Controller 数据流开销 (Cadence & Throughput Benchmark) `[Observed]`

- **测试工具**: `tools/discovery/analyze-cadence.mjs`
- **Controller 特性验证**: Mihomo Meta v1.10.0 原生支持向 `/connections` WebSocket 传递 `?interval=<ms>` 查询参数 `[Documented / Observed]`。
- **实测性能与吞吐量矩阵 (236 并发活跃连接)**:
  - `default (~1000ms)`: 平均采样间隔 `1000.3ms` (抖动 < 1ms)；数据吞吐量 `132.8 KB/s`；
  - `500ms`: 平均采样间隔 `500.2ms`；数据吞吐量 `277.5 KB/s`；
  - `250ms`: 平均采样间隔 `250.2ms`；数据吞吐量 `577.2 KB/s`。
- **定位**: 证实了通过 URL 参数可调节 Controller 快照推送周期。这些间隔作为 **Phase 1 Collector 性能基准测试候选值 (Benchmark Candidate Intervals)**，最终生产推荐需结合 Collector 实际 CPU/RAM/GC/DB 写入负载综合决定。

---

### 8.2 DIRECT / PROXY 快照捕获率矩阵 (Capture-Rate Matrix, `N=600` TCP Requests, 12 Trials) `[Observed]`

- **实验方法**:
  - 执行 3 种快照间隔 (`default`, `500ms`, `250ms`) × 2 条路由 (`direct`, `proxy`) × 2 轮独立试验 = **12 Trials**（每个 Trial 发起 50 个严格独立的非 Keep-Alive 短请求，总计 600 个请求）；
  - 以操作系统内核实际完成 TCP 握手的 `eligibleConnectedCount = 50` 为基准分母；
  - 强制 1-to-1 唯一性映射、Probe 窗口全生命周期覆盖（`windowViolations = 0`）与 Raw chains 路由验证（`routeMismatches = 0`, `routeUnknown = 0`, `ambiguous = 0`）。

#### 聚合路由 × 快照间隔对比表 (`N=100` per Cell) `[Observed]`

| 路由与快照间隔 | 试验总样本 (N) | 成功捕获数 (Matched) | 快照盲区漏抓数 (Missed) | 快照捕获率 (Capture Rate) | 单帧捕获占比 (1 Frame Presence) |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **DIRECT @ default (1000ms)** | 100 | 14 | 86 | **14.0%** (盲区 86.0%) | 14 / 14 (100.0%) |
| **PROXY @ default (1000ms)** | 100 | 28 | 72 | **28.0%** (盲区 72.0%) | 27 / 28 (96.4%) |
| **DIRECT @ 500ms** | 100 | 28 | 72 | **28.0%** (盲区 72.0%) | 28 / 28 (100.0%) |
| **PROXY @ 500ms** | 100 | 52 | 48 | **52.0%** (盲区 48.0%) | 49 / 52 (94.2%) |
| **DIRECT @ 250ms** | 100 | 55 | 45 | **55.0%** (盲区 45.0%) | 55 / 55 (100.0%) |
| **PROXY @ 250ms** | 100 | 86 | 14 | **86.0%** (盲区 14.0%) | 73 / 86 (84.9%) |

#### 存活时长分布 (Duration Bins Summary) `[Observed]`
- `<100ms`: 样本量极少或瞬间关闭，捕获率接近 0%；
- `100–250ms`: 在 250ms 快照下捕获率为 **55.9%**，在 1000ms 下仅 **15.4%**；
- `250–500ms`: 在 250ms 快照下捕获率为 **87.2%**，在 1000ms 下仅 **22.9%**；
- `>=500ms`: 跨越快照周期的连接捕获率提升至 **60%~100%**。

#### 核心审计结论 `[Observed / Inferred]`
1. **短连接快照盲区是只读轮询架构的固有物理限制**：在默认 1000ms 下，短连接漏抓率为 **72%~86%**；缩短周期至 250ms 能将 DIRECT 捕获率提升至 **55.0%**，PROXY 捕获率提升至 **86.0%**。
2. **捕获的短连接呈现明显的单帧捕获特征**：在被捕获的短连接中，DIRECT 单帧占比为 100.0%，PROXY 单帧占比为 84.9%~96.4%，再次验证单帧首次观察字节必须计入增量。

---

## 9. Phase 0C-6: 全局流量对账与分层归因模型 (Global Accounting & Hierarchical Attribution) `[Observed]`

### 9.1 分层归因公式与残差模型

- **已知应用归因流量 (Known App Attributed)**: $\sum \Delta(\text{KnownAppConnection})$（具备确凿进程与规则）
- **未配对缺归因流量 (Unpaired Missing Attribution)**: $\sum \Delta(\text{MissingAttrConnection})$（保留在独立观测中，绝不冒充已知应用）
- **确认中继底层连接 (Confirmed Relay Duplicate)**: 仅在时间重叠、流量高度吻合且存在结构包含关系时去重
- **唯一独立观测总和 (Unique Observed)**: $\text{KnownApp} + \text{UnpairedMissingAttr} + \text{OtherUnique}$
- **残差 (Residual)**: $\text{Residual} = \Delta(\text{uploadTotal}) - \text{UniqueObserved}$

#### 12 轮会话与稳态长连接对账数据 `[Observed]`

- **稳态长连接场景 (`A1-default`, 100 并发连接)**:
  - 全局上传: `1,826,942 B` | 应用归因上传: `1,826,105 B` | **上传残差仅 837 B (0.05%)**；
  - `/traffic` 速率近似时间积分: `1,826,700 B` | **与全局上传误差仅 -0.01%**。
- **突发短连接矩阵聚合 (12 trials, N=600 requests)**:
  - 严格分层统计证明未配对缺归因流量得到完整保留；
  - `/traffic` 近似积分偏差在各采样周期下收敛在 **-1.7% ~ +1.7%** 之间。

---

### 9.2 链式代理底层连接双重计数与 Relay Candidate 配对模型 `[Observed / Provisional]`

- **机理证实**: 当 Mihomo 配置中继/链式代理时，`/connections` 列表中同时存在应用层逻辑连接与 Mihomo 发往第一跳中继节点的底层连接，两者的流量高度吻合。
- **严谨去重规约 (Relay Candidate & Pairing Model)**:
  - 严禁简单地把“缺 process + 缺 rule”普遍当成 relay 过滤；
  - 必须将其先标记为 `relay_candidate`，只有在会话中找到时间重叠、流量高度吻合且存在结构链条关系的配对应用连接时，才被判定为 `CONFIRMED_RELAY_DUPLICATE` 并予以去重；
  - 未配对成功的候选连接保留为 `unpaired_missing_attribution`，计入待核查流量。

---

## 10. Phase 0C-4: 路由证据盘点与动态节点切换实测 (Routing & Dynamic Switching) `[Observed / Scoped]`

### 10.1 静态路由模式覆盖 (27 Distinct Patterns)
基于 78,000+ connection snapshot observations 的聚合盘点，覆盖全部 7 种规则类型（`RuleSet`, `DomainSuffix`, `Domain`, `IPCIDR`, `GeoSite`, `Network`, `Match`）。

### 10.2 动态节点切换受控实测 `[Scoped Observed / Provisional]`
- **测试工具**: `tools/discovery/scenarios/switch-node.mjs`（带 `--allow-live-mutation`、二次 GET 验证与 Best-effort 回滚保护）、`tools/discovery/analyze-dynamic-switch.mjs`
- **实测事实**:
  1. **已有长连接不可变性**: 在受控下载进行中切换策略组节点（`🇭🇰 香港W06 | x0.8 -> 🇭🇰 香港W01`），已有存活连接的 `chains` 字段在生命周期内保持稳定（0 突变），继续沿原路径完成传输；
  2. **新连接链路即时迁移**: 切换后新发起的请求，其 `chains[0]` 立即变为新选中的物理节点 `🇭🇰 香港W01`。
- **代理链拓扑因果顺序规约 (Confirmed Hop Order)**:
  - **`chains[0]`**: **最终物理出站节点 (Physical Egress Node / DIRECT)**
  - **`chains[1..length - 2]`**: **级联策略选择组 (Intermediate Policy Selectors)**
  - **`chains[length - 1]`**: **分流规则匹配命中的顶层策略组 (Top-Level Rule Target Group)**
  - UI 呈现统一采用：`chains.slice().reverse()`。

---

## 11. Phase 0C-5: 生命周期、配置更新与 Monitoring Gap 实测 (Lifecycle & Gaps)

### 11.1 4.46s Monitoring Gap 断线重连实测 `[Observed]`
- **测试会话**: `gap-experiment-2026-08-20T17-14-26-244Z`
- **实测事实**:
  - `coverageGap`: `2026-08-20T17:14:30.437Z -> 17:14:34.900Z` (4.46s)；
  - 80/80 条跨 Gap 存活长连接 ID 保持稳定；
  - 真实物理流量为 153KB，Naive “First-Seen” 算法产生 212MB 虚假流量爆炸（虚增 1450 倍）；
  - 确立了 Gap 期间流量归档为区间增量 `attributionInterval: [lastObserved, firstObserved]`。

### 11.2 同进程 WebSocket 断线重连故障注入实测 `[Observed]`
- **测试工具**: `tools/discovery/scenarios/run-ws-reconnect-experiment.mjs`
- **测试报告**: `ws-reconnect-report.json`
- **实测事实**:
  - 同一 Collector 客户端断线 3.02s 后重新建立 WS，42/42 条活跃连接 ID 保持连续稳定；
  - 确立了自动重连后的首帧 Baseline 状态机处理。

### 11.3 基础配置运行时更新 (Runtime Config PATCH) 实测 `[Observed]`
- **测试工具**: `tools/discovery/scenarios/run-config-patch-experiment.mjs`
- **实测事实**:
  - `PATCH /configs` 下全局计数器单调连续（0 reset），连接保持存活。

### 11.4 官方 API 语义说明 `[Documented]`
根据 Metacubex 官方 API 文档：
- `PATCH /configs`: Update basic configuration
- `PUT /configs?force=true`: Reload basic configuration
- `POST /restart`: Restart the kernel

### 11.5 内核冷重启与 Counter Reset 检测信号 `[Inferred / Documented]`
- 计数器回退（$\text{current} < \text{previous}$）规范为 `counter_epoch_break` 信号，指示可能发生了内核重启或数据源重置，状态机应触发 Re-bootstrap 并记录生命周期事件，禁止单凭此等式强行假定唯一根因。

---

## 12. Phase 0 核心数据源验证总结 (Phase 0 Completion Matrix)

| 核心课题 | 验证状态 | 证据类型 | 阻塞 Phase 1? | 结论 |
| :--- | :---: | :---: | :---: | :--- |
| **Live Controller Correlation** | **PASS** | `[Observed]` | **YES** | 100% 确认接入承载 TUN 的 live Mihomo |
| **Core Fields Coverage** | **PASS** | `[Observed]` | **YES** | 字段语义与缺失分类完全清晰 |
| **Connection Counter Monotonicity** | **PASS** | `[Observed]` | **YES** | 稳态单调非递减确立 |
| **Bootstrap Baseline** | **PASS** | `[Observed]` | **YES** | 区分冷启动与稳态增量 |
| **Snapshot Blind Spot** | **PASS** | `[Observed]` | **YES** | N=600 矩阵量化短连接物理盲区 |
| **Residual Accounting** | **PASS** | `[Observed]` | **YES** | 分层归因与残差模型确立 |
| **Relay Candidate Dedup** | **PASS** | `[Observed/Provisional]` | **YES** | 配对去重与未配对保留 |
| **Dynamic Routing Hop Order** | **PASS** | `[Observed scoped]` | **YES** | `chains[0]` 出口节点，逆向因果流 |
| **Collector Offline Gap** | **PASS** | `[Observed]` | **YES** | Coverage Gap 与跨 Gap 存活连接归档 |
| **Same-Process WS Reconnect** | **PASS** | `[Observed]` | **YES** | 自动重连首帧 Baseline 状态机 |
| **Config Update (PATCH)** | **PASS** | `[Observed]` | **NO** | 计数器连续，连接保持 |
| **True Config Reload (PUT)** | **DOCUMENTED** | `[Documented]` | **NO** | 保守处理：检测 epoch break |
| **Kernel Cold Restart** | **DOCUMENTED** | `[Documented/Inferred]` | **NO** | `counter_epoch_break` 信号模型 |
| **QUIC / HTTP3** | **NOT OBSERVED** | `[Not Observed]` | **NO** | 非阻塞项，按 UDP 通用建模 |
| **REJECT Rule** | **NOT OBSERVED** | `[Not Observed]` | **NO** | 非阻塞项，独立分类桶 |

**Phase 0 核心阻塞证据全部达成闭环。Live FLClash restart 行为作为 scoped 非阻塞验证项。系统正式具备进入 Phase 1 的充分实测基石。**




