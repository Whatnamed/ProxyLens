# ProxyLens 系统架构与技术认知

> 本文记录当前已经确认的系统边界、首选技术方向和仍需通过 Phase 0 / Phase 1 实测验证的问题。未经验证的实现细节不得写成既定事实。

---

## 1. 系统边界

ProxyLens 是一个**旁路只读观察系统（Bypass Observer）**。

它不位于实际网络数据路径中，不代理流量、不接管网卡、不修改路由，也不替代 Mihomo / FLClash。

```text
用户进程
   │
   │ TUN / 系统代理 / 其他 Mihomo 入站
   ▼
Mihomo ─────────────────────────→ 远程目标
   │
   │ External Controller（只读观察）
   ▼
ProxyLens Collector
   │
   ▼
本地持久化
   │
   ▼
ProxyLens UI
```

核心隔离原则：

```text
ProxyLens 故障 ≠ Mihomo 故障 ≠ 系统断网
```

Collector 崩溃最多造成审计数据缺口，不得影响用户的实际网络连接。

---

## 2. 初步组件划分

当前确定 Collector、Storage、UI 三类职责需要解耦，但具体语言、框架和通信方式尚未最终确定。

### Collector

职责：

- 连接 Mihomo External Controller；
- 读取实时连接和必要的全局状态；
- 维护连接生命周期状态；
- 计算可靠的流量增量；
- 记录 Controller / Collector 监控缺口；
- 将历史数据写入本地持久化层。

要求：

- 可轻量后台运行；
- UI 关闭不影响采集；
- Mihomo 不可用时自动等待并尝试恢复；
- 不参与实际代理和分流；
- 资源占用应显著低于主代理客户端，但具体 CPU / RAM 预算必须由原型实测后确定。

### Storage

需要保存：

- 历史连接及其发生当时的元数据；
- 连接流量；
- 监控缺口；
- 后续必要的聚合数据或索引。

当前**首选候选**是本地 SQLite，并优先评估 WAL 模式是否适合 Collector 单写、UI 并发读取的模式。

SQLite + WAL 目前不是不可修改的最终决定。Phase 1 / Phase 2 需要用真实负载验证其写入、查询和恢复行为后再正式确认。

### UI

职责：

- 历史连接搜索与过滤；
- 多维聚合统计；
- 单条连接审计链展示；
- 待检查流量；
- 监控覆盖率和缺口展示。

UI 随用随开，关闭时不得停止 Collector。

---

## 3. Mihomo External Controller：首要数据源

第一阶段不做 WFP、WinDivert、pcap、ETW 或 TLS 解密，而是先验证 Mihomo 自己已经掌握的数据是否足够支撑 ProxyLens。

### 3.1 需要验证的接口

Phase 0 优先检查：

- `/connections`：重点验证 WebSocket 实时连接快照、连接元数据、累计流量、Rule 和 Chains 等字段；
- `/traffic`：验证其 GET / WebSocket 实时流量语义，作为全局流量观察和一致性辅助数据源；
- `/version`：记录实际 Mihomo 版本；
- `/configs`：在确有必要时读取与当前运行模式有关的只读状态。

不能只依据第三方 Dashboard 的类型定义推断 Mihomo 行为，必须保存真实运行样本验证。

### 3.2 重点字段

对每条连接重点验证以下字段是否存在、什么时候为空、语义是否稳定：

```text
id
start
metadata.network
metadata.type
metadata.sourceIP
metadata.sourcePort
metadata.destinationIP
metadata.destinationPort
metadata.host
metadata.sniffHost
metadata.dnsMode
metadata.process
metadata.processPath
upload
download
chains
rule
rulePayload
```

一个**预期形态**可能类似：

```json
{
  "id": "...",
  "metadata": {
    "network": "udp",
    "destinationIP": "203.0.113.10",
    "destinationPort": "123",
    "host": "us.pool.ntp.org",
    "process": "HipsDaemon.exe",
    "processPath": "C:\\Program Files\\...",
    "sniffHost": ""
  },
  "upload": 120,
  "download": 120,
  "chains": ["某实际节点", "入口选择"],
  "rule": "Network",
  "rulePayload": "udp"
}
```

这只是字段示例，不代表当前版本已经验证了所有字段、命名和 Chains 顺序。

---

## 4. 核心正确性问题

ProxyLens 最难的部分不是 UI，而是把持续变化的连接快照转换成不重复、不漏记、可解释的历史。

