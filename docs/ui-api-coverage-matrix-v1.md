# ProxyLens — UI API Coverage Matrix v1

- **Status**: Frozen
- **Date**: 2026-08-25
- **Scope**: Mapping between UI Surfaces and Go Local Query API endpoints

---

## 1. Surface to API Mapping Matrix

| UI Surface / Component | Required Data Block | Existing Query API Endpoint | Status | Notes |
| :--- | :--- | :--- | :--- | :--- |
| **Overview: Totals** | Total/Proxy/Direct Usage | `GET /api/v1/analytics/summary` | **READY** | 支持 `from`, `to`, `route` 过滤 |
| **Overview: Freshness** | Accounting Freshness & Lag | `GET /api/v1/meta`, `/analytics/summary` | **READY** | 返回 `isFresh`, `lagEvents`, `latestCompletedRun` |
| **Overview: System Status** | Collector Liveness & Session | `GET /api/v1/meta` | **READY** | 返回 `latestSession`, `dbState`, `schemaVersion` |
| **Overview: Top Processes** | Process Ranking by Bytes | `GET /api/v1/analytics/top/processes` | **READY** | 支持 `limit`, `route`, `from`, `to` |
| **Overview: Top Rules** | Rule Ranking by Bytes | `GET /api/v1/analytics/top/rules` | **READY** | 返回 `rule`, `rulePayload`, 流量与连接数 |
| **Overview: Top Exit Nodes**| Outbound Node Ranking | `GET /api/v1/analytics/top/final-proxies` | **READY** | 返回代理出站节点流量与连接数 |
| **Overview: Top Hosts** | Destination Host Ranking | `GET /api/v1/analytics/top/hosts` | **READY** | 返回域名流量排名 |
| **Overview: Protocols** | Network / Protocol Breakdown| `GET /api/v1/analytics/protocols` | **READY** | 返回 TCP / UDP 流量对比 |
| **Coverage Summary** | Coverage Ratio & Duration | `GET /api/v1/coverage` | **READY** | 返回 `coverageRatio`, `mergedGaps` |
| **History: Connection List**| Filtered Connection Stream | `GET /api/v1/connections` | **READY** | 支持多维过滤与分页 (`limit`, `offset`) |
| **Detail: Metadata** | Composite Identity Metadata | `GET /api/v1/connections/{s}/{e}/{c}` | **READY** | 三元组主键检索，返回 `connection` |
| **Detail: Accounting Events**| Full Accounting Event Flow | `GET /api/v1/connections/{s}/{e}/{c}` | **READY** | 返回 `accountingEvents[]` 时序数组 |
| **Detail: Accounting Summary**| Reconciled Aggregation | `GET /api/v1/connections/{s}/{e}/{c}` | **READY** | 返回 `accountingSummary` |
| **Detail: Raw Traffic Frame**| Raw Delta Traffic Frames | `GET /api/v1/connections/{s}/{e}/{c}/traffic`| **READY**| 返回 `traffic` 增量采样数组 |

---

## 2. API Sufficiency & Deferral Decision

- **Phase 3B 覆盖完整性**:
  当前 Go Local Query API 提供的 10 个核心端点已 **100% 覆盖 Phase 3B Overview 审计主面板** 所需的所有指标与排名数据。
- **无须提前臆测新增 API**:
  关于分时流量趋势图（如 24 小时或 30 天的按天/按小时时序折线图），在 Phase 3B 的信息架构中已完成基础聚合支持（`usage_hourly_dimensions`），其专门的前端时序端点标记为：
  ```text
  DEFER UNTIL VISUAL DESIGN REQUIRES IT
  ```
  在 Phase 3B 视觉设计明确要求呈现趋势图表之前，保持现有只读 API 契约冻结，不提前发明无单测佐证的新接口。
