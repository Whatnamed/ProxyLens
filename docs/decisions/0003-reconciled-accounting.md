# 0003. 版本化核算、保守中继对账与分时聚合模型 (Reconciled Accounting Architecture)

- **状态**: 已接受 (Accepted)
- **日期**: 2026-08-25
- **决策者**: ProxyLens 核心架构团队
- **相关任务**: Phase 2B1 (Big Work Package E)

---

## 1. 背景与问题陈述

在 Phase 0 与 Phase 1 的实测中，Mihomo 的 External Controller 存在以下复杂网络特征：
1. **中继/本地代理链产生重复流量 (Relay Duplicate)**：特定客户端（如部分桌面应用或本地转发代理）与 Mihomo 之间建立连接时，会同时产生一条带进程的入站连接和一条无进程的底层物理出站连接，导致同一次网络请求在原始快照中被计数两次；
2. **歧义与假阳性风险 (Ambiguity & Structural False Positives)**：在短连接爆发或并发请求时，单纯依靠字节相近或时间窗口匹配极易将两个独立的并发请求误判为中继对，导致不可逆的流量误扣；
3. **出站节点状态漂移 (Selector State Drift)**：Mihomo 当前活动节点的切换不等于历史流量发生时的节点，历史审计绝不能靠实时读取当前 Selector 状态；
4. **时间分配与跨窗口统计 (Time Bucket Allocation)**：长连接和跨小时/跨天区间的流量如果粗暴归入某一时刻，会导致分时曲线失真；而在分摊时如果使用浮点数舍入，会导致总用量丢失或凭空产生字节。

---

## 2. 决策方案

### 2.1 原始事实与派生视图解耦 (Raw Evidence Immutable)
- **原始事实层不可变 (`event_journal`, `connection_traffic`)**：Collector 收集到的所有原始快照增量和事件永久不可变，禁止任何核算算法直接 `UPDATE` 或 `DELETE` 原始流量表；
- **双事实权威源 (Dual Authority Model)**：
  - 网络观测权威 = `event_journal`
  - 采集生命周期权威 = `collector_sessions`
- **版本化核算层 (`accounting_runs`, `relay_relations`, `accounted_traffic`)**：所有审计去重、归因清洗和中继判定均作为带版本的派生运行（`algorithm_version = reconciled-accounting-v1`），支持 100% 确定性全量重算。

### 2.2 保守中继对账 (Conservative Relay Reconciliation v1)
- 仅在同 `session_id + epoch_id` 内寻找候选对；
- 必须同时满足：
  1. 结构关联一致（出站物理节点或目标 IP 匹配）；
  2. 生命周期实质时间重叠；
  3. 上传/下载总字节数在严格保守误差范围内；
  4. **严格 1-to-1 匹配**：候选连接只匹配一个逻辑连接，且该逻辑连接也只匹配该候选连接；
- **硬性安全防线 (Hard Safety Rule)**：
  - 遇到 1-to-N、N-to-1 或多重歧义时，标记为 `ambiguous_relay`，**一律不扣流量 (`accounted = raw`)**；
  - 遇到无匹配的孤立中继候选，标记为 `missing_attribution`，**不扣流量**；
  - 仅在唯一且确凿的 1-to-1 判定为 `confirmed_relay_duplicate` 时，将中继副连接置 `accounted = 0`，逻辑主连接保留真实用量；
- 所有判定依据与误差指标完整记录于 `relay_relations.evidence_json`。

### 2.3 单层物化分时聚合 (`usage_hourly_dimensions`)
- 系统只维护一层 **Hourly（小时级）** 物化聚合表，不建立多套冗余的 Minute/Day/Month 表；
- 预聚合并索引核心维度：`total`, `process`, `host`, `destination_ip`, `rule`, `rule_payload`, `final_proxy`, `top_policy_group`, `network`；
- **时间分配与绝对字节守恒 (Byte Conservation)**：
  - `exact_snapshot` 流量归入 `observed_at` 对应的小时桶（`exact_upload/download`）；
  - 跨整小时区间的 `interval_only` 流量按纳秒级重叠时长比例分摊（`estimated_upload/download`）；
  - 分摊时采用向下取整（Floor Allocation），剩余字节（Remainder）确定性补偿到首个时间桶，确保所有桶之和严格等于原始输入字节数，杜绝浮点精度漂移。

### 2.4 监控覆盖率与缺口并集 (Monitoring Coverage Model)
- 从首个 Collector 会话的 `started_at` 确立 `known_scope_start`；
- 查询窗口早于 `known_scope_start` 的部分明确标记为 `outside_known_monitoring_scope`，返回 `coverageRatio = nil`，绝不伪装为监控故障；
- 查询窗口内的重叠缺口（`controller_stream` 与 `collector_session_boundary`）通过区间求并集（Interval Union）计算真实的 `uncovered_duration_ms`，消除重复计算。

---

## 3. 产生的影响与收益

- **数据完整性**：即使用户升级了核算算法或修复了归因逻辑，只需执行 `collector accounting rebuild` 即可重新生成最新核算数据，原始审计证据完好无损；
- **统计可信度**：歧义流量永不误扣，真实 PROXY、DIRECT、REJECT 严格隔离，Sampling Residual 与 Gap Physical Delta 独立呈现；
- **查询高性能**：通过 `usage_hourly_dimensions` 聚合表，未来 UI 可以毫秒级响应按天、周、月的 Top 进程、Top 域名、Top 节点排行榜。
