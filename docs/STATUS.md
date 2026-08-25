# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 2 Complete — Storage, Accounting & Runtime Validation Finalized (Ready for Phase 3 UI)`
- **代码状态**：Phase 2B2 运行时验证、非阻塞核算边界、心跳存活检测、派生层安全保留策略与全栈性能/验收实测已完全落地并验证通过：
  1. **非阻塞绑定序列核算重建 (F1 / F2, ADR 0004)**：引入全局单调自增 `journal_sequence` 与 `accounting_runs.source_journal_sequence_max`；阶段 A 仅用 <5ms 短事务锁定边界，阶段 B~F 采用分批（1000 行/批）退避重试短事务写入，彻底杜绝 Collector 实时采集被阻塞；
  2. **显式 Freshness / Staleness API (F3)**：实现 `GetAccountingFreshness(ctx)`，精准返回 `LagEvents` 与 `IsFresh` 状态，并在 `GetUsageSummary` 中提供；
  3. **Collector 心跳与运行时存活检测 (F4)**：`SQLiteEventSink` 运行 5s 后台轻量心跳；`GetCoverage` 在会话处于 `running` 但心跳超时时动态派生 `collector_runtime_liveness: collector_heartbeat_stale` 监控缺口；
  4. **安全派生层保留策略 (F5, Safe Derived Retention)**：实现 `PlanDerivedRetention` 与 `ApplyDerivedRetention`，仅清理旧 completed/failed 派生运行，**100% 保证 raw authority 数据（Journal, Sessions, Gaps）永不被删除**；
  5. **WAL 运维与 SQLite 完整性保障 (F6 / F11)**：DSN 统一配置 `synchronous=NORMAL`, `busy_timeout=10000`, 并在 CLI 提供 `collector storage integrity` 执行 `PRAGMA integrity_check` 与 `foreign_key_check`；
  6. **全栈性能基准矩阵 (F7 / F8 / F9)**：
     - 1000ms Steady (100 conns): 10.1s, DB=2960 KB, WAL=0 KB, Coverage=96.9%, Integrity=PASS;
     - 500ms Churn (50 conns): 10.1s, DB=3948 KB, WAL=0 KB, Coverage=97.4%, Integrity=PASS;
     - 250ms Mixed (NTP+Proxy+Direct): 10.1s, DB=3648 KB, WAL=0 KB, Coverage=97.2%, Integrity=PASS;
     - 250ms Relay-Heavy (50 pairs): 10.1s, DB=11308 KB, WAL=0 KB, Coverage=94.9%, Integrity=PASS;
     - 并发 Rebuild: 在持续 250ms 写入期间，Non-blocking Rebuild 耗时 **278 ms**，Freshness 正确识别并追平；
     - 30s 高压 Soak: 连续处理 125 帧，内存平稳，数据库完整性检验为 **HEALTHY**；
  7. **PRODUCT A–E 确定性验收 (F10)**：所有 5 项产品核心场景全部通过确定性单元测试验证；
  8. **测试套件覆盖**: 全部 33 个 Go 测试 + 18 个 Phase 0 回归测试 100% PASS。
- **环境资产清单 (Environment Inventory)**：
  - OS: Windows 11 (AMD64) / 12th Gen Intel Core i5-12400 (12 cores)
  - 客户端: FLClash (PID 13436) + FlClashCore (PID 20320) 运行中
  - TUN 状态: 启用 (`device: FlClash`, `find-process-mode: always`, `enhanced-mode: fake-ip`, `mode: rule`)
  - Live 运行时内核: `Mihomo Meta v1.10.0` (GET `/version` 返回)
- **双事实权威源与三层存储模型 (Dual Authority & Layered Storage)**：
  - 网络观测权威: `event_journal`
  - 采集生命周期权威: `collector_sessions`
  - 原始事实层: `event_journal`, `connection_traffic`, `monitoring_gaps`
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
13. **运行时核算边界、Freshness 与安全保留策略 (ADR 0004)**：全局单调 `journal_sequence` 快照边界；分批短事务写让出写锁；显式 Freshness 表达；心跳存活动态缺口判定；保留策略绝对不可删除 Raw Authority。
14. **代理链拓扑因果顺序规约 (Hop Order Semantics)**：`chains[0]` 为最终物理出站节点，`chains[last]` 为顶层规则分流策略组，出站节点历史只从发生时的 chains 派生，绝不读取当前活动选择组状态篡改历史。
15. **Gap 恢复与 Bootstrap 规约**：冷启动/重连首帧已有连接记录为 baseline（delta=0），跨 Gap 存活连接增量归属为 Gap 期间累积流量；Gap 期间若发生 Epoch Break 则废弃跨 Gap 增量并新建 Epoch。

---

## Open Questions

### 实现选型 (Phase 3 决策项)

- UI 技术选型：Tauri + React/Vue vs 本地 Web UI (Go 内置轻量静态服务 + REST API)；
- 审计智能与规则诊断交互设计 (Audit Intelligence, Top-K 规则误命中、节点归属分布图表)。

---

## Next Step

进入 **Phase 3 — Audit Intelligence & Local UI (MVP 可视化审计看板)**：
1. UI 架构选型与轻量 API 端点对接；
2. 实现会话与时间窗口选择器、监控覆盖率状态栏；
3. 实现出站代理节点、分流规则命中与进程流量排行榜看板；
4. 实现多跳中继去重可解释性下钻与证据展示。
