# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 0 — Documentation & Discovery (Phase 0 Core Evidence Closure Complete, Ready for Phase 1)`
- **代码状态**：Phase 0 核心阻塞性实测已全部闭环（Stage R0~R8 完成），进入 Phase 1 准备就绪。
- **环境资产清单 (Environment Inventory)**：
  - OS: Windows 11 (AMD64)
  - 客户端: FLClash (PID 13436) + FlClashCore (PID 20320) 运行中
  - TUN 状态: 启用 (`device: FlClash`, `find-process-mode: always`, `enhanced-mode: fake-ip`, `mode: rule`)
  - Live 运行时内核: `Mihomo Meta v1.10.0` (GET `/version` 返回)
- **Controller 验证状态 (Controller Validation Status)**：
  - `Phase 0C-0 Gate: PASS` `[Observed]`：通过受控网络请求与进程相关性比对（curl.exe / 日常连接），100% 证明 Probe 成功接入正在承载 TUN 日常流量的 live FlClashCore (127.0.0.1:9090)。
  - `Phase 0C-1 DIRECT / PROXY Baseline: COMPLETED` `[Observed]`：成功捕获典型 DIRECT 样本 (`chains: ["DIRECT"]`) 与 PROXY 样本 (`chains: [出站节点, 策略组...]`)。
  - `Phase 0C-2 UDP / NTP / QUIC: COMPLETED` `[Observed]`：验证了 Windows TUN 下 UDP 进程归因完整性、受控 NTP 48B/48B 流量精确性与 UDP Pseudo-connection 留存现象（6秒以上）。
  - `Phase 0C-3A & 3B Connection Counter Semantics: COMPLETED` `[Observed]`：实测证明稳态长连接单调非递减计数与冷启动基线（Bootstrap vs Steady-State）。
  - `Phase 0C-3C Snapshot Cadence & Capture-Rate Matrix: COMPLETED` `[Observed]`：
    - 实测证明 Mihomo 原生支持 `?interval=<ms>`（250ms / 500ms / 1000ms），吞吐量在 236 连接下分别为 577 KB/s / 277 KB/s / 132 KB/s（确立为 Phase 1 基准测试候选间隔）；
    - 执行 12 轮独立试验矩阵（12 trials, N=600），量化短连接捕获盲区（1000ms 下短连接捕获率 DIRECT 14.0% / PROXY 28.0%；250ms 下 DIRECT 提升至 55.0%，PROXY 提升至 86.0%），捕获连接单帧占比分布在 84.9%~100.0%。
  - `Phase 0C-6 Global Accounting & Reconciliation: COMPLETED` `[Observed]`：
    - 建立分层流量归因模型（`knownApplication`, `unpairedMissingAttribution`, `confirmedRelayDuplicate`, `uniqueObserved`）；
    - 实测证明近似时间积分下 `/traffic` 速率与全局计数器增量高度吻合（偏差在 -1.7% ~ +1.7% 之间）；
    - 建立基于时间重叠、流量吻合与链路包含的 Relay Candidate 配对去重规约（杜绝未知流量掩盖）。
  - `Phase 0C-4 Dynamic Routing & Node Switching: COMPLETED` `[Observed / Scoped]`：
    - 实测证明连接建立后 `chains` 具有存活期历史不可变性（0 突变），新连接即时迁移至新物理节点；正式升格 `chains` 动态拓扑因果顺序规约（`chains[0]` 为最终物理出站节点）。
  - `Phase 0C-5 Lifecycle, Config Update & Controller Gap: COMPLETED` `[Observed / Documented]`：
    - 实测 4.46s Monitoring Gap 与 3.02s 同进程受控 WebSocket 重连（验证了重连首帧基线与 ID 连续性语义），跨 Gap 存活长连接 ID 保持稳定；量化了 Naive 算法在重连时产生的 1450 倍虚假流量爆炸，确立了 Gap 恢复规约；实测验证配置更新（PATCH）下全局计数器单调连续；规范内核冷重启为 `counter_epoch_break` 信号。
- **当前项目级 Skill**：`.agents/skills/mihomo-data-source-validation/SKILL.md`。

---

## Confirmed Decisions