### 4.1 Connection Diff 与状态机规约 (State Machine Specification)

基于 Phase 0C-3 实测，确立了连接状态机的核心基线规则：

```text
增量计算 = 当前累计值 - 上一次累计值
```

实现必须严格区分以下生命周期阶段：

1. **会话冷启动 (Session Bootstrap)**：
   - 采集器启动首帧收到的全部已有连接，记录为 `preexisting_at_session_start`，其 `upload`/`download` 作为 baseline，**不得作为当期增量流量计入**（防止 MetaCubeXD 式的冷启动历史流量虚假当期爆发）；
2. **稳态新连接 (Steady-State New Connection)**：
   - 在连续监控中新出现的 ID，以 `0 B` 为基线，首次观测到的计数器直接计入当期增量（支持单帧瞬态短连接的流量计入）；
3. **连接移出快照 (Disappeared from Snapshot)**：
   - 当连接从活跃列表消失时标记为 `disappeared_from_snapshot`，并记录 `last_observed` 计数，显式标记 `possible_unobserved_tail`。

### 4.2 快照轮询盲区与残差模型 (Snapshot Blind Spot & Residual Accounting)

Phase 0C-3C 与 0C-6 实测确立了快照轮询机制的物理边界：

1. **短连接快照盲区**：
   - 默认 1000ms 采样下，存活短于 500ms 的短连接有 **84%~91%** 无法被快照捕获；
   - 提升采样率至 250ms 可将捕获率提升 3.5 倍至 56%，但无法完全消除物理盲区；
2. **残差模型 (Residual Model)**：
   - 内核全局计数器 $\Delta(\text{uploadTotal})$ 记录了包含盲区短连接在内的全量物理流量；
   - 连接层归因流量之和 $\sum \Delta(\text{AppConn})$ 记录了所有被快照捕获的应用连接流量；
   - 系统必须显式计算并持久化残差：$$\text{Residual} = \Delta(\text{uploadTotal}) - \sum \Delta(\text{AppConn})$$
   - 在 250ms 快照下，上传残差收敛至 **<1%**（稳态长连接残差 < 0.1%）。

### 4.3 链式代理/多跳底层连接去重规约 (Multi-hop Relay Deduplication)

Phase 0C-6 实测发现：
- 在配置链式/中继代理时，`/connections` 会同时列出应用层逻辑连接（`process: agy.exe, host: play.googleapis.com`）与 Mihomo 发往第一跳中继节点的底层连接（`process: "", host: 168.158.196.123`），两者流量完全相同；
- **规约**：底层中继连接必须被显式识别（`rule === "" && process === ""`），并在应用层流量汇总时进行**去重过滤**，防止代理流量产生 **200% 重复计算（Double Counting）**。

### 4.4 代理链拓扑顺序规约 (Hop Order Semantics)

Phase 0C-4 实测证明：
- `chains[0]` 为**最终物理出口节点 (Physical Egress Node)**；
- `chains[chains.length - 1]` 为**顶层分流规则匹配策略组 (Top-Level Rule Group)**；
- UI 呈现统一采用正向因果顺序渲染：`chains.slice().reverse()`。

---

---

## 5. 监控缺口模型

ProxyLens 明确区分两类问题：

### 信息不完整

连接被采到了，但某些字段为空，例如：

- MissingProcess
- MissingProcessPath
- MissingHost
- IPOnly
- MissingRule
- MissingChain

### 未被监控

某个时间段 Collector 无法观察 Mihomo，例如：

- Collector 未运行；
- Controller 无法连接；
- Controller WebSocket 中断；
- Mihomo 未运行或正在重启。

这类情况必须记录为 **Monitoring Gap**，而不是“Unknown Traffic”。

概念上至少需要：

```text
gap_start
gap_end
reason
optional_detail
```

是否还能进一步区分“系统睡眠”和“Mihomo 未运行”等原因，需要根据 Collector 实际可观察的信息验证，不能提前假定都能准确诊断。

### 监控覆盖率

第一版可以先定义**时间覆盖率**：

```text
已监控时长 / 查询区间总时长
```

它表示“这一段时间 Collector 是否在线观察”，不等价于“流量字节覆盖率达到同样百分比”。如果以后能够建立可靠的流量对账，再单独引入字节级 coverage 指标。

---

## 6. DIRECT / PROXY 分类

产品默认关注真正走代理的流量，因此必须可靠区分：

- DIRECT
- PROXY
- REJECT / DROP
- 其他特殊策略结果

