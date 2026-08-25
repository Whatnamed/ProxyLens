# ProxyLens 开发与验证路线图

---

## 路线图原则

1. **验证导向**：先确认 Mihomo 真实数据语义，再设计生产级 Collector。
2. **正确性优先**：先解决归因、连接生命周期、double counting、重启与监控缺口，再做完整 UI。
3. **逐阶段收敛技术选型**：Go / Rust、SQLite、Tauri / Web UI 等不能只凭偏好决定，应由前一阶段证据推动。
4. **阶段验收优于时间排期**：当前不设置主观日期和未经基准测试的性能数字。

---

## 阶段概览

```text
Phase 0  Mihomo 数据源验证
   ↓
Phase 1  Collector 原型
   ↓
Phase 2  持久化与正确性
   ↓
Phase 3  审计 UI
   ↓
Phase 4  待检查流量
   ↓
Later    长期增强
```

---

## Phase 0 — Mihomo Data Source Discovery [COMPLETED]

### Goal

在真实 Windows 11 + Mihomo + FLClash 环境下，弄清 External Controller 能够稳定提供什么数据、这些字段真实语义是什么，以及旧方案“大量 Unknown”究竟来自数据源缺失还是采集 / 计算方式错误。`[Core Blocking Evidence Complete, Live FLClash Restart Remains Scoped Non-Blocking Item]`

### Deliverables

1. `docs/research/mihomo-data-source.md`
   - Mihomo 版本与测试环境；
   - `/connections`、`/traffic` 等实际行为；
   - 字段覆盖率；
   - TCP / UDP / QUIC 差异；
   - DIRECT / PROXY / REJECT / 多层代理链表达；
   - 连接生命周期和重启行为；
   - 已确认风险和仍未解决问题。
2. 一组**本地保存、不提交 Git**的原始 JSON 样本，用于复核真实行为。
3. 必要时提供极少量脱敏后的示例片段放进调研报告。
4. MetaCubeXD Data Usage 等参考实现的对比结论：哪些思路可借鉴、哪些不能直接当作 Mihomo 事实。

### Required scenarios

至少覆盖：

- 空闲 TUN 下的后台连接；
- NTP / UDP；
- 浏览器 HTTPS；
- QUIC / HTTP3（在环境能够稳定触发时）；
- 常见桌面软件；
- 一个明确走代理的大流量场景；
- 一个明确 DIRECT 的大流量场景；
- 长连接；
- 节点 / 策略切换；
- Mihomo 重载或重启；
- External Controller 断开和恢复。

### Acceptance

Phase 0 完成时必须能够回答 `docs/ARCHITECTURE.md` 的“Phase 0 必须回答的问题”，并明确区分：

- **Documented**：官方文档明确说明；
- **Observed**：当前真实环境已经复现；
- **Inferred**：合理推断但尚未直接验证。

对关键字段形成覆盖率和缺失原因表，不得只写“基本可用”。

### Out of scope

- 生产级 Collector；
- 正式数据库 schema；
- 桌面 UI；
- 为了完成调研提前初始化完整应用技术栈。

---

## Phase 1 — Collector Prototype [COMPLETED - Core Collector Complete]

### Goal

构建轻量、只读、低开销的独立采集器原型，建立确定性状态机、Fail-Stop 错误处理与事件流标准契约。全栈端到端写入性能与长期 Soak 基准测试在 Phase 2 持久化阶段统一执行。

### Deliverables

1. 可连接指定 Mihomo External Controller 的最小 Collector；
2. 内存 Active Connection 状态表；
3. 基于真实语义的 upload / download 增量计算；
4. 连接出现、更新、消失的生命周期处理；
5. Controller 断线 / 重连处理；
6. 对 Mihomo 重启、Collector 自身重启、未知状态转换的显式日志；
7. 第一轮性能和资源 benchmark。

### Acceptance

- 常见 TCP / UDP / QUIC 场景下不会明显漏记或 double count；
- 大量短连接下运行稳定，无明显持续内存增长；
- Mihomo / Controller 中断后 Collector 不崩溃，并能自动恢复观察；
- 对无法安全续算的区间明确标记，而不是猜测补齐；
- 形成真实 benchmark，之后才决定合理的 RAM、CPU、重连延迟和采样 / 缓冲目标。

### Out of scope

- 完整持久化历史；
- 图形 UI；
- 复杂聚合统计。

---

## Phase 2 — Local Storage & Aggregations [IN PROGRESS]

### Goal

把已经验证可靠的连接状态转换为长期可查询的本地历史，并建立正确性对账和 Monitoring Gap 模型。

### Phase 2A — Storage Foundation & Event Journal [COMPLETED]
- [x] 选定 SQLite + WAL 驱动（纯 Go `modernc.org/sqlite`，ADR 0002）；
- [x] 实现不可变权威事件日志 `event_journal` 与单调游标 `storage_cursors`；
- [x] 实现 `connections`、`connection_traffic`、`monitoring_gaps`（支持流中断与进程级离线 Gap）、`residual_intervals` 与 `collector_health` 实时投影；
- [x] 实现 `RebuildProjections` 支持从 Journal 100% 完整重建；
- [x] 实现轻量 `QueryService` 并提供 `collector storage inspect / gaps / rebuild` CLI 命令；
- [x] 端到端实测验证通过（NTP、短请求、持续下载持久化与离线 Gap 推导 100% PASS）。