1. **产品定位**：ProxyLens 是代理流量审计与分流优化辅助工具，核心是解释“谁、去哪、为什么、走哪里、多少”，不是单纯总流量计费器。
2. **审计优先**：默认关注真实代理流量，同时正确区分 DIRECT、PROXY、REJECT 等结果。
3. **Unknown 必须可解释**：缺进程、缺域名、仅 IP、缺规则、缺代理链等必须分别表达。
4. **Monitoring Gap 独立建模**：Collector / Controller 中断不能伪装成 Unknown Traffic。
5. **Collector 与 UI 解耦**：允许轻量 Collector 后台运行；UI 随用随开。
6. **旁路只读**：ProxyLens 不修改 Mihomo 配置、规则、节点、TUN、系统代理或路由，不阻断或限速。
7. **本地优先**：不上传网络历史，不保存 HTTP 正文、Cookie、Token、密码、TLS 明文或其他 Payload。
8. **Mihomo First**：第一阶段先使用 Mihomo External Controller；只有实测证明存在不可接受盲区时，才评估第二观测数据源。
9. **正确性优先于 UI**：先解决字段语义、connection diff、double counting、重启与缺口，再推进完整 UI。
10. **不提前锁死技术栈**：Go / Rust、SQLite、Tauri / Web UI 等均需要由前序验证推动决定。
11. **Relay 配对去重规约 (Relay Candidate & Pairing Model)** `[Scoped Observed / Provisional]`：仅在存在确凿配对应用连接（时间重叠、流量高度吻合、链路结构包含关系）时才判定为底层中继去重，严禁简单按“缺进程+缺规则”过滤，杜绝掩盖未归因流量。
12. **快照轮询盲区与分层残差模型**：承认轮询架构下的短连接物理盲区，通过 $\text{Residual} = \Delta(\text{uploadTotal}) - \text{UniqueObserved}$（其中 $\text{UniqueObserved} = \text{KnownApp} + \text{UnpairedMissingAttr} + \text{OtherUnique}$）显式维护全局残差。
13. **代理链拓扑因果顺序规约 (Hop Order Semantics)** `[Scoped Observed / Provisional: 当前测试的策略选择组与出站拓扑]`：`chains[0]` 为最终物理出站节点，`chains[last]` 为顶层规则分流策略组，UI 渲染按 `chains.slice().reverse()` 呈现。
14. **连接历史不可变性 (Routing Immutability)** `[Scoped Observed / Provisional: 当前受控长连接与测试拓扑]`：存活连接绑定创建时出站路径，受控实测显示节点切换不篡改已有存活连接的历史节点路径。
15. **Gap 恢复与 Bootstrap 规约**：冷启动/重连首帧已有连接记录为 baseline（delta=0），跨 Gap 存活连接增量归属为 Gap 期间累积流量。

---

## Open Questions

### 实现选型 (Phase 1 评估决策项)

- Collector：Go vs Rust（结合 250ms/500ms 快照下的 CPU、RAM、GC 表现与 SQLite 写入性能）；
- Storage：SQLite + WAL（长连接阶段性 checkpointing 与崩溃恢复）；
- UI：Tauri vs 本地 Web UI 等；
- UI 与 Collector：共享数据库读取 vs 本地 IPC / HTTP API。

---

## Current Risks

1. **短连接采样盲区**：实测证明轮询机制下短连接存在物理盲区，必须在架构上支持 Residual 残差表达。
2. **多跳代理 Double Counting**：多层代理会产生底层连接，已建立 Relay Candidate 配对去重规约。
3. **连接生命周期语义**：必须使用 `disappeared_from_snapshot` 与 `possible_unobserved_tail` 正确建模。
4. **敏感样本**：原始网络历史必须默认留在 `tmp/` 等 Git 忽略目录并在提交前脱敏。

---

## Next Step

进入 **Phase 1 — Collector 架构、数据模型与技术选型**：
1. 设计本地 SQLite + WAL 数据模型（支持 Connection Details、Aggregates、Monitoring Gaps 与 Residuals）；
2. 建立 Go vs Rust 原型性能对比（验证 250ms 快照下的 CPU、RAM、JSON 解析与批处理写盘开销）；
3. 实现符合 RFC 规约的生产级 Collector 状态机。

---

## Recent Changes

### 2026-08-20 / 2026-08-21 (Big Work Package A & B & B.1.1)

- **Stage B0 / B.1.1 (Evidence Repair & Validation Gate)**：
  - 修复实验窗口有效性 Gate、增强 Matcher 1-to-1 映射与路由校验；
  - 重构 Relay Candidate 配对去重引擎为 pair-specific structural check，严禁静默过滤；
  - 严谨对齐 `/traffic` 近似积分时间窗与 `COUNTER_RESET / epoch break` 检测；
  - 增强 Live mutation safety perimeter（dry-run 保护与回滚校验）。
- **Stage B1 (Phase 0C-4 Dynamic Routing & Node Switching)**：
  - 实现受控节点切换工具与安全回滚保护；
  - 实测证明已有连接链路历史不可变性（0 突变），新连接即时迁移；
  - 正式升格 `chains` 动态拓扑因果顺序规约（`chains[0]` 为最终物理出站节点）。
- **Stage B2 (Phase 0C-5 Lifecycle, Config Update & Controller Gaps)**：
  - 实测 4.46s Monitoring Gap，证明存活连接稳定性与 Naive 算法 1450 倍虚假爆炸缺陷；
  - 实测运行时配置更新（PATCH）下计数器连续性与连接保持；
  - 确立生命周期恢复与内核冷重启检测状态机。
  - 确立生命周期恢复与内核冷重启检测状态机。
- **Stage B3 (Phase 0 Synthesis & Collector RFC)**：
  - 汇总量化指标矩阵，输出完整的 Collector 状态机转移图与数学模型。
- **Stage B4 (Final Delivery)**：
  - 全量同步更新 `docs/research/mihomo-data-source.md`、`docs/STATUS.md`、`docs/ARCHITECTURE.md` 与 `docs/ROADMAP.md`。





