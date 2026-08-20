# ProxyLens 系统架构与技术认知 (ARCHITECTURE.md)

---

## 1. 系统边界与核心定位

ProxyLens 定位为**纯粹的旁路观察系统 (Bypass Observer)**。

### 1.1 旁路隔离原则
ProxyLens **完全不在网络数据路径中**，不代理流量、不劫持网卡、不下发路由。

$$\text{ProxyLens 故障 / 崩溃} \quad\cancel{\Longrightarrow}\quad \text{Mihomo 故障} \quad\cancel{\Longrightarrow}\quad \text{系统断网}$$

即使 ProxyLens Collector 进程异常退出，用户的实际代理连接、FLClash 界面与系统网络通信均不受任何影响。

```
┌───────────────────────────────────────────────────────────┐
│                      系统网络通信主干路径                      │
│                                                           │
│  [用户进程] ──(TUN/代理)──> [Mihomo 代理内核] ──> [远程目标]  │
└─────────────────────────────┬─────────────────────────────┘
                              │
                              │ 旁路只读观察 (External Controller)
                              ▼
┌───────────────────────────────────────────────────────────┐
│                    ProxyLens 审计系统                      │
│                                                           │
│   [Collector 后台采集器] ──> [SQLite 本地数据库] <── [UI 审计界面] │
└───────────────────────────────────────────────────────────┘
```

---

## 2. 系统组件与数据流

ProxyLens 系统划分为三个解耦的核心层级：

```
┌─────────────────────────────────────────────────┐
│         Mihomo (FLClash 内置 / 独立运行)         │
│  - WebSocket: /connections (活跃连接快照流)        │
│  - REST API:  /traffic, /version, /memory       │
└────────────────────────┬────────────────────────┘
                         │ JSON Stream
                         ▼
┌─────────────────────────────────────────────────┐
│              ProxyLens Collector (后台服务)      │
│  - 连接状态机 (Active Track / Diff 计算)          │
│  - 监控缺口检测器 (Gap Detector)                   │
│  - 内存缓冲与批量入库队列 (Batch Buffer)            │
└────────────────────────┬────────────────────────┘
                         │ WAL 批量写入
                         ▼
┌─────────────────────────────────────────────────┐
│              本地持久化数据库 (SQLite)            │
│  - connections 表 (连接历史与元数据)               │
│  - traffic_hourly_stats 表 (小时/天聚合缓存)      │
│  - monitoring_gaps 表 (监控中断时间段记录)         │
└────────────────────────┬────────────────────────┘
                         │ 只读查询 (Read-Only)
                         ▼
┌─────────────────────────────────────────────────┐
│              ProxyLens UI (随用随开)             │
│  - 历史连接检索与过滤                             │
│  - 多维聚合仪表盘                                │
│  - 待检查流量与分流优化建议                         │
│  - 监控健康度与覆盖率视图                          │
└─────────────────────────────────────────────────┘
```

### 2.1 组件职责分工

| 组件 | 运行形态 | 核心职责 | 约束与要求 |
| :--- | :--- | :--- | :--- |
| **Collector** | 后台常驻服务（静默无窗口） | 监听 External Controller、维护内存连接表、计算连接 diff、判断连接 closed、记录监控缺口、批量事务写入 SQLite。 | 极低资源占用（CPU < 1%, RAM < 30MB）；Mihomo 停止时自动挂起等待，恢复时自动重连。 |
| **Storage (SQLite)** | 本地嵌入式文件 | 存储明细数据、聚合索引与监控缺口。 | 启用 WAL 模式，支持并发只读查询与批量写入；按需自动建立时间/进程/域名索引。 |
| **UI** | 桌面客户端 / Web 前端（按需开启） | 供用户查询历史明细、查看聚合报表、分析待检查流量、查看监控覆盖率。 | 随开随关，关闭时不影响 Collector 的后台采集与落库。 |

---

## 3. Mihomo External Controller 数据源认知

ProxyLens 第一阶段完全依赖 Mihomo 官方 External Controller 接口。

### 3.1 预计消费的核心接口
1. **WebSocket `/connections`**：
   - 持续下发当前的连接状态快照（包含当前 `downloadTotal`、`uploadTotal` 以及 `connections` 活跃连接列表）。
   - 是提取单条连接元数据、分流链与生命周期状态的核心数据源。
