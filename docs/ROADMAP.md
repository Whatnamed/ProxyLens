# ProxyLens 开发与验证路线图 (ROADMAP.md)

---

## 路线图规划原则

1. **验证导向，先核后壳**：按实际技术难点与数据验证顺序推进，严禁在底层数据未验证前提前开发复杂前端。
2. **渐进交付**：每个阶段均设立明确的目标 (Goal)、交付物 (Deliverables)、验收标准 (Acceptance) 与范围外事项 (Out of scope)。
3. **拒绝空泛排期**：本路线图聚焦于工程里程碑与质量关卡，不设置主观的时间估算。

---

## 阶段概览

```
Phase 0: 数据源验证 (Discovery & Validation)
   │
   ▼
Phase 1: 采集器原型 (Collector Prototype)
   │
   ▼
Phase 2: 持久化与一致性 (Persistence & Correctness)
   │
   ▼
Phase 3: 审计可视化 (Audit UI)
   │
   ▼
Phase 4: 待检查流量引擎 (Audit Intelligence)
   │
   ▼
Later: 长期增强 (Long-term Enhancements)
```

---

## Phase 0 — 数据源验证 (Mihomo Data Source Discovery)

### 🎯 Goal
在 Windows 11 + Mihomo (TUN) 真实环境下，深入调研并实测 Mihomo External Controller 的 WebSocket / REST 接口，验证数据完备性，彻底搞清过往旧方案中“大量 Unknown 流量”的技术成因。

### 📦 Deliverables
1. **API 数据字段实测报告**：详细记录 `process`, `processPath`, `host`, `sniffHost`, `destinationIP`, `rule`, `rulePayload`, `chains` 在不同应用场景下的表现。
2. **连接生命周期抓包样本**：捕获 TCP 长连接、短连接、UDP 突发包在 WebSocket `/connections` 中的事件流。
3. **参考实现对比备忘**：分析 MetaCubeXD Data Usage 等开源实现的优缺点与边界陷阱。

### ✅ Acceptance
- 明确验证以下场景下的数据捕获能力：
  - Windows 系统后台服务（如 NTP 同步、Windows Update）
  - 常见浏览器访问（HTTP/1.1、HTTP/2、HTTP/3 QUIC）
  - 常见桌面软件（聊天工具、IDE、下载工具）
- 形成清晰的 Unknown 归因分类表，证明 Mihomo 原生数据能够支撑可解释的审计诉求。

### 🚫 Out of scope
- 编写生产级代码
- 初始化数据库或桌面 UI

---

## Phase 1 — Collector 原型验证 (Collector Prototype)

### 🎯 Goal
实现一个极简的命令行版 Collector 原型，验证核心连接状态机、连接生命周期追踪、差值 (diff) 计算与 Controller 断线自愈能力。

### 📦 Deliverables
1. **CLI 采集器原型**：能够连接指定的 Mihomo Controller，持续监听并在内存中维护当前活跃连接哈希表。
2. **连接关闭与 Diff 算法**：在连接关闭时准确计算总传输量，支持在控制台输出审计日志。
3. **自愈与重连模块**：当 Mihomo 重启或网络波动时，能够平滑断线重连并标记中断状态。

### ✅ Acceptance
- 在 500+ 并发短连接冲击下，Collector 内存占用 $\le 30\,\text{MB}$，无内存泄漏。
- 当模拟 Mihomo 内核主动重启时，Collector 不崩溃且能在 Mihomo 恢复后 3 秒内恢复采集。
- 捕获到的单条连接字节数与实际传输量相符。

### 🚫 Out of scope
- 数据库落库
- 多维聚合统计
- 图形用户界面

---

## Phase 2 — 持久化与一致性 (Persistence & Correctness)

### 🎯 Goal
引入本地持久化存储（SQLite），解决大规模连接历史落库、Double Counting 防范、监控缺口 (Monitoring Gap) 持久化与 Unknown 精准归因。

