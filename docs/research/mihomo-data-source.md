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
    - `Frame #23 (15:55:48.082Z)`: Up `696 B` (+24), Down `807,291 B` (+0) (关闭握手尾帧)
- **统计与一致性验证** `[Observed]`:
  - **快照出现帧数**: 连续 18 帧（无中途消失重现）；
  - **First Counter**: `467 B / 0 B`；
  - **Last Counter**: `696 B / 807,291 B`；
  - **Observed Delta (Last - First)**: Upload `+229 B`，Download `+807,291 B`；
  - **Sum of Adjacent Deltas**: Upload `+229 B`，Download `+807,291 B`；
  - **算术一致性**: **EXACT MATCH (PASS)**；
  - **计数器递减检测**: **0 起 (严格单调非递减)**。
- **Connection Closure Tail 观察** `[Observed]`:
  - 下载完成后，连接在 Frame #23 发送关闭协商（`696 B`）；
  - 在 Frame #24 及后续帧中，该 ID 直接从活跃连接快照表中消失；
  - **事实证明**: `/connections` 是当前活跃连接的快照，内核在连接结束时直接将其移出列表，**不存在显式的 closed 状态事件帧**。

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
  - 当采集器中途接入或启动时，第一帧中已存在的连接所携带的 `download` 字段**直接包含了采集器启动前的全部历史累计流量**；
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
3. **连接消失 (Disappearance Tail)**:
   - 当上一帧存在的 ID 在当前帧消失时，将其标记为 `closed` 并移入历史归档；
   - 最终结算流量以其最后一次被快照捕获的 `lastUpload / lastDownload` 为准。

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

## 8. 开放问题与后续阶段 (Open Questions & Roadmap)

- [ ] **TODO (Phase 0C-3C)**: 短连接捕获率 (Capture Rate) 与采样间隔影响评估；
- [ ] **TODO (Phase 0C-4)**: 链式代理 (Multi-hop / Relay) 与节点切换时的 `chains` 动态变化；
- [ ] **TODO (Phase 0C-5)**: Mihomo 内核重启与 Controller 断开时的连接 ID 状态机与计数器重置行为；
- [ ] **TODO (Phase 0C-6)**: `/traffic` 实时速率与 `/connections` 连接增量的一致性对账机制。