### Phase 2B1 — Accounting, Lifecycle Semantics & Hourly Aggregations [COMPLETED]
- [x] 规范连接观察生命周期字段（`observation_ended_at` / `reason` / `event_id`），支持 Disappeared、Epoch Break、Clean Stop 与 Interrupted 恢复；
- [x] 实现版本化核算架构（`accounting_runs`、`relay_relations`、`accounted_traffic`，ADR 0003）；
- [x] 实现保守中继对账算法（Conservative Relay Reconciliation v1，歧义不扣流量）；
- [x] 实现单层物化分时聚合（`usage_hourly_dimensions`，9 大核心维度，整数纳秒向下取整 + 确定性余数补偿，整数字节绝对守恒）；
- [x] 实现监控覆盖率区间并集模型（`CoverageSummary`，Interval Union，Known Scope 隔离）；
- [x] 交付面向 UI 的稳定 `AnalyticsService` 与 `collector accounting rebuild`、`collector analytics` 系列 CLI 命令。

### Phase 2B2 — Lifecycle Ops, Full-Stack Benchmarks & PRODUCT Acceptance [NEXT]
- [ ] 数据保留与清理策略 (Retention Policy & Automated Cleanup)；
- [ ] 周期性 SQLite WAL checkpoint 调度器；
- [ ] 全栈端到端写入性能基准（针对真实高负载）与长效 Soak 稳定性实测；
- [ ] 运行 PRODUCT.md A-E 场景持久化与核算最终验收。

### Acceptance

必须通过 `PRODUCT.md` 中的核心场景：

- **后台 NTP / UDP**：连接结束后仍能完整回查；
- **代理大文件**：能解释进程、目标、规则、代理链和流量，不出现无原因的大块 Unknown；
- **DIRECT 大流量**：不会错误计入 Proxy Traffic；
- **Collector / Controller 中断**：形成明确 Gap；
- **节点切换**：历史保留发生当时的代理路径；
- **应用 / Mihomo 重启**：不会把重启造成的数据缺口伪装成正常连续统计。

### Out of scope

- 完整用户界面；
- 自动规则建议。

---

## Phase 3 — Audit UI

### Goal

开发随用随开的审计 UI，让用户能够快速从历史中回答“谁、去哪、为什么、走哪里、多少”。

### Deliverables

1. 历史连接列表与搜索；
2. 时间、进程、域名 / IP、规则、协议、节点等组合筛选；
3. 单连接审计链；
4. 按应用 / 域名 / 规则 / 节点等聚合；
5. DIRECT / PROXY / REJECT 等分类视图；
6. 监控覆盖率和 Gap 时间轴；
7. UI 与 Collector 生命周期彻底解耦。

### Acceptance

- UI 关闭后 Collector 不受影响；
- 在 Phase 2 建立的目标数据集规模下查询和交互保持流畅；
- 用户可以快速识别统计区间是否存在监控缺口；
- 单条代理连接能够清楚展示可用的完整因果链；
- 基于真实数据建立启动和查询性能基准，不为满足早期文档数字而优化。

### Out of scope

- 自动修改 Mihomo；
- 云同步；
- 与系统网络路径耦合。

---

## Phase 4 — Audit Intelligence

### Goal

在可靠历史之上，用透明、可解释的规则筛选“值得检查的代理流量”，辅助用户优化分流。

### Initial checks

优先考虑：

- 首次走代理的后台进程；
- Windows / 安全软件后台服务走代理；
- `MATCH` 兜底的大流量；
- `NETWORK,udp` 等宽泛 UDP 规则；
- 只有 IP、缺少域名的大流量；
- 单连接或单进程异常增长；
- 过去长期 DIRECT、近期变成 PROXY 的目标。

### Deliverables

1. 待检查流量列表；
2. 每个检查项的触发理由；
3. 相关历史连接快速下钻；
4. 必要时生成**可复制但不自动应用**的 Mihomo 规则建议。

### Acceptance

- 能从真实历史中高亮类似“后台 NTP 被宽泛 UDP 规则送入代理”的场景；
- 所有判断均能解释“为什么被标记”；
- 规则建议必须由用户决定是否应用；
- 不引入不可解释的黑盒评分作为核心依据。

### Out of scope

- 未经用户确认直接修改配置；
- 自动切换节点；
- 防火墙或阻断功能。

---

## Later — Long-term Enhancements

核心审计链稳定后再评估：

- 机场套餐周期与用户自定义重置日；
- 节点倍率和机场计费估算；
- 更长历史的聚合 / 清理策略；
- CSV / JSON 导出；
- 多 Mihomo GUI 兼容验证；
- Linux / macOS；
- 如果 Mihomo 数据源经长期实测确实存在无法接受的盲区，再评估第二观测数据源。