2. **REST `/traffic`**：
   - 实时推送全局上行/下行速率，用于全局健康度校验。
3. **REST `/version` / `/configs`**：
   - 获取 Mihomo 版本号、当前运行模式 (Rule/Global/Direct) 与 Controller 配置。

### 3.2 预计读取的连接字段结构
根据 Mihomo 标准定义，每条连接包含以下字段：
```json
{
  "id": "c1a2b3c4-...",
  "metadata": {
    "network": "tcp",
    "type": "HTTP",
    "sourceIP": "198.18.0.1",
    "destinationIP": "104.16.132.229",
    "sourcePort": "54321",
    "destinationPort": "443",
    "host": "api.example.com",
    "dnsMode": "fakeip",
    "process": "ExampleApp.exe",
    "processPath": "C:\\Program Files\\Example\\ExampleApp.exe",
    "sniffHost": ""
  },
  "upload": 1024,
  "download": 20480,
  "start": "2026-08-20T20:00:00.000Z",
  "chains": ["节点选择", "香港 01 节点"],
  "rule": "DomainSuffix",
  "rulePayload": "example.com"
}
```

---

## 4. 连接增量与生命周期核心机制

如何从持续推送的 WebSocket 连接流中准确记录历史且不发生 Double Counting，是系统的核心算法难点。

### 4.1 连接生命周期状态机
1. **连接发现 (New Connection)**：
   - 在 WebSocket 帧中首次出现新的连接 `id`，在 Collector 内存中建立 `ActiveConnection` 跟踪对象，记录初始时间与元数据。
2. **连接活跃中 (Active Transferring)**：
   - Mihomo 持续更新该 `id` 的累积 `upload` / `download` 字节数。
3. **连接结束 (Closed)**：
   - 当某 `id` 在最新的一帧 WebSocket 列表中消失，判定该连接已正常关闭。
   - 计算该连接的最终持续时长与最终传输量，打包写入待落库队列。

### 4.2 长连接的阶段性增量 (Snapshot / Delta) 考量
- *场景*：某些长连接（如持续数天的下载、在线视频、后台的长轮询 WebSocket）可能长时间不关闭。
- *策略*：若仅在连接关闭时才落库，会导致 UI 无法及时统计当日流量，且一旦进程异常退出会丢失累积流量。
- *待验证机制*：对持续活跃超过特定阈值（如 5 分钟或流量 > 50MB）的长连接，支持定期产生增量差值 (Delta) 或阶段性快照，既保证实时统计，又确保最终关闭时不重复计算。

### 4.3 Mihomo 重启与连接 ID 漂移处理
- 当 Mihomo 重启时，原有的连接可能全部中断，且 Mihomo 自身的全局计数器清零。
- Collector 必须能够识别 Controller 连接断开事件，将当前内存中所有未关闭的连接做截断处理，并生成一条 `monitoring_gap` 记录。

---

## 5. 监控中断与缺口模型 (Monitoring Gap Model)

为坚决贯彻“未知必须可解释”的产品原则，系统引入显式的 **监控缺口 (Monitoring Gap)** 概念。

### 5.1 缺口产生的原因类型
- `CollectorDown`：ProxyLens Collector 进程未运行或异常崩溃。
- `ControllerUnreachable`：Mihomo External Controller 端口未开放、密码错误或 Mihomo 未启动。
- `NetworkInterfaceDown`：系统休眠、网卡禁用或系统网络彻底断开。

### 5.2 缺口数据模型
```sql
CREATE TABLE monitoring_gaps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    start_time INTEGER NOT NULL,  -- 毫秒时间戳
    end_time INTEGER,            -- 恢复时填入，未恢复前为 NULL
    reason TEXT NOT NULL,        -- CollectorDown / ControllerUnreachable 等
    detail TEXT                  -- 异常信息或退出日志
);
```

### 5.3 统计与展示语义
在计算任意时间段（如“今日 00:00 - 24:00”）的流量统计时：
$$\text{监控覆盖率} = \frac{\text{统计区间总时长} - \sum \text{区间内缺口时长}}{\text{统计区间总时长}} \times 100\%$$
UI 在展示图表时，必须在时间轴上将 Gap 区间以浅灰色斑马纹或告警条标出，明确告知用户该区间无监控数据，而非 0 流量。

