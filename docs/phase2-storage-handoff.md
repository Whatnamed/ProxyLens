# Phase 2 Storage & Persistence Handoff Contract (docs/phase2-storage-handoff.md)

> 本文档定义 Phase 1 Collector 输出到 Phase 2 Storage (SQLite + WAL) 的严格事件契约。本契约只定义事件数据规范、顺序保证与消费语义，不锁定 Phase 2 内部具体的数据库表结构与索引设计。

---

## 1. 事件通信与顺序保证模型 (Event & Ordering Model)

Collector 通过解耦的 `sink.EventSink` 接口输出强类型事件。事件流提供以下核心保证：

1. **单调序列权威有序 (Authoritative Sequence Ordering)**:
   - 事件流的权威顺序由 `FrameSequence`（单调递增帧号）与 `EventSequence`（单帧内单调递增序号）决定；
   - 时间戳 `Timestamp` 单调非递减（同一快照帧内的多个事件时间戳允许相等，Phase 2 恢复与重放必须以 `FrameSequence + EventSequence` 为准）。
2. **确定性幂等重放标识 (Deterministic Idempotency Key)**:
   - 每个事件携带确定性哈希生成的 `EventID`（基于 `SessionID|EpochID|FrameSequence|EventSequence|Type|ConnectionID|DeltaUp|DeltaDown` 计算），支持离线 Replay 与幂等写入去重。
3. **分层状态与不可变性 (Immutable Raw Evidence & Derived Views)**:
   - 原始连接事实不可变，派生归因与残差作为显式事件独立发出；
   - 计数器语义完全分离：`ObservedUploadCounter` / `ObservedDownloadCounter`（Mihomo 原始读数）与 `MonitoredCumulativeUpload` / `MonitoredCumulativeDownload`（自当前 Baseline 以来的监控增量累加值）严格隔离。

---

## 2. 标准化事件规格 (Standardized Events)

### 2.1 `ConnectionBootstrap` (冷启动/恢复首帧基线)
- **触发时机**: 会话启动首帧、Epoch Break 重置后首帧，或 Gap 期间新增连接的首次观察。
- **关键字段**:
  - `EventID`, `SessionID`, `EpochID`, `FrameSequence`, `EventSequence`, `Timestamp`
  - `ConnectionID`: string (Mihomo UUID)
  - `Metadata`: 包含 17 个原始字段（`Process`, `ProcessPath`, `Host`, `SniffHost`, `Network`, `Type`, `SourceIP`, `SourcePort`, `DestinationIP`, `RemoteDestination`, `DestinationPort`, `DnsMode`, `SpecialProxy`, `SpecialRules`, `InboundUser`, `InboundName`, `InboundPort`）
  - `QualityFlags`: 结构化质量标记（`MissingProcess`, `MissingProcessPath`, `MissingHost`, `IPOnly`, `MissingRule`, `MissingChain`）
  - `MihomoStart`: string (Mihomo 记录的连接开始时间)
  - `Rule`, `RulePayload`, `Chains`, `ProviderChains`
  - `Route`: `DIRECT` | `PROXY` | `REJECT` | `UNKNOWN`
  - `AttributionClass`: `known_application` | `unpaired_missing_attribution` | `relay_candidate` | `confirmed_relay_duplicate`
  - `ObservedUploadCounter`, `ObservedDownloadCounter`: 原始快照计数
  - `BaselineUploadCounter`, `BaselineDownloadCounter`: 作为 Baseline 的计数
  - `DeltaUpload`: 0, `DeltaDownload`: 0
  - `MonitoredCumulativeUpload`: 0, `MonitoredCumulativeDownload`: 0
  - `PreexistingAtStart`: bool
  - `Precision`: `"exact_snapshot"` | `"gap_post_baseline"`
- **持久化语义**: 插入连接元数据与初始基线记录，增量记为 0，杜绝历史流量爆发。

---

### 2.2 `ConnectionNew` (稳态新增连接)
- **触发时机**: 稳态运行中首次观察到的连接。
- **关键字段**:
  - 同上元数据与质量标记
  - `DeltaUpload`, `DeltaDownload`: int64 (首次捕获计数)
  - `MonitoredCumulativeUpload`, `MonitoredCumulativeDownload`: int64 (等于首次计数)
  - `PreexistingAtStart`: false
