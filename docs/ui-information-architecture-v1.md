# ProxyLens — UI Information Architecture Contract v1

- **Status**: Frozen
- **Date**: 2026-08-25
- **Scope**: Functional Information Architecture for Audit UI (No visual design / No component mockups)

---

## 1. Overview (审计主看板)

### 1.1 核心审计目标
解答五个核心问题：
> “在指定时间窗口内，谁（进程）使用了代理流量？去了哪里（域名/IP）？为什么这样分流（规则）？经过什么出口（最终节点）？分别消耗了多少（上传/下载）？”

### 1.2 数据块与语义结构
1. **时间窗口选择 (Time Window Range)**:
   - 支持快捷窗口（Today, Yesterday, 7d, 30d）与精确自定义区间；
   - 保持本地时区感知与 `[from, to)` 半开区间语义。
2. **流量总量与分流对比 (Traffic & Routing Totals)**:
   - Proxy 总流量 (Upload / Download / Total)；
   - Direct 总流量 (Upload / Download / Total)；
   - Reject 拦截流量 (Upload / Download / Total)；*(注: Reject 连接数当前无独立聚合 API，标记为 DEFERRED)*；
   - 流量归因完整性指示（Known Application vs Missing Attribution vs Residual）。
3. **监控覆盖与健康度 (Monitoring Coverage)**:
   - 监控覆盖率（%）；
   - 覆盖时长 vs 缺口时长；
   - 缺口归因（Controller Stream vs Collector Session Boundary）。
4. **核算权威与新鲜度 (Accounting Authority & Freshness)**:
   - 当前已发布核算权威状态（优先 active v2 generation；无 active generation 时才回退 completed legacy run）；
   - Fresh / Stale 状态、已发布边界与落后事件数 (`lagEvents`)。
5. **Top 排名维度 (Top Dimension Breakdown)**:
   - **Top Processes**: 消耗代理流量最多的进程列表（进程名 `process`、上传、下载、连接数；*注: 分时聚合不承诺 `processPath`*）；
   - **Top Final Proxies**: 流量最大的出口节点列表（节点名称 `finalProxy`、分流、上传、下载、连接数）；
   - **Top Rules**: 触发频率与流量最高的路由规则（规则名 `rule`、规则载荷 `rulePayload`、分流 `route`、上传、下载、连接数）；
   - **Top Hosts**: 目标域名流量排行（域名 `host`、分流、上传、下载、连接数；*注: destination-IP ranking 标记为 DEFER UNTIL DESIGN REQUIRES IT*）；
   - **Protocols & Networks**: TCP vs UDP 流量分布。

---

## 2. History (历史连接查询)

### 2.1 核心审计目标
提供全量历史连接的多维检索与审计溯源。

### 2.2 功能过滤维度 (Functional Filters)
- **Time Range**: `[from, to)` 时间范围；
- **Route**: `ALL` | `PROXY` | `DIRECT` | `REJECT`；
- **Process**: 进程名模糊匹配；
- **Host / Domain**: 域名/嗅探域名匹配；
- **Destination IP**: 目标 IP 匹配 *(注: destinationPort 过滤标记为 Phase 3C DEFERRED)*；
- **Network**: `tcp` | `udp`。

### 2.3 列表项元数据
- 权威复合三元组主键: `(session_id, epoch_id, connection_id)`；
- 观测时间范围: `first_observed_at` ~ `last_observed_at`；
- 进程与路径: `process`, `process_path`；
- 目标与网络: `host`, `sniff_host`, `destination_ip`, `destination_port`, `network`；
- 规则与分流: `rule`, `rule_payload`, `route`, `chains`, `final_proxy`；
- 流量统计: `monitored_upload_total`, `monitored_download_total`；
- 观测生命周期状态: `state`, `preexisting_at_start`, `observation_end_reason`。

---

## 3. Connection Detail (单连接因果详情与核算溯源)

### 3.1 权威连接身份
严格使用复合三元组唯一标识：
```text
(session_id, epoch_id, connection_id)
```

### 3.2 详细审计证据块
1. **基础元数据**: 进程名、完整可执行文件路径、入站协议/端口、目标域名、嗅探域名、目标 IP、DNS 解析模式等；
2. **分流与代理链证据**: 匹配规则、规则 Payload、代理链（`chains`）、Provider 代理链（`provider_chains`）、最终出站节点（`final_proxy`）、顶层策略组（`top_policy_group`）；
3. **核算事件时序列表 (`accountingEvents[]`)**:
   - 展示该连接在 active v2 generation 中的完整核算事件流；无 active generation 时回退最新 completed legacy run，保持与 Analytics/Review 同一权威选择规则；
   - 按 `observed_at ASC, source_event_id ASC` 严格排序；
   - 保留由于节点切换、规则更新或中继配对产生的历史演化证据；
4. **核算汇总 (`accountingSummary`)**:
   - Raw Upload/Download vs Accounted Upload/Download；
   - 归因类别（`known_application`, `direct_application`, `system_proxy_inbound`, `ambiguous_relay_candidate` 等）；
5. **原始流量增量时序 (`traffic` 事件流)**:
   - 每次采样捕获的增量上传/下载（`delta_upload`, `delta_download`）与累计计数器（`observed_upload_counter`, `observed_download_counter`）。

---

## 4. Coverage (监控缺口与完整性审计)

### 4.1 核心审计目标
区分健康的常驻监控与 Controller 断流或 Collector 重启/中断造成的监控缺口，保证流量统计的可解释性。

### 4.2 审计数据块
1. **已知监控范围 (Known Scope)**: 明确统计时间窗内的有效观测时间跨度；
2. **覆盖率 (Coverage Ratio)**: 连续有效监控时长占总窗口时长的百分比；
3. **缺口列表与来源 (Monitoring Gaps Provenance)**:
   - `controller_stream`: Mihomo Controller 断流、重连或休眠缺口；
   - `collector_session_boundary`: Collector 进程未运行、重启或系统离线缺口；
4. **缺口估算物理流量 (Estimated Physical Bytes)**:
   - 基于全局计数器前后差值估算缺口期间发生的物理流量；
   - 显式标记为 `estimated` / `interval_derived`，严禁与点对点 exact 流量混淆。

---

## 5. App / System Status (系统与服务状态)

### 5.1 功能状态指标
- **Query API 状态**: Loopback 端口、进程运行状态、API 契约版本；
- **数据库状态**: `READY` | `UNAVAILABLE` | `INCOMPATIBLE`，当前 Schema 迁移版本；
- **Collector 采集器状态**: 最新 Session ID、启动时间、心跳时间戳（`last_heartbeat_at`）、活跃度（Healthy vs Stale vs Offline）；
- **Accounting 核算状态**: 当前核算权威的 generation/run ID（按 active v2 → completed legacy fallback 选择）、已发布边界、`lagEvents` 与 `isFresh`。

---

## 6. V1 边界说明
- V1 核心 workspace 固定为 `Overview → Review → History → Coverage`。
- Settings 已作为 Phase 3E-2B2B 实现的 sidebar secondary utility dialog，支持当前安装版设置；它不新增 Settings 顶层导航，也不改变核心 workspace。
