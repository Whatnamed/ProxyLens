# MetaCubeXD Data Usage 参考实现审阅报告 (metacubexd-reference.md)

> **前言**：本报告对 MetaCubeXD（当前 `main` 分支）的 Data Usage 与 Connections 状态管理源码进行有限范围的独立技术审阅，旨在识别已有开源实现的算法思路与潜在陷阱，为 ProxyLens 的 Phase 0 验证与后续设计提供参考。
> 
> *声明：ProxyLens 保持完全独立设计，不 fork、不 vendor 亦不依赖 MetaCubeXD。*

---

## 一、核心实现机制审阅

### 1. Connection Traffic Delta 计算机制
- **实现位置**：`packages/ui/stores/connections.ts` (`updateDataUsage`)
- **实现逻辑**：
  - 维护内存 `Map<string, { upload: number; download: number }>`（变量名 `connectionLastData`），以连接 `id` 作为 Key。
  - 每秒收到 WebSocket `/connections` 消息时遍历 `activeConns`。
  - 若 `connectionLastData` 中存在该 ID，则取非负差值：
    ```ts
    uploadDelta = Math.max(0, currentUpload - lastData.upload)
    downloadDelta = Math.max(0, currentDownload - lastData.download)
    ```
  - 更新 `connectionLastData.set(conn.id, { upload: currentUpload, download: currentDownload })`。
  - 将产生的 delta 累加到分钟级内存聚合缓冲 `logBuffer` 中。

### 2. 首次观察连接的处理方式 (First Observation)
- **实现逻辑**：
  - 当新连接首次出现在 WebSocket 帧中时（`!lastData`），MetaCubeXD 执行：
    ```ts
    uploadDelta = currentUpload
    downloadDelta = currentDownload
    ```
- **关键风险与陷阱**：
  - **冷启动 / 页面刷新虚增 (Double Counting / False Burst)**：若用户在中途打开或刷新 MetaCubeXD 页面，当前 Mihomo 中已存在的长连接（如已传输 1GB 的下载任务）其 `currentDownload` 已经为 1GB。MetaCubeXD 会在第一帧把历史累计的 1GB 全部当成当前这一分钟的新增 delta 写入数据库，造成流量剧烈虚增。

### 3. 连接消失的处理方式 (Connection Cleanup)
- **实现逻辑**：
  - 提取当前活跃连接 ID 集合：`activeIds = new Set(activeConns.map(c => c.id))`。
  - 调用 `cleanupInactiveConnections(activeIds)`，遍历 `connectionLastData`，将不在 `activeIds` 中的连接直接从 Map 中 `delete`。
- **关键风险与陷阱**：
  - 连接从活跃列表消失后，仅清除其内存状态，不记录单条连接的终态元数据或完整生命周期（它只保存聚合后的分钟流量日志，连接明细直接丢弃）。

### 4. 内核/服务重启判断 (Restart Detection)
- **实现逻辑**：
  - 缓存上一帧的全局总量：`lastUploadTotal` 与 `lastDownloadTotal`。
  - 若当前帧全局总量小于上一帧，判定为内核重启：
    ```ts
    if (currentUploadTotal < lastUploadTotal || currentDownloadTotal < lastDownloadTotal) {
      resetConnectionTracking()
      clearDataUsage()
      globalStore.clearChartHistory()
    }
    ```
- **关键风险与陷阱**：
  - **清空历史数据库（不可接受的灾难性处理）**：`clearDataUsage()` 会直接调用 `await db.clearAll()`，将 IndexedDB 中过去记录的所有流量日志**彻底清空**。在审计场景下，重启应记录监控缺口 (Monitoring Gap)，绝不能抹除已有历史。

### 5. 持久化与 Flush 机制
- **实现位置**：`packages/ui/stores/connections.ts` + `packages/ui/utils/db.ts`
- **实现逻辑**：
  - 内存缓冲 `logBuffer = new Map<string, DataUsageLog>()`。
  - 内存 Key 采用紧凑的分隔符字符串避免 `JSON.stringify` 开销：
    ```ts
    const getDataUsageBufferKey = (log: DataUsageLog) =>
      `${log.timestamp}\x1F${log.sourceIP}\x1F${log.host}\x1F${log.outbound}\x1F${log.process}\x1F${log.inboundUser}`
    ```
  - 每 30 秒（时间戳对齐）触发一次 `flushLogs()`，调用 `db.addLogs(logsToFlush)` 写入 IndexedDB `data_usage_logs` 表，并根据 retention 期限执行 `db.cleanup()`。