- **持久化语义**: 插入新连接元数据并记录首笔增量。

---

### 2.3 `ConnectionDelta` (存活连接增量)
- **触发时机**: 已有存活连接在连续快照帧中产生流量。
- **关键字段**:
  - `ConnectionID`, `ObservedUploadCounter`, `ObservedDownloadCounter`
  - `DeltaUpload`, `DeltaDownload`: int64 ($\ge 0$)
  - `MonitoredCumulativeUpload`, `MonitoredCumulativeDownload`: int64
  - `AttributionInterval`: `[gap_start, gap_end]`（仅在 Gap 恢复后非空）
  - `Precision`: `"exact_snapshot"` | `"interval_only"`
- **持久化语义**: 累加至对应连接的分时增量时序表。

---

### 2.4 `ConnectionDisappeared` (快照中连接消失)
- **触发时机**: 上一帧存在但在当前帧消失。
- **关键字段**:
  - `ConnectionID`, `Metadata`, `QualityFlags`
  - `PossibleUnobservedTail`: true
- **持久化语义**: 将连接状态标记为 `disappeared_from_snapshot`（不假设为绝对完整关闭）。

---

### 2.5 `RelayClassificationChanged` (中继归因更新)
- **触发时机**: 底层连接与顶层应用连接完成确凿配对（或解除配对）。
- **关键字段**:
  - `ConnectionID`, `AttributionClass`: `confirmed_relay_duplicate`
  - `Details`: 包含配对证据对象（`candidateId`, `logicalId`, `sharedHops`, `structuralRelation`, `uploadMatch`, `downloadMatch` 等）
- **持久化语义**: 更新连接的归因状态，从 `UniqueObserved` 统计视图中剔除。

---

### 2.6 `MonitoringGapOpened` & `MonitoringGapClosed` (监控断线缺口)
- **触发时机**:
  - `Opened`: Controller 失去响应并进入重连退避；
  - `Closed`: 重连成功并完成恢复首帧处理。
- **关键字段**:
  - `AttributionInterval`: `[gap_start, gap_end]`（`gap_start` 严格为最后成功处理的健康快照时间戳）
  - `Details.actualGapMs`: int64
  - `Details.gapPhysicalDeltaUnavailable`: bool（若 Gap 期间发生 Epoch Break 则为 true）
- **持久化语义**: 写入独立的 `monitoring_gaps` 审计表，防止断线被误判为未知流量或零流量。

---

### 2.7 `CounterEpochBreak` (数据源重启/计数器重置)
- **触发时机**: 全局计数器回退 (`current < previous`)。
- **关键字段**:
  - `Details`: `prevUploadTotal`, `currUploadTotal`, `prevDownloadTotal`, `currDownloadTotal`, `acrossGap`
- **持久化语义**: 截断当前 Epoch 统计周期，创建新 Epoch，禁止跨 Epoch 差分。

---

### 2.8 `SamplingResidual` (轮询采样盲区残差)
- **触发时机**: 每个连续稳态采样区间。
- **关键字段**:
  - `Details.globalUploadDelta`, `Details.globalDownloadDelta`
  - `Details.uniqueObservedUpload`, `Details.uniqueObservedDownload`
  - `Details.residualUpload`, `Details.residualDownload` ($\text{Residual} = \Delta\text{global} - \text{UniqueObserved}$)
- **持久化语义**: 记入分时残差汇总表，供 UI 展现未捕获物理流量缺口。

---

### 2.9 `CollectorHealth` (完整度与健康告警)
- **触发时机**: 发生反序列化错误、队列过载降级、单连接计数器回退等异常。
- **关键字段**:
  - `Details.issue`, `Details.details`
- **持久化语义**: 记录采集器健康日志。

---

## 3. Sink 故障与 Fail-Safe 语义

- `StateEngine` 在每次调用 `sink.Emit()` 时均检查返回的 error；
- 一旦底层存储写入失败（如磁盘满、SQLite 锁死），`StateEngine` 立即终止状态推进并返回错误，杜绝状态前进但存储丢事件导致的数据不一致。
