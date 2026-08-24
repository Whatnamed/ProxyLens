# ProxyLens Collector RFC (docs/collector-rfc.md)

## 1. 系统定位与核心设计原则

ProxyLens Collector 是面向 Windows + Mihomo 的轻量、只读、旁路流量审计采集引擎。

### 核心原则：
1. **只读旁路 (Read-Only Observer)**: 仅调用只读查询接口（`GET /version`, `GET/WS /connections`, `GET/WS /traffic`），严禁包含任何修改配置、切换节点、重启内核的写入逻辑；
2. **准确性与可解释性 (Correctness & Explainability)**: 严格区分已知应用连接、未配对缺失归因连接、底层中继候选/去重连接与采样残差，不使用笼统桶掩盖监控事实；
3. **确定性状态机 (Deterministic State Machine)**: 冷启动/断线恢复首帧建立 Baseline、稳态单调差分、消失连接不虚构尾部字节、计数器回退与重启触发 Epoch Boundary；
4. **背压与韧性 (Backpressure & Resilience)**: 有界队列缓冲，遇过载显式降级并记录告警，指数退避受控重连。

---

## 2. 模块边界与架构划分

```text
collector/
  cmd/
    collector/              # 生产 CLI 入口 (run, replay, benchmark)
  pkg/
    config/                 # CLI 与环境变量配置 (安全脱敏)
    client/                 # 只读 Controller WebSocket/HTTP 客户端
    types/                  # 核心数据模型 (Snapshot, Frame, Event)
    state/                  # 状态机 (Session, Bootstrap, Delta, Disappearance, Epoch)
    attribution/            # 分层归因、Relay 配对与残差计算
    queue/                  # 有界队列与背压过载保护
    sink/                   # 事件接收器接口与内存/回放分发实现
```

---

## 3. 核心数据模型 (Core Data Model)

### 3.1 原始观测模型 (Raw Observation Model)

```go
type ConnectionSnapshot struct {
    ID                string   `json:"id"`
    Start             string   `json:"start"`
    Network           string   `json:"network"`
    Type              string   `json:"type"`
    SourceIP          string   `json:"sourceIP"`
    SourcePort        string   `json:"sourcePort"`
    DestinationIP     string   `json:"destinationIP"`
    DestinationPort   string   `json:"destinationPort"`
    Host              string   `json:"host"`
    SniffHost         string   `json:"sniffHost"`
    RemoteDestination string   `json:"remoteDestination"`
    Process           string   `json:"process"`
    ProcessPath       string   `json:"processPath"`
    Upload            int64    `json:"upload"`
    Download          int64    `json:"download"`
    Chains            []string `json:"chains"`
    ProviderChains    []string `json:"providerChains"`
    Rule              string   `json:"rule"`
    RulePayload       string   `json:"rulePayload"`
}
```
*注：`Host`, `DestinationIP`, `RemoteDestination`, `SniffHost` 必须作为独立字段保存，禁止合并覆盖。*

### 3.2 归因分层与事件模型 (Attribution & Event Model)

```go
type AttributionClass string

const (
    ClassKnownApplication       AttributionClass = "known_application"
    ClassUnpairedMissingAttr    AttributionClass = "unpaired_missing_attribution"
    ClassRelayCandidate         AttributionClass = "relay_candidate"
    ClassConfirmedRelayDuplicate AttributionClass = "confirmed_relay_duplicate"
    ClassOtherObservedUnique    AttributionClass = "other_observed_unique"
    ClassSamplingResidual       AttributionClass = "sampling_residual"
    ClassMonitoringGap          AttributionClass = "monitoring_gap"
)
```

---

## 4. 状态机规范 (State Machine Specification)

### 4.1 会话生命周期状态 (Session Lifecycle)
- `Starting` $\rightarrow$ `Connecting` $\rightarrow$ `Bootstrap` $\rightarrow$ `Healthy` $\rightarrow$ `ReconnectBackoff` $\rightarrow$ `Recovering` $\rightarrow$ `Stopped`

### 4.2 连接生命周期与计数器差分
1. **Bootstrap 首帧**：
   - 所有已有连接 ID 记录为基线：`lastUpload = current.Upload, lastDownload = current.Download`；
   - 产生增量 `delta = 0`，标记 `preexisting_at_session_start = true`（杜绝冷启动突增）；
2. **稳态新增连接 (Steady-State New ID)**：
   - 记录 `firstObservedCounter = current`，产生的增量 `delta = current`；
3. **稳态已有连接更新 (Existing ID Update)**：
   - `deltaUpload = current.Upload - previous.Upload`；
   - `deltaDownload = current.Download - previous.Download`；
   - 若出现任何 Per-ID 计数器回退（`delta < 0`），触发 `connection_counter_regression` 异常事件，严禁简单 `max(0, delta)`；
4. **消失连接 (Disappearance)**：
   - 状态标记为 `disappeared_from_snapshot`，`possible_unobserved_tail = true`；严禁假定为绝对完整关闭；
5. **Epoch Break (计数器回退/内核重启)**：
   - 若 `uploadTotal < prevUploadTotal || downloadTotal < prevDownloadTotal`，标记 `counter_epoch_break`；
   - 结束当前 Epoch，禁止跨 Epoch 差分，重新进入 Bootstrap。

### 4.3 Monitoring Gap 与恢复语义
- 记录 `gap_start = lastHealthyTimestamp`, `gap_end = firstHealthyPostRecoveryTimestamp`；
- 跨 Gap 存活连接增量归属于 `attribution_interval = [gap_start, gap_end]`；
- 恢复后首次出现的连接，以首帧作为基线，禁止把全部历史流量计为恢复瞬间爆发。

---

## 5. 客户端与重连机制 (Client & Reconnect)

- **只读约束**：客户端仅发起 `GET` 与 `WebSocket` 连接，无任何写操作；
- **重连退避**：初始退避 500ms，最大退避 10s，随机抖动 20%；
- **前置健康检查**：重连后首先发起 `GET /version` 校验 Controller 连通性，再建立 WebSocket 流。

---

## 6. 背压与队列管理 (Backpressure & Queue)

- 采用有界通道（默认缓冲 100 帧快照）；
- 当消费端（如存储写入）出现拥塞且队列满时，触发 `collector_overload` 降级事件并记录监控完整度缺口，禁止静默丢弃。