不能简单地用“是否存在 chains”或“最终名字不是 DIRECT”拍脑袋判断。

Phase 0 需要针对以下场景记录真实数据：

- 明确 DIRECT 规则；
- 明确代理规则；
- MATCH 兜底代理；
- UDP 宽泛规则；
- REJECT；
- 多层代理链（例如入口 → 第二跳出口）。

确认 Mihomo 的真实字段表现后，再定义正式分类算法。

---

## 7. 隐私与安全边界

ProxyLens 只保存审计所需的连接元数据，不保存内容载荷。

允许保存：

- 进程信息；
- 域名 / IP / 端口；
- 协议；
- Rule / Rule Payload；
- Proxy Chain / Final Proxy；
- 上传 / 下载字节；
- 时间信息。

禁止保存：

- HTTP 请求 / 响应正文；
- Cookie；
- Token；
- 密码；
- TLS 解密内容；
- 代理订阅凭据；
- Controller Secret 明文进入 Git。

原始调研样本可能包含敏感网络历史，默认存放于 Git 忽略目录（例如 `tmp/`），只有脱敏后的最小样本才允许提交。

---

## 8. 性能原则

当前只确定以下方向，不提前设未经验证的 KPI：

- Collector 应长期稳定、低 CPU、低内存；
- UI 不运行时不应加载完整前端运行时；
- 写盘应避免“每个 WebSocket 帧同步写一次”的高频模式；
- 优先评估内存缓冲 + 批量提交；
- 数据库设计必须支持长期历史查询，而不会因为大量短连接迅速退化。

Phase 1 / Phase 2 应建立真实 benchmark，再根据实测结果确定：

- 常驻 RAM 目标；
- 日常 / 高并发 CPU 预算；
- 批量提交周期；
- 单批条数；
- 数据保留与聚合策略；
- 大数据量查询目标。

---

## 9. 已确认技术决策

当前真正确定的只有：

1. **旁路只读**：ProxyLens 不进入网络主路径。
2. **Collector / UI 解耦**：为了历史连续性，接受一个轻量 Collector 后台运行；UI 随用随开。
3. **Mihomo First**：第一阶段只使用 Mihomo External Controller，除非实测证明不足，否则不引入第二套底层网络观测机制。
4. **本地持久化**：历史必须保存在本机；具体存储实现仍需验证。
5. **显式 Monitoring Gap**：采集中断必须独立表达，不能混入 Unknown。
6. **正确性优先**：先解决归因、double counting、重启与缺口，再做完整 UI。

---

## 10. 当前首选候选与开放问题

### Storage

首选候选：SQLite + WAL。

需要验证：

- Collector 单写 + UI 并发读取；
- 大量短连接；
- 长连接阶段性更新；
- 崩溃恢复；
- 索引后的历史查询性能。

### Collector 语言

候选：Go / Rust。

Phase 0 不做最终选择，先确定真实 API 交互模式和数据模型。

### UI

候选：Tauri 或本地 Web UI 等轻量方案。

在 Phase 2 正确性稳定前，不因为 UI 偏好反向约束 Collector。

### Collector 与 UI 通信

待比较：

- UI 只读访问本地数据库；
- Collector 暴露本地 IPC / HTTP 查询接口。

选择依据应包括并发安全、部署复杂度、查询能力和长期维护成本。

---

## 11. Phase 0 必须回答的问题

进入正式 Collector 开发前，至少应得到这些结论：

1. `/connections` 的推送频率和快照语义是什么？
2. `upload` / `download` 是否稳定表现为连接级累计值？
3. 连接 ID 在生命周期内是否稳定？
4. 连接消失在不同场景下代表什么？
5. Windows TUN 下 `process` / `processPath` 的实际覆盖率如何？
6. TCP、UDP、QUIC 的字段差异是什么？
7. `host` / `sniffHost` / `destinationIP` 的可用性如何？
8. `rule` / `rulePayload` 在各种规则类型下如何表现？
9. `chains` 的顺序、DIRECT 表达、多层代理表达分别是什么？
10. `/traffic` 能否用于可靠的整体一致性辅助校验，以及它在 Mihomo 重启时如何变化？
11. MetaCubeXD 等参考实现采用的 diff 方式，与真实 Mihomo 行为是否一致？

这些结论应以“官方文档 / 真实观察 / 推断”三种证据等级分别记录，并最终沉淀到 Phase 0 调研报告中。
