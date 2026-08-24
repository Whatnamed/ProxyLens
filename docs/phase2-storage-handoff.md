# Phase 2 Storage & Persistence Handoff Contract (docs/phase2-storage-handoff.md)

> 本文档定义 Phase 1 Collector 输出到 Phase 2 Storage (SQLite + WAL) 的事件契约。本契约只定义事件数据规范与消费语义，不锁定 Phase 2 具体的数据库 Schema 与表结构设计。

---

## 1. 事件通信模型 (Event Model)

Collector 通过解耦的 `sink.EventSink` 接口输出强类型事件。事件流保证：
1. **单调时间有序 (Strict Timestamp Ordering)**: 同一 Collector 会话内事件按产生时间单调递增；
2. **幂等重放支持 (Deterministic Replayability)**: 每个连接事件携带唯一的 `ConnectionID` 与时间戳，支持离线 Replay 与幂等写入；
3. **分层状态与不可变性 (Layered & Immutable Event Stream)**: 原始连接事实不可变，派生归因与残差作为显式事件独立发出。

---

## 2. 标准化事件规格 (Standardized Events)

### 2.1 `ConnectionBootstrap` (冷启动/恢复首帧基线)
- **触发时机**: 会话启动首帧或 Epoch Break 重置后首帧。
- **关键字段**:
  - `ConnectionID`: string (Mihomo UUID)
  - `Timestamp`: RFC3339Nano
  - `Process`, `ProcessPath`, `Host`, `DestinationIP`, `DestinationPort`
  - `Rule`, `RulePayload`, `Chains`
  - `Route`: `DIRECT` | `PROXY` | `REJECT` | `UNKNOWN`
  - `AttributionClass`: `known_application` | `unpaired_missing_attribution` | `relay_candidate`
  - `DeltaUpload`: 0
  - `DeltaDownload`: 0
  - `CumulativeUpload`: int64 (当前累计字节，作为基线)
  - `CumulativeDownload`: int64 (当前累计字节，作为基线)
  - `PreexistingAtStart`: true
- **持久化语义**: 插入连接元数据记录，初始增量记为 0，杜绝冷启动流量暴增。

---

### 2.2 `ConnectionNew` (稳态新增连接)
- **触发时机**: 稳态运行中首次观察到的连接。
- **关键字段**:
  - `ConnectionID`: string
  - `Timestamp`: RFC3339Nano
  - `DeltaUpload`: int64 (首次捕获计数)
  - `DeltaDownload`: int64 (首次捕获计数)
  - `CumulativeUpload`: int64
  - `CumulativeDownload`: int64
  - `PreexistingAtStart`: false
- **持久化语义**: 插入新连接元数据并记录第一笔增量。

---

### 2.3 `ConnectionDelta` (存活连接增量)
- **触发时机**: 已有存活连接在连续快照帧中产生流量。
- **关键字段**:
  - `ConnectionID`: string
  - `Timestamp`: RFC3339Nano
  - `DeltaUpload`: int64 ($>0$)
  - `DeltaDownload`: int64 ($>0$)
  - `CumulativeUpload`: int64
  - `CumulativeDownload`: int64
  - `AttributionInterval`: `[start, end]` (仅在 Gap 恢复后为区间值，正常稳态为空)
  - `Precision`: `exact_snapshot` | `interval_only`
- **持久化语义**: 累加至对应连接的分时时序表或增量流水表。

---

### 2.4 `ConnectionDisappeared` (快照中连接消失)
- **触发时机**: 上一帧存在但在当前帧消失。
- **关键字段**:
  - `ConnectionID`: string
  - `Timestamp`: RFC3339Nano
  - `PossibleUnobservedTail`: true
- **持久化语义**: 将连接状态更新为 `disappeared_from_snapshot`，不假设为绝对完整关闭。

---

### 2.5 `MonitoringGapClosed` (监控断线缺口闭合)
- **触发时机**: Collector 断线重连成功并处理完首帧。
- **关键字段**:
  - `Timestamp`: RFC3339Nano
  - `AttributionInterval`: `[gap_start, gap_end]`
  - `Details.actualGapMs`: int64
- **持久化语义**: 记录一条独立的 `monitoring_gaps` 审计记录，防止断线被误计为零流量或未知流量。

---

### 2.6 `CounterEpochBreak` (内核重启/数据源重置)
- **触发时机**: 全局计数器回退 (`current < previous`)。
- **关键字段**:
  - `Timestamp`: RFC3339Nano
  - `Details`: `prevUploadTotal`, `currUploadTotal`, `prevDownloadTotal`, `currDownloadTotal`
- **持久化语义**: 截断当前 Epoch 统计周期，创建新 Epoch，禁止跨 Epoch 差分。

---

### 2.7 `SamplingResidual` (快照采样盲区残差)
- **触发时机**: 每个连续稳态采样区间。
- **关键字段**:
  - `Timestamp`: RFC3339Nano
  - `Details.globalUploadDelta`: int64
  - `Details.globalDownloadDelta`: int64
  - `Details.uniqueObservedUpload`: int64
  - `Details.uniqueObservedDownload`: int64
  - `Details.residualUpload`: int64 ($\ge 0$)
  - `Details.residualDownload`: int64 ($\ge 0$)
- **持久化语义**: 记入分时残差汇总表，供 UI 展现未捕获物理流量缺口。

---

## 3. Phase 2 实施建议

1. **存储引擎**: 推荐采用 SQLite 3 + WAL 模式（纯 Go 驱动 `modernc.org/sqlite`，零 CGO，单二进制分发）；
2. **批处理写盘 (Batch Checkpointing)**: 每 1~5 秒或每 100 条事件批量事务写入，避免高频逐条 fsync 开销；
3. **崩溃恢复**: WAL 模式配合事务提交保证进程异常终止时不损坏历史审计数据。
