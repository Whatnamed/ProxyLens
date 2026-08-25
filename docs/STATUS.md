# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 2B1 — Accounting & Aggregation Foundation Complete (Phase 2B still IN PROGRESS)`
- **代码状态**：Phase 2B1 版本化核算、保守中继对账与分时聚合层已完全落地实现：
  1. **连接观察生命周期语义 (E1)**：在 `connections` 引入 `observation_ended_at`、`observation_end_reason` 与 `observation_end_event_id`，精确区分 `disappeared_from_snapshot`、`epoch_boundary`、`collector_session_closed` 与 `collector_session_interrupted`，绝不把采集器启停伪装成网络连接关闭；
  2. **版本化核算层 (E2 / E3, ADR 0003)**：构建 `accounting_runs`、`relay_relations` 与 `accounted_traffic`。保持原始事实（`event_journal` / `connection_traffic`）绝对不可变，核算视图作为派生层 100% 确定性全量可重建；
  3. **保守中继对账 (Conservative Relay Reconciliation v1)**：仅在同 session + epoch 内执行结构、时间与流量三元匹配，遇到 1-to-N / N-to-1 歧义一律不扣流量（`accounted = raw`），杜绝误扣；
  4. **单层物化分时聚合 (E5)**：构建 `usage_hourly_dimensions` 覆盖 9 大核心维度；时间分配采用向下取整 + 确定性余数补偿，整数字节绝对守恒；
  5. **监控覆盖率模型 (E6)**：从首个会话定义 `known_scope_start`，区间早于此识别为 `outside_known_scope`；区间内重叠缺口通过区间求并集（Interval Union）精准去重；
  6. **面向 UI 的稳定 Analytics API 与 Minimal CLI (E7)**：提供 `AnalyticsService` 与 `collector accounting rebuild`、`collector analytics summary / top-processes / top-hosts / top-proxies / coverage` 命令；
  7. **自动化测试与端到端实测 (E8)**：全套 Migration（v1->v5）、Lifecycle、Relay、Byte Conservation、Coverage Union 与 10k 性能 Sanity 测试 100% PASS。
- **环境资产清单 (Environment Inventory)**：
  - OS: Windows 11 (AMD64) / 12th Gen Intel Core i5-12400 (12 cores)
  - 客户端: FLClash (PID 13436) + FlClashCore (PID 20320) 运行中
  - TUN 状态: 启用 (`device: FlClash`, `find-process-mode: always`, `enhanced-mode: fake-ip`, `mode: rule`)
  - Live 运行时内核: `Mihomo Meta v1.10.0` (GET `/version` 返回)
- **双事实权威源与三层存储模型 (Dual Authority & Layered Storage)**：
  - 网络观测权威: `event_journal`
  - 采集生命周期权威: `collector_sessions`
  - 原始事实层: `event_journal`, `connection_traffic`
  - 版本化核算层: `accounting_runs`, `relay_relations`, `accounted_traffic`
  - 分时聚合层: `usage_hourly_dimensions`

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
11. **存储引擎与持久化选型 (ADR 0002)**：选定 **SQLite + WAL**（纯 Go `modernc.org/sqlite` 驱动，`synchronous=NORMAL`）；`event_journal` 与 `collector_sessions` 构成双事实权威层。
12. **版本化核算与保守中继对账 (ADR 0003)**：原始事实永久不可变；核算与物化分时聚合带版本且支持确定性全量重算；歧义中继连接不扣减；分时聚合严格保证整数字节守恒。
13. **代理链拓扑因果顺序规约 (Hop Order Semantics)**：`chains[0]` 为最终物理出站节点，`chains[last]` 为顶层规则分流策略组，出站节点历史只从发生时的 chains 派生，绝不读取当前活动选择组状态篡改历史。
14. **Gap 恢复与 Bootstrap 规约**：冷启动/重连首帧已有连接记录为 baseline（delta=0），跨 Gap 存活连接增量归属为 Gap 期间累积流量；Gap 期间若发生 Epoch Break 则废弃跨 Gap 增量并新建 Epoch。

---

## Open Questions

### 实现选型 (Phase 2B2 & Phase 3 决策项)

- Storage 运维：长期 Retention 自动清理策略与 WAL checkpoint 调度器调优；
- UI：Tauri vs 本地 Web UI；
- UI 与 Collector 通信：共享 SQLite 只读连接 vs 本地轻量 IPC / HTTP 查询端点。

---

## Next Step

进入 **Phase 2B2 — 存储生命周期运维、全栈基准与审计核算最终验收**：
1. 实现数据生命周期 Retention 自动清理机制；
2. 优化 SQLite WAL checkpoint 调度；
3. 执行端到端全栈性能基准测量与长时间稳定性 Soak；
4. 运行 PRODUCT A-E 全场景持久化与核算最终验收。