### 6. 历史数据实际保存字段
- **实现位置**：`packages/ui/utils/db.ts` (`DataUsageLog`)
- **实际保存字段**：
  ```ts
  export interface DataUsageLog {
    id?: number
    timestamp: number    // 分钟起始时间戳 (对齐到 60000ms)
    sourceIP: string     // 来源 IP 或 'Inner'
    host: string         // metadata.host || metadata.destinationIP
    outbound: string     // chains[0] ?? 'DIRECT'
    process: string      // metadata.process || 'Unknown'
    inboundUser: string  // inbound 标识或 'Unknown'
    upload: number       // 该分钟该维度的累计上传增量
    download: number     // 该分钟该维度的累计下载增量
  }
  ```
- **关键审计字段缺失**：
  - ❌ 彻底丢弃 `rule`（规则类型）与 `rulePayload`（匹配内容）。
  - ❌ 彻底丢弃完整 `chains`（代理策略链）。
  - ❌ 彻底丢弃 `network`（TCP/UDP）、`destinationPort`、`processPath`、`sniffHost`。
  - ❌ 混淆 `host` 与 `destinationIP`（有 host 时丢弃 IP，无 host 时 IP 充当 host）。
  - ❌ 丢失单条连接粒度（无 UUID、无独立起止时间与时长）。

### 7. outbound / chains 的解释方式
- **源码事实 (Documented from MetaCubeXD implementation)**：
  ```ts
  outbound: conn.chains[0] ?? 'DIRECT'
  ```
  MetaCubeXD 在其数据模型中直接提取 `conn.chains[0]` 作为其 `outbound` 字段，并在缺失时回退为 `'DIRECT'`。
- **待验证推论 (Inferred / To be observed in Phase 0C)**：
  `chains[0]` 是否等价于最终出站物理节点，以及 `chains` 数组在 DIRECT、单层直连代理、多层嵌套/链式代理时的元素顺序与语义，不能仅凭第三方前端的单方面映射来假定，必须由 Phase 0C 捕获到的 Mihomo 真实连接原始样本进行严格验证。第三方实现不能定义 Mihomo 官方语义。

---

## 二、对 ProxyLens 的对比与启示

### 8. 值得 ProxyLens 借鉴/验证的设计
1. **内存聚合 Composite Key 性能优化**：使用非打印字符（如 `\x1F`）进行多维字段拼接作为 Map Key，相比 `JSON.stringify` 显著减少 CPU 占用与垃圾回收开销。
2. **时间窗口对齐与批量 Flush**：对齐到时间边界（如每分钟、每 30 秒）进行批量事务写入，有效降低数据库写放大。
3. **响应式浅层引用 (shallowRef)**：高频更新的连接快照避免深度响应式代理（Deep Proxy），降低内存和渲染开销。

### 9. 明确不能直接采用的设计
1. ❌ **重启删库逻辑**：Mihomo 重启触发 `db.clearAll()` 抹除全部历史数据。
2. ❌ **冷启动虚增**：未建立初始 baseline 就直接将连接的已传输总量视为 delta。
3. ❌ **过度聚合丢弃审计明细**：完全丢弃单连接元数据，无法回答“刚才那次请求为什么走代理”。
4. ❌ **未经实测的 Outbound 映射**：直接假设 `chains[0]` 代表出站节点，存在掩盖真实代理链路的风险。
5. ❌ **UI 与采集强耦合在浏览器内**：网页关闭即停止采集，无法实现后台持续守护。

### 10. MetaCubeXD 实现 $\neq$ Mihomo 官方语义
- **事实澄清 1**：MetaCubeXD 数据库中没有 `rule` 和 `chains`，是其自身为缩减前端 IndexedDB 存储所做的裁切设计，**并不代表 Mihomo API 不提供这些字段**。
- **事实澄清 2**：MetaCubeXD 把 `chains[0]` 当 outbound 是其自身的展示设计选择，Mihomo 原生 `chains` 数组的真实语义与顺序必须通过原始样本验证。
- **事实澄清 3**：MetaCubeXD 在页面重载时把连接累计量算为当前增量，属于前端采集生命周期的缺陷，不能误认为 Mihomo 推送的是瞬时增量。

---

## 三、结论与后续关注点

MetaCubeXD 的 Data Usage 定位是**轻量级 Web 前端辅助统计**，而非系统级审计工具。ProxyLens 在 Phase 0 与后续设计中，必须：
1. 在 Phase 0 中通过真实探针捕获 Mihomo 原始 `chains`、`rule`、`rulePayload` 的真实完整结构；
2. 为 Collector 建立健壮的会话初始化基线（Initial Baseline），避免首次抓取时的 double-counting；
3. 将内核重启与断线显式记录为 `monitoring_gaps`，坚决保护历史数据的持久与完整。
