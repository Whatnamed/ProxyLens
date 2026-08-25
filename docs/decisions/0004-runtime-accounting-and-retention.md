# ADR 0004: Runtime Accounting Architecture, Freshness API, and Derived Retention Policy

- **Status**: Confirmed / Implemented
- **Date**: 2026-08-25
- **Deciders**: ProxyLens Core Team
- **Consulted**: Phase 2B2 Validation Team

---

## 1. Context & Problem Statement

在 ProxyLens Phase 2A (Storage Foundation) 与 Phase 2B1 (Accounting & Aggregation) 之后，系统已具备不可变事件流水记录（`event_journal`）、保守的 Relay 关联与扣减以及小时级多维聚合表。
然而，在多进程与高并发生产运行时，存在以下关键工程挑战：

1. **Ingestion 阻塞风险**: 如果 Accounting Rebuild 使用长时间的排他写事务，会导致 Collector 在高频 250ms cadence 下被 SQLite 写锁阻塞（`SQLITE_BUSY` 517）；
2. **权威边界模糊**: 实时流持续写入新事件时，如果 Rebuild 没有明确的权威序列边界，可能导致数据在计算和写入期间出现前后不一致；
3. **核算数据陈旧度（Freshness）表达**: 用户或 UI 查询时，如何明确获知当前视图相较于最新采集落后了多少事件；
4. **运行时存活检测**: Collector 异常挂起或操作系统断电时，历史记录不能永久伪装为在线；
5. **数据保留策略（Retention）安全边界**: 长期运行下的派生层清理必须绝对安全，严禁删除原始不可变证据。

---

## 2. Decision Drivers

- **采集绝对优先**: `Collector ingestion` 具有最高优先级，Analytics/Accounting 永远不能让实时采集停摆；
- **Raw Authority 永不破坏**: `event_journal`、`collector_sessions`、`connection_traffic` 与 `monitoring_gaps` 是法律与审计级的原始事实，Retention 严禁删除任何原始事实；
- **单调自增边界**: 核算必须严格绑定快照边界，并支持分批（1000 行/批）短事务写入与锁让出；
- **显式陈旧度**: UI 与 API 必须具备显式的 `Freshness` 表达，不能用无感覆盖掩盖延迟。

---

## 3. Key Technical Decisions

### 3.1 全局单调序列边界与快照捕获 (F1, F2)
- 为 `event_journal` 增加全局单调自增的 `journal_sequence INTEGER UNIQUE`；
- `RebuildAccounting` 阶段 A 仅用极短事务（<5ms）读取并锁定 `boundarySeq = MAX(journal_sequence)`，随后立即释放锁；
- 阶段 B、C、D、F 均限定在 `<= boundarySeq` 的只读快照范围内计算，且分批（`DefaultBatchSize = 1000`）采用退避重试短事务写入派生表，批次间让出写锁给实时采集器。

### 3.2 显式 Freshness / Staleness API (F3)
- 实现 `GetAccountingFreshness(ctx)`：
  - `SourceMaxSequence`: 当前有效核算 run 所覆盖的最大序列；
  - `CurrentMaxSequence`: `event_journal` 中的实际最新最大序列；
  - `LagEvents`: 当前落后事件数；
  - `IsFresh`: 当且仅当 `LagEvents == 0` 时为 true。
- `GetUsageSummary` 在响应中携带该 Freshness 状态。

### 3.3 Collector 运行时心跳与动态 Liveness Gap 判定 (F4)
- `SQLiteEventSink` 运行轻量后台心跳循环（5s 间隔），更新 `last_heartbeat_at`；
- `GetCoverage` 在查询处于 `running` 状态的会话时，若 `now - last_heartbeat_at > max(3 * interval, 15s)`，动态派生 `collector_runtime_liveness: collector_heartbeat_stale` 监控缺口，防止挂起会话伪装为 100% 覆盖。

### 3.4 纯派生层保留策略 (Safe Derived Retention, F5)
- 保留策略仅清理旧的已完成或失败的派生 runs（`accounting_runs`、`accounted_traffic`、`relay_relations`、`usage_hourly_dimensions`）；
- 保留策略 **100% 严禁删除 raw authority 数据**（`event_journal`、`collector_sessions`、`connection_traffic`、`monitoring_gaps` 等）；
- 提供 `collector storage cleanup --dry-run` 与 `--apply` 两种操作模式。

### 3.5 WAL 运维配置与 Integrity Check (F6, F11)
- SQLite 统一连接配置 `_pragma=busy_timeout=10000&_pragma=foreign_keys=ON&_pragma=synchronous=NORMAL`；
- 首次初始化时持久化 `PRAGMA journal_mode=WAL`，避免多进程重复升级模式导致的排他锁冲突；
- CLI 提供 `collector storage integrity --db <path>` 执行 `PRAGMA integrity_check` 与 `PRAGMA foreign_key_check`。

---

## 4. Consequences & Verification

- **基准测试矩阵**:
  - 1000ms Steady (100 conns): 10.1s, DB=2960 KB, WAL=0 KB, Coverage=96.9%, Integrity=PASS;
  - 500ms Churn: 10.1s, DB=3948 KB, WAL=0 KB, Coverage=97.4%, Integrity=PASS;
  - 250ms Mixed (NTP+Proxy+Direct): 10.1s, DB=3648 KB, WAL=0 KB, Coverage=97.2%, Integrity=PASS;
  - 250ms Relay-Heavy (50 pairs): 10.1s, DB=11308 KB, WAL=0 KB, Coverage=94.9%, Integrity=PASS.
- **并发核算性能**: 在 250ms 高频写入下，Rebuild 耗时 **278 ms**，Freshness 正确反映 `LagEvents=38, isFresh=false`，随后追平至 `isFresh=true, LagEvents=0`。
- **Soak 稳定性**: 30 秒高压全栈连续处理 125 帧无内存泄漏、无锁超时，数据库完整性检验为 **HEALTHY**。
- **PRODUCT A–E 确定性验收**: 全部 5 项产品核心场景验收全部通过。

Phase 2 存储、核算与运行时验证阶段正式圆满完成，系统已具备进入 Phase 3 Audit UI 开发的全部条件。