### 📦 Deliverables
1. **SQLite 存储引擎**：设计 `connections`、`monitoring_gaps` 及必要的索引。
2. **批量事务写入队列**：支持内存缓冲与批量 Flush 机制，保障磁盘 IO 效率与并发读写安全。
3. **数据一致性验证脚本**：用于比对 Mihomo 总 Traffic 速率与 ProxyLens 入库流量的一致性。
4. **监控缺口记录器**：自动在采集中断时生成 Gap 记录。

### ✅ Acceptance
- **场景 A（后台 NTP）**：成功持久化后台 UDP 连接且关机重启后仍能查询。
- **场景 B（1GB 下载）**：大文件下载后入库流量与实际一致，无未解释的 Unknown。
- **场景 C（DIRECT 隔离）**：DIRECT 大流量准确标记，不计入 PROXY 流量池。
- **场景 D（停机缺口）**：关闭 Collector 10 分钟后启动，数据库中精确生成 10 分钟 Gap 记录。
- **场景 E（节点切换）**：上午走节点 A、下午走节点 B 的记录在数据库中各自独立且保持原样。

### 🚫 Out of scope
- 前端图表展示与交互界面
- 智能规则推荐算法

---

## Phase 3 — 审计可视化 (Audit UI)

### 🎯 Goal
开发随用随开的审计 UI 界面，提供直观的历史连接查询、多维过滤检索、流量聚合看板与监控覆盖率表达。

### 📦 Deliverables
1. **历史连接审计视图**：支持按时间范围、进程名、域名、目标 IP、规则、节点、传输协议等进行多维组合过滤与分页搜索。
2. **多维聚合看板**：展示今日/最近 7 天/自定义周期的进程排行、域名排行、规则命中排行与节点消耗排行。
3. **监控健康度与覆盖率视图**：以时间轴形式直观标出正常监控时段与监控缺口 (Gap)，展示统计周期的覆盖率百分比。
4. **单条连接溯源卡片**：清晰展示“进程 $\to$ 目标 $\to$ 规则 $\to$ 策略链 $\to$ 节点 $\to$ 流量”的因果链路。

### ✅ Acceptance
- UI 启动速度 $\le 1.5$ 秒，关闭后后台 Collector 运行不受任何影响。
- 在 10 万条历史记录下，多维条件搜索与聚合结果在 200 毫秒内渲染完成。
- 用户可一眼识别指定时间段内是否存在监控盲区。

### 🚫 Out of scope
- 自动修改系统代理或分流规则
- 云端同步

---

## Phase 4 — 待检查流量引擎 (Audit Intelligence)

### 🎯 Goal
构建基于规则与启发式特征的“待检查流量”分析引擎，主动识别可疑或低效的代理分流行为，提供规则优化建议。

### 📦 Deliverables
1. **待检查流量推荐列表**：
   - 首次走代理的后台进程
   - Windows / 安全软件后台服务走代理
   - `MATCH` 兜底规则产生的大流量
   - 宽泛 UDP 规则（如 `NETWORK,udp`）产生的代理连接
   - 纯 IP 目标且缺乏反查域名的代理大流量
   - 长期 DIRECT 突变为 PROXY 的目标
2. **规则优化辅助面板**：生成可供复制的 Mihomo 规则片段（如 `DOMAIN-SUFFIX,example.com,DIRECT`）。

### ✅ Acceptance
- 系统能够自动高亮场景 A 中的 NTP 错误代理连接，并给出推荐的 DIRECT 规则建议。
- 所有异常判断逻辑完全透明可解释，不引入不可控的黑盒算法。

### 🚫 Out of scope
- 未经用户确认直接修改 Mihomo 配置文件

---

## Later — 长期增强规划

以下功能在核心审计价值稳定后再行评估：
- **机场套餐与计费估算**：支持配置月度重置日、节点计费倍率，生成估算账单。
- **数据归档与压缩**：超过 30 天的历史明细自动降采样聚合为小时/天统计，释放存储空间。
- **历史数据导出**：支持导出 CSV / JSON 审计报表。
- **多客户端与跨平台支持**：探索 Linux / macOS 运行环境及其他兼容 External Controller 的代理内核。
