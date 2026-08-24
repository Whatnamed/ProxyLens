# RFC: ProxyLens Phase 1 Go Collector Architecture & Contracts (docs/collector-rfc.md)

> 本 RFC 规范 ProxyLens 生产 Collector 原型的系统边界、并发模型、状态机契约与事件流规范。

---

## 1. 架构定位与职责边界

ProxyLens Collector 是 Windows + Mihomo 环境下的轻量级旁路流量审计采集器：

```text
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│                                   Collector Subsystem                                    │
│                                                                                          │
│   ┌───────────────────────────┐         ┌────────────────────────┐                       │
│   │ Controller Client         │         │ Bounded Queue          │                       │
│   │ (gorilla/websocket)       │ ──────> │ chan *types.IngestItem │                       │
│   │ - WS /connections?interval│         │ - Guaranteed Enqueue   │                       │
│   │ - Stream Idle Watchdog    │         │ - Backpressure Block   │                       │
│   └───────────────────────────┘         └───────────┬────────────┘                       │
│                                                     │ Pop(ctx) (Single Worker)           │
│                                                     ▼                                    │
│                                         ┌────────────────────────┐                       │
│                                         │ StateEngine            │                       │
│                                         │ - Deterministic Sort   │                       │
│                                         │ - Fail-Stop on Error   │                       │
│                                         │ - Relay 1-to-1 Pairing │                       │
│                                         └───────────┬────────────┘                       │
│                                                     │ Emit()                             │
│                                                     ▼                                    │
│                                         ┌────────────────────────┐                       │
│                                         │ EventSink              │                       │
│                                         │ (ProductionStatsSink)  │                       │
│                                         └────────────────────────┘                       │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

### 核心原则与非目标：
1. **只读旁路**: 严禁调用任何 mutating API（无 POST/PUT/PATCH/DELETE），不影响网络数据路径；
2. **单一有序 Ingestion 通道**: 快照帧 (`ItemFrame`)、断线标记 (`ItemGapOpened`) 与健康事件 (`ItemCollectorHealth`) 统一推入有界队列由单 Worker 串行消费，彻底根除并发时序竞争；
3. **阻塞 Backpressure 与 Fail-Stop**: 队列满时阻塞生产者施加 Backpressure；底层 Sink 出错时立即停机，防止状态前进与数据丢失；
4. **`/traffic` 定位**: 作为可选后续对账辅助工具，不参与 Phase 1 核心状态流。

---

## 2. 状态机与事件契约 (State Machine & Event Contract)

### 2.1 权威序列与确定性
- **权威顺序**: `SessionID + EpochID + FrameSequence + EventSequence`；
- **Observational Wall Time**: `Timestamp` 记录观测到的系统时间，允许跳变，不作为权威排序；
- **确定性排序**: 稳态遍历严格保持快照切片顺序；消失连接收集后执行 `sort.Strings(ids)` 排序发出；
- **确定性 EventID**: 基于四元组与事件类型、连接 ID 及增量哈希计算，支持 50+ 次回放比特级一致。

### 2.2 归因分类与 Relay 保守去重
- **Initial Attribution**:
  - `known_application`: 进程且规则均存在；
  - `relay_candidate`: 无进程且无规则，但包含代理链；
  - `unpaired_missing_attribution`: 其他未归因连接；
- **Relay 1-to-1 确凿配对**:
  - 仅当 candidate 仅匹配唯一 1 个 logical，且该 logical 也仅匹配该 candidate 时，才确认为 `confirmed_relay_duplicate`；
  - 存在 1-to-N 或 N-to-1 歧义时，保守保留在 candidate，不执行去重扣减；
- **全局残差**: $\text{Residual} = \Delta\text{global} - \text{UniqueObserved}$（其中 $\text{UniqueObserved} = \text{KnownApp} + \text{UnpairedMissingAttr} + \text{OtherUnique}$）。

### 2.3 监控缺口与恢复
- **GapOpened**: 记录 `gap_start`（最后一次健康处理的快照时间）；
- **GapClosed**: 记录 `gap_start`, `gap_end`, `actualGapDurationMs`（基于 Go monotonic 时间计算），以及 `globalGapUploadDelta` 与 `globalGapDownloadDelta`；
- **跨 Gap Epoch Break**: 重连首帧若检测到计数器回退，标记 `gapPhysicalDeltaUnavailable: true` 并发出 `CounterEpochBreak`。
