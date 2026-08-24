# ADR 0002: Storage Engine and SQLite Persistence Strategy Selection

## Context

ProxyLens 需要为 Windows + Mihomo 的流量审计提供可靠、低开销的本地持久化存储。Collector 产生强类型事件流（`ConnectionBootstrap`, `ConnectionNew`, `ConnectionDelta`, `ConnectionMetadataUpdated`, `ConnectionDisappeared`, `MonitoringGapOpened/Closed`, `CounterEpochBreak`, `SamplingResidual`, `CollectorHealth`）。

核心产品与架构要求：
1. **Durable Authority**: 原始事件必须原子持久化至 `event_journal`，作为不可变的事实层；
2. **可重建投影 (Rebuildable Projections)**: `connections` 维度表与 `connection_traffic` 时序表等派生视图必须能从 `event_journal` 完整无损重建；
3. **Single Writer, Concurrent Readers**: Collector 作为单写入进程持续落盘，后续 UI 客户端随用随开并发查询，不互相阻塞；
4. **Windows 免编译单二进制分发**: 避免 CGO 依赖及 MSVC/MinGW 工具链限制。

---

## Decision

**选定 SQLite + WAL 模式作为 ProxyLens Phase 2 的核心持久化引擎，采用纯 Go 驱动 `modernc.org/sqlite`。**

### 核心技术决策：
1. **纯 Go 驱动 (`modernc.org/sqlite`)**:
   - Zero CGO：纯 Go 编译，完美支持 Windows 11 AMD64 单二进制分发，彻底消除跨编译与动态链接库分发难题；
   - 兼容标准 `database/sql` 接口。
2. **WAL 模式 (`PRAGMA journal_mode=WAL;`)**:
   - 读写互不阻塞：Collector 写入 WAL 时，UI 只读连接可并发查询历史与实时快照；
   - 写入追加优化：单写事务追加至 WAL，减少随机磁盘 I/O。
3. **Synchronous 策略 (`PRAGMA synchronous=NORMAL;`)**:
   - 在 WAL 模式下，`NORMAL` 保证事务原子性与崩溃一致性（进程崩溃或非正常终止不会损坏数据库），同时显著减少磁盘 `fsync` 延迟；
   - 结合 `PRAGMA busy_timeout=5000;` 与 `PRAGMA foreign_keys=ON;` 保证完整性。
4. **单事件短事务强一致写入 (Per-Event Single Transaction)**:
   - 坚持 Correctness-First：每个 `CollectorEvent` 及其对应的 Journal 插入、Projection 更新和游标推进在单个数据库事务内原子提交；
   - 暂不引入可能破坏 Fail-Stop 语义与导致状态向后漂移的异步批处理队列。

---

## Consequences

- 生产存储模块组织在 `collector/pkg/storage/`；
- 所有事件先写 `event_journal`，再同步更新投影；
- 提供 `RebuildProjections` 支持从 Journal 100% 重建所有派生视图；
- 全栈性能基准测试与长效持久化验证在 Phase 2B 统一进行。
