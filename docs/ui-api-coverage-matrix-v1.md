# ProxyLens — UI API Coverage Matrix v1

- **Status**: Frozen
- **Date**: 2026-08-25
- **Scope**: Mapping between UI Surfaces and Go Local Query API endpoints

---

## 1. Surface to API Mapping Matrix

| UI Surface / Component | Required Data Block | Existing Query API Endpoint | Status | Notes |
| :--- | :--- | :--- | :--- | :--- |
| **Overview: Totals** | Total/Proxy/Direct Usage | `GET /api/v1/analytics/summary` | **READY** | 支持 `from`, `to`, `route` 过滤 (Reject 包含 Upload/Download 流量；Reject 连接数无独立 API，标记为 DEFERRED) |
| **Overview: Freshness** | Accounting Freshness & Lag | `GET /api/v1/meta`, `/analytics/summary` | **READY** | 返回 `isFresh`, `lagEvents`, `latestCompletedRun` |
| **Overview: System Status** | Collector Liveness & Session | `GET /api/v1/meta` | **READY** | 返回 `latestSession`, `dbState`, `schemaVersion` |
| **Overview: Top Processes** | Process Ranking by Bytes | `GET /api/v1/analytics/top/processes` | **READY** | 返回 `process` 名称、分流、上传/下载/总流量与连接数 (*不承诺 `processPath`*) |
| **Overview: Top Rules** | Rule Ranking by Bytes | `GET /api/v1/analytics/top/rules` | **READY** | 返回 `rule`, `rulePayload`, `route`, 上传/下载/总流量与连接数 |
| **Overview: Top Exit Nodes**| Outbound Node Ranking | `GET /api/v1/analytics/top/final-proxies` | **READY** | 返回代理出站节点流量与连接数 |
| **Overview: Top Hosts** | Destination Host Ranking | `GET /api/v1/analytics/top/hosts` | **READY** | 返回域名流量排名 (*destination-IP 独立排行标记为 DEFER UNTIL DESIGN REQUIRES IT*) |
| **Overview: Protocols** | Network / Protocol Breakdown| `GET /api/v1/analytics/protocols` | **READY** | 返回 TCP / UDP 流量对比 |
| **Coverage Summary** | Coverage Ratio & Duration | `GET /api/v1/coverage` | **READY** | 返回 `coverageRatio`, `mergedGaps` |
| **Review: Detector Findings** | Deterministic PROXY review candidates | `GET /api/v1/intelligence/findings` | **READY — Phase 4A** | 四类结构化 detector；固定 PROXY、[from,to)、exact/interval evidence；无 score/severity |
| **Review: Temporal Process Changes** | Complete-hour process comparison | `GET /api/v1/intelligence/process-changes` | **READY — Phase 4B1 compatibility** | 四个显式 UTC-hour bounds；coverage/accounting window 不完整时 fail-closed；newly-observed/growth、bytes/hour、无 score/severity |
| **Review: Temporal Findings Bundle** | Process + recorded-host route transition comparison | `GET /api/v1/intelligence/temporal-findings` | **READY — Phase 4B2A** | 共享 temporal readiness；process findings + recorded host DIRECT→PROXY；mixed recent route explicit；per-kind bounded results；无 score/severity |
| **History: Connection List**| Filtered Connection Stream | `GET /api/v1/connections` | **READY** | 支持时间、分流、进程、域名、目标 IP、网络与精确 Rule 过滤；destinationPort / Final Proxy 等仍 deferred |
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

- **Phase 4A/4B1/4B2A Review 覆盖**：`/api/v1/intelligence/findings`、
  `/api/v1/intelligence/process-changes`、`/api/v1/intelligence/temporal-findings`
  与现有只读 History 下钻已完成；更广泛的 route-change、智能建议、评分
  与真实环境数据验证仍 Deferred。
  在 Phase 3B 视觉设计明确要求呈现趋势图表之前，保持现有只读 API 契约冻结，不提前发明无单测佐证的新接口。