---

## 6. 隐私与安全边界

1. **本地存储**：SQLite 数据库默认存放在用户本地应用数据目录（如 `%APPDATA%\ProxyLens\proxylens.db`），不配置任何外部网络同步功能。
2. **零敏感内容**：Collector 仅解析 Mihomo Metadata 层的网络与分流元数据，严禁通过任何方式窥探数据包 Payload 或 TLS 证书解密。
3. **Controller Secret 安全**：Mihomo Controller 的 Secret Token 本地加密保存或仅从受保护的配置文件中读取，不向任何第三方暴露。

---

## 7. 性能与资源目标

- **内存占用**：Collector 常驻运行工作集 (Working Set) 目标 $\le 30\,\text{MB}$。
- **CPU 占用**：在日常 100~500 个并发连接波动时，后台 CPU 占用 $\le 0.5\%$。
- **高并发写入缓解**：对于浏览器高频请求等短连接爆发场景，Collector 采用**内存队列 + 批量事务提交 (Batch Transaction)**（如每 1 秒或满 200 条批量写入一次），彻底消除 SQLite 文件锁竞争。

---

## 8. 当前技术决策 (Confirmed Decisions)

以下是当前已完全确定的技术基线：
1. **架构解耦**：确立 Collector（常驻后台采集）与 UI（按需随用随开）分离架构。
2. **数据源基线**：第一阶段数据源 100% 取自 Mihomo External Controller，不做底层抓包。
3. **数据存储基线**：采用本地嵌入式数据库（初步确定为 SQLite + WAL 模式）实现持久化。
4. **旁路设计**：ProxyLens 故障绝不影响网络可用性与 Mihomo 代理功能。
5. **显式缺口模型**：采集中断必须以 Gap 数据结构独立持久化。

---

## 9. 开放问题与待选型事项 (Open Questions)

以下事项需在 Phase 0 与 Phase 1 阶段通过实际原型验证后决定，目前**保持开放，不预先定死**：

1. **Collector 语言选型**：
   - 候选 A：**Go**（与 Mihomo 原生同源，网络 IO 与 JSON 解析生态极成熟，交叉编译简单）。
   - 候选 B：**Rust**（内存占用与 CPU 极其克制，类型系统可严密保障状态机与并发安全）。
2. **UI 技术栈选型**：
   - 候选 A：**Tauri (Rust + React / Vue / Svelte)**（轻量原生桌面窗口，资源占用低）。
   - 候选 B：**Web UI (轻量本地 HTTP 服务 + 前端 SPA)**（无桌面打包复杂度，跨端容易）。
3. **Collector 与 UI 通信方式**：
   - 方案 A：**直接共享 SQLite 数据库**（UI 直接通过只读连接查询 SQLite WAL，架构极简）。
   - 方案 B：**本地 IPC / 轻量 HTTP API**（Collector 暴露查询端口，UI 通过 API 获取数据）。
4. **大量短连接写入与存储优化**：
   - 是否需要对超短生命周期且流量极小（如 < 1KB）的特定内部探针连接进行聚合压缩存储？
5. **历史数据清理与归档策略**：
   - 本地数据库保留策略（例如：明细保留 30 天，30 天以上自动降采样为小时级聚合数据）。

---

## 10. 主要技术风险与缓解方案

| 风险点 | 风险描述 | 预期缓解策略 |
| :--- | :--- | :--- |
| **WebSocket 帧丢失 / 粘包** | 网络或进程高负载时，WebSocket 连接快照推送可能出现延迟或连接重置。 | 设计具备重连自愈能力的 WebSocket 客户端，重连后拉取完整快照重新校准内存状态。 |
| **Mihomo 字段不一致** | 不同版本的 Mihomo 或特殊配置下，`processPath` 或 `sniffHost` 可能为空。 | 数据层使用 Optional / Nullable 模式，并建立归因标注机制（如 `MissingProcess`）。 |
| **SQLite 写入锁争用** | 高频并发写入导致 UI 查询被阻塞或报 `database is locked`。 | 强制开启 `PRAGMA journal_mode = WAL;`，严格实施单写多读与批量提交。 |
