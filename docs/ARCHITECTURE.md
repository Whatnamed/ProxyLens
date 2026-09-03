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
proxylens-runtime
   ├─ Collector
   └─ Scheduled Accounting
   │
   ▼
SQLite + WAL（Authority DB）
   │
   │ read-only
   ▼
proxylens-query-api → ProxyLens UI
```

核心隔离原则：

```text
ProxyLens 故障 ≠ Mihomo 故障 ≠ 系统断网
```

Collector 崩溃最多造成审计数据缺口，不得影响用户的实际网络连接。

---

## 2. 系统组件划分

当前确定 Runtime、Collector、Storage、UI 四类职责解耦运行：

### Desktop Runtime Core (Phase 3E-1)

- `proxylens-runtime` 是新的 Go 前台 Runtime Core executable，组合可复用的 `CollectorRunner` 与周期性 `AccountingScheduler`；
- Runtime 负责解析 DB path、初始化 writer DB、启动 Collector 与自动核算，并在 cancellation 时按 scheduler-first 顺序 graceful shutdown；
- Accounting 默认每 30s 检查 Freshness，fresh 或无事件时 skip，落后时复用 `storage.RebuildAccounting`；单次核算失败不终止 Collector；
- `collector run` 保留为薄 CLI wrapper，继续提供原有 flags、signal/stdin STOP、validation sink 与 summary；
- 当前 Runtime 是前台可运行 core，不承担 Windows Service、开机/登录自启、托盘、single-instance、detached ownership 或安装器生命周期。

### Collector (已确立生产原型)

- **实现语言**: **Go (v1.24+)**（详见 `docs/decisions/0001-collector-language.md`）；
- **核心职责**:
  - 只读连接 Mihomo External Controller（`GET /version`, `GET/WS /connections`，`/traffic` 作为可选辅助工具）；
  - 维护连接确定性生命周期状态机（Bootstrap 首帧基线、稳态单调差分、消失连接标记）；
  - 分层流量归因（KnownApp、UnpairedMissingAttr、RelayCandidate、ConfirmedRelayDuplicate）与残差计算（初阶诊断视图，原始事实永久保留由 Phase 2 Storage 重算）；
  - 记录 Controller / Collector 监控缺口（Monitoring Gaps 与 Counter Epoch Breaks）；
  - 通过有界队列（Bounded Queue）背压机制输出确定性事件流（详见 `docs/collector-rfc.md` 与 `docs/phase2-storage-handoff.md`）。
- **运行特征**:
  - 轻量后台常驻（采用有界内存队列与活跃连接表回收机制）；
  - UI 随开随用，关闭 UI 完全不影响后台采集；
  - Controller 不可用时通过指数退避 + Jitter 自动恢复。

### Storage (已确认生产架构)

- **实现引擎**: **SQLite + WAL**（纯 Go 驱动 `modernc.org/sqlite`，`PRAGMA synchronous=NORMAL;`，详见 `docs/decisions/0002-storage-engine.md` 与 `docs/decisions/0003-reconciled-accounting.md`）；
- **双事实权威源 (Dual Authority Model)**:
  - **网络观测事实权威 (Network Observation Authority)**: `event_journal`（包含所有 CollectorEvent 原始 JSON 与 SHA256 签名）；
  - **采集器生命周期权威 (Collector Lifecycle Authority)**: `collector_sessions`（记录启停、状态与崩溃边界）；
  - **派生查询/核算/聚合视图 (Derived Views)**: 由 `event_journal` 与 `collector_sessions` 100% 确定性可重建；
- **三层数据结构**:
  1. **原始事实层 (Immutable Raw Evidence)**: `event_journal`, `connection_traffic`；
  2. **版本化核算层 (Versioned Reconciled Accounting)**: `accounting_runs`, `relay_relations`, `accounted_traffic`；
  3. **分时聚合层 (Materialized Hourly Aggregates)**: `usage_hourly_dimensions`。

### UI Platform & Local Query API (已确认生产架构，ADR 0005)

- **实现技术**: **Tauri v2 + React 19 + TypeScript + Vite**；
- **架构分工与查询边界 (Locked Query Boundary)**:
  ```text
  Mihomo
     ↓
  proxylens-runtime
     ├─ Go Collector ───────────→ SQLite + WAL (Authority DB)
     └─ Scheduled Accounting ───→ derived accounting
                                   ↑
                                   │ read-only (query_only=ON)
                            Go Local Query API (Sidecar)
                                   ↑
                           127.0.0.1 : ephemeral port (HTTP JSON)
                                   ↑ (Authorization: Bearer <token>)
                         React + TypeScript + Vite
                                   ↑
                           Tauri v2 (Desktop Shell)
  ```
- **核心契约**:
  - **Tauri / Rust**: 仅负责桌面原生窗口生命周期与 Go Query API Sidecar 启停，生成单次会话高熵 Bearer Token（>=256-bit），**严禁** 在 Rust 中实现 Analytics SQL、核算或存储业务逻辑；Phase 3E-1 不自动 spawn/stop `proxylens-runtime`；
  - **Go Local Query API (`proxylens-query-api`)**: 以只读模式（`query_only=ON`, `busy_timeout=10000`）打开数据库，严格绑定 `127.0.0.1` 随机端口，校验 Bearer Token 与 CORS，完全复用 `storage.AnalyticsService` 与 `storage.QueryService`；
  - **React / TypeScript**: 纯 Web 前端，通过 TanStack React Query 消费 HTTP JSON API，**严禁** 直接读取 SQLite 数据库；
  - **零耦合生命周期**: UI 随开随用，UI 关闭时仅终止 Query API Sidecar，后台常驻 Collector 保持独立运行，完全不受影响。

### 2.1 Desktop data path contract (Phase 3E-1)

正式 Windows V1 authority DB 默认位于 `%LOCALAPPDATA%\ProxyLens\data\proxylens.db`。路径 precedence 为 `PROXYLENS_DB_PATH` → `PROXYLENS_DATA_DIR\proxylens.db` → 默认路径；Runtime writer 可创建目录/DB，Tauri/Query API 只读 resolver 在 DB 缺失时返回 `DB_NOT_READY`，不创建或猜测数据库。

Runtime、Query API 与 Tauri 仍共享同一 canonical path contract，但职责不同：Runtime 写入，Query API 只读，React 不接触 SQLite。CLI `--db` 可为一次 Runtime invocation 指定直接 explicit path。

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
   - 默认 1000ms 采样下，短连接有 **72%~86%** 无法被快照捕获（捕获率 DIRECT 14.0% / PROXY 28.0%）；
   - 提升采样率至 250ms 可将 DIRECT 捕获率提升至 55.0%，PROXY 捕获率提升至 86.0%，但无法完全消除物理盲区；
2. **分层流量归因与残差模型 (Residual Model)**：
   - 内核全局计数器 $\Delta(\text{uploadTotal})$ 记录了包含盲区短连接在内的全量物理流量；
   - 分层统计连接层归因：$\text{KnownApp}$（已知应用）、$\text{UnpairedMissingAttr}$（未配对缺归因流量）与 $\text{UniqueObserved}$；
   - 系统必须显式计算并持久化残差：$$\text{Residual} = \Delta(\text{uploadTotal}) - \text{UniqueObserved}$$
   - 在稳态长连接下残差收敛至 < 0.1%，在突发短连接下残差随采样周期缩短而显著收敛。

### 4.3 链式代理/多跳底层连接 Relay Candidate 配对规约 (Relay Pairing Model)

Phase 0C-6 实测发现：
- 在配置链式/中继代理时，`/connections` 会同时列出应用层逻辑连接与 Mihomo 发往第一跳中继节点的底层连接，两者流量高度吻合；
- **规约**：严禁简单按“缺进程+缺规则”普遍过滤。必须将其标记为 `relay_candidate`，只有在会话中找到时间窗口重叠、流量高度吻合且存在链路结构关系的配对应用连接时，才被判定为 `CONFIRMED_RELAY_DUPLICATE` 并予以去重；未配对的缺失归因连接必须保留为 `unpaired_missing_attribution`，计入待核查流量。

### 4.4 代理链拓扑因果顺序规约 (Confirmed Hop Order Semantics)

Phase 0C-4 受控实测正式确立 `[Scoped Observed / Provisional]`：
- **`chains[0]`**: **最终物理出站节点 (Physical Egress Node)**（或 `DIRECT`）；
- **`chains[1 .. length - 2]`**: **级联策略选择组 (Intermediate Policy Selectors)**；
- **`chains[length - 1]`**: **分流规则匹配命中的顶层策略组 (Top-Level Rule Target Group)**；
- **连接历史不可变性 (Routing Immutability)**：受控实测显示连接建立后其 `chains` 路径在生命周期内保持稳定（0 突变），外部节点切换不篡改已有存活连接的历史路径；
- **渲染规范**: UI 呈现统一采用正向因果渲染：`chains.slice().reverse()`（即：分流规则命中组 $\rightarrow$ 级联选择组 $\rightarrow$ 物理出站节点）。

### 4.5 长连接阶段性持久化 (Sustained Connection Persistence)

只在连接关闭时保存最终值可能导致：
- UI 长时间看不到正在产生的大流量；
- Collector 异常退出或崩溃时丢失未持久化的中间进度。

因此系统必须支持长连接阶段性增量持久化（Checkpointing）。具体多久提交一次、达到多少增量流量时写入，属于 Phase 1 / Phase 2 待测参数。

### 4.6 连接消失与生命周期恢复状态机 (Lifecycle & Gap Recovery Semantics)

基于 Phase 0C-5 实测与官方 API 文档，确立了四类生命周期恢复规约：
1. **正常关闭**: 从快照移除，记录 `last_observed` 字节与 `possible_unobserved_tail` 标记；
2. **基础配置运行时更新 (Runtime Config PATCH)**: 全局计数器单调连续，绝大多数长连接保持原 ID 存活；
3. **监控中断与重连 (Monitoring Gap & Reconnect)**:
   - 记录 `coverageGap: [lastObserved, firstObserved]` 区间与物理流量 $\Delta(\text{uploadTotal})$；
   - 跨 Gap 存活连接的增量归属为 **Gap 期间累积流量**，杜绝重连当期的虚假流量爆炸；
4. **Counter Reset / Epoch Break 信号检测**:
   - 逐帧扫描检测相邻帧：当检测到 $\text{current}.\text{uploadTotal} < \text{previous}.\text{uploadTotal}$ 时，触发 `counter_epoch_break` 信号，指示可能发生了内核重启或数据源重置，状态机重置并重新执行 Session Bootstrap。

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
