# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 2 — Persistence Foundation Complete (Package D Complete)`
- **代码状态**：Phase 2A 存储底座（SQLite + WAL）已完全落地实现。引入纯 Go 驱动 `modernc.org/sqlite`（Zero CGO），构建了不可变的权威事件日志 `event_journal`、单调游标校验与幂等插入机制；实现了 `connections`、`connection_traffic`、`monitoring_gaps`（支持流中断与进程级离线 Gap）、`residual_intervals` 与 `collector_health` 的实时投影；实现了 `RebuildProjections` 支持从 Journal 100% 完整重建派生视图；`collector run` 正式支持 `--db <path>` 持久化与 `collector storage inspect / gaps / rebuild` 管理命令；端到端实测验证通过（NTP、短请求、持续下载持久化与离线 Gap 推导 100% PASS）。
- **环境资产清单 (Environment Inventory)**：
  - OS: Windows 11 (AMD64) / 12th Gen Intel Core i5-12400 (12 cores)
  - 客户端: FLClash (PID 13436) + FlClashCore (PID 20320) 运行中
  - TUN 状态: 启用 (`device: FlClash`, `find-process-mode: always`, `enhanced-mode: fake-ip`, `mode: rule`)
  - Live 运行时内核: `Mihomo Meta v1.10.0` (GET `/version` 返回)
- **存储引擎实测指标 (SQLite WAL Engine Evidence)**：
  - 驱动: `modernc.org/sqlite` (Pure Go, Windows AMD64)
  - 模式: `PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA foreign_keys=ON;`
  - 事务边界: 单事件原子短事务（Journal + Projections + Cursor + Session Progress），强保证 Fail-Stop 与崩溃一致性；
  - 离线 Gap 推导: 进程非正常终止识别为 `interrupted`，启动时自动生成 `collector_session_boundary` 缺口，杜绝将离线期间流量误判为 Unknown。

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
10. **Collector 语言选型 (ADR 0001)**：选定 **Go (v1.24+)** 作为生产 Collector 开发语言。
11. **存储引擎与持久化选型 (ADR 0002)**：选定 **SQLite + WAL**（纯 Go `modernc.org/sqlite` 驱动，`synchronous=NORMAL`）；`event_journal` 作为不可变权威层，所有投影视图可随时 100% 完整重建。
12. **Relay 结构配对去重规约 (Relay Pairing Model)** `[Scoped Observed / Provisional]`：仅在存在确凿 1-to-1 配对应用连接（时间重叠、流量高度吻合、链路结构包含关系）时才判定为底层中继去重；若出现 1-to-N 或 N-to-1 歧义则保守保留在 candidate，不执行去重扣减；Collector 实时输出初阶诊断视图，权威 adjusted accounting 保留由 Phase 2 Storage 结合全局时序重算；$\text{Residual} = \Delta(\text{uploadTotal}) - \text{UniqueObserved}$。
13. **代理链拓扑因果顺序规约 (Hop Order Semantics)** `[Scoped Observed / Provisional: 当前测试的策略选择组与出站拓扑]`：`chains[0]` 为最终物理出站节点，`chains[last]` 为顶层规则分流策略组，UI 渲染按 `chains.slice().reverse()` 呈现。
14. **连接历史不可变性 (Routing Immutability)** `[Scoped Observed / Provisional: 当前受控长连接与测试拓扑]`：存活连接绑定创建时出站路径，受控实测显示节点切换不篡改已有存活连接的历史节点路径；若发生突变，发出健康告警并安全更新。
15. **Gap 恢复与 Bootstrap 规约**：冷启动/重连首帧已有连接记录为 baseline（delta=0），跨 Gap 存活连接增量归属为 Gap 期间累积流量；Gap 期间若发生 Epoch Break 则废弃跨 Gap 增量并新建 Epoch。

---

## Open Questions

### 实现选型 (Phase 2B & Phase 3 决策项)

- Storage：多维分时 Aggregates 聚合表结构、权威 Relay 重算逻辑与阶段性 Checkpoint/Retention 策略；
- UI：Tauri vs 本地 Web UI；
- UI 与 Collector：共享 SQLite 读取 vs 本地轻量 IPC / HTTP 查询端点。

---

## Next Step

进入 **Phase 2B — 聚合引擎、权威核算与全栈性能基准**：
1. 实现多维分时 Aggregation（按 Process、Host、Outbound Node 聚合分钟/小时/天流量）；
2. 实现全局时序下的权威 Relay 重新对账与 Residual 校验；
3. 执行端到端 SQLite 批量写入性能基准与长效 Soak 稳定性实测。
