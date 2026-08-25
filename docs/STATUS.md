# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 2 Complete — Storage, Accounting & Runtime Validation Finalized (Ready for Phase 3 Audit UI)`
- **代码状态**：Phase 2 存储、版本化核算、运行时生命周期与安全保留策略已全部落地并验证通过：
  1. **非阻塞绑定序列核算重建 (F1 / F2, ADR 0004)**：引入全局单调自增 `journal_sequence` 与 `accounting_runs.source_journal_sequence_max`；阶段 A 仅用 <5ms 短事务锁定边界，阶段 B~F 采用分批（1000 行/批）退避重试短事务写入并主动让锁，实时采集在 250ms 高频写入下零阻塞；
  2. **显式 Freshness / Staleness API (F3)**：实现 `GetAccountingFreshness(ctx)`，精准返回 `LagEvents` 与 `IsFresh` 状态，并在 `GetUsageSummary` 中提供；
  3. **Collector 心跳与运行时存活检测 (F4)**：`SQLiteEventSink` 运行 5s 后台轻量心跳并具备安全 stop/join 机制；`GetCoverage` 在会话处于 `running` 但心跳超时时动态派生 `collector_runtime_liveness: collector_heartbeat_stale` 监控缺口；
  4. **安全派生层保留策略 (F5, Safe Derived Retention)**：实现 `PlanDerivedRetention` 与 `ApplyDerivedRetention`，采用 1000 行/批短事务清理旧 completed/failed 派生运行，**100% 保证 raw authority 数据（Journal, Sessions, Gaps）永不被删除**；
  5. **WAL 运维与 SQLite 完整性保障 (F6 / F11)**：DSN 统一配置 `synchronous=NORMAL`, `busy_timeout=10000`, 并在 CLI 提供 `collector storage integrity` 执行 `PRAGMA integrity_check` 与 `foreign_key_check`（含 `rows.Err()` 校验）；
  6. **全栈性能基准实测矩阵 (F7 / F8 / F9, 标准 >=30s 每组实测)**：
     - 1000ms Steady (100 conns, 30s): 30 帧, DB=8536.0 KB, Peak WAL=4124.1 KB, Coverage=97.9%, Integrity=PASS;
     - 500ms Churn (50 conns, 30s): 60 帧, DB=8120.0 KB, Peak WAL=4164.3 KB, Coverage=98.9%, Integrity=PASS;
     - 250ms Mixed (NTP+Proxy+Direct, 30s): 117 帧, DB=7008.0 KB, Peak WAL=4116.0 KB, Coverage=98.7%, Integrity=PASS;
     - 250ms Relay-Heavy (50 pairs, 30s): 118 帧, DB=32804.0 KB, Peak WAL=4140.1 KB, Coverage=95.2%, Integrity=PASS;
     - 并发 Rebuild 耗时: 持续 250ms 写入下 Non-blocking Rebuild 耗时 **213 ms**，Freshness 正确识别 `LagEvents=101, isFresh=false`，追平后 `isFresh=true, LagEvents=0`，全局 Journal 序列严格单调连续无空洞 (Journal Continuity Invariant: PASS);
     - Soak 稳定性: 30s Sanity 高压处理 118 帧，队列溢出为 0，Post-Soak 完整性为 **HEALTHY**（10min 长期认证模式保持参数可选）；
     - CPU / RSS: 显式标记为 `unavailable`（未附加系统级探针，不作主观估计）；
  7. **PRODUCT A–E 全字段确定性验收 (F10)**：所有 5 项产品核心场景按 PRODUCT.md 逐字段机械断言 100% PASS（含 NTP 端口独立、1GB 大文件各元数据字段与策略组精确对齐、DIRECT 隔离、中断缺口与节点历史锁定）；
  8. **测试套件覆盖**: 全部 52 个 Go 测试 (test: 9, state: 10, storage: 33) + 18 个 Phase 0 回归测试 100% PASS。
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

- UI 技术选型：Tauri + React/Vue vs 本地轻量 Web UI (Go 内置轻量静态服务 + REST API)；
- UI 与 Collector 通信契约：共享 SQLite 只读连接 vs 本地轻量 IPC / HTTP 查询端点。

---

## Next Step

进入 **Phase 3 — Audit UI (MVP 可视化审计看板)**：
1. UI 技术路线选型与轻量 API 端点对接；
2. 会话与时间窗口选择器、监控覆盖率状态栏；
3. 出站代理节点、分流规则命中与进程流量排行榜看板；
4. 多跳中继去重可解释性下钻与证据展示。
*(注：Audit Intelligence 智能规则诊断与异常发现将在 Phase 4 开展)*
