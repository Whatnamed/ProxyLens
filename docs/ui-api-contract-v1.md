# ProxyLens Local Query API v1 Contract

- **Version**: `v1`
- **Protocol**: HTTP/1.1 (JSON)
- **Binding**: `127.0.0.1:<ephemeral-port>` (Loopback Only)
- **Lifecycle**: Owned by Tauri Desktop Shell (Sidecar)

---

## 1. Authentication & Security Policy

### 1.1 Bearer Token
除 `/healthz` 外，所有 `/api/v1/*` 端点必须携带单次会话临时高熵 Token：
```http
Authorization: Bearer <ephemeral-token>
```
- Token 每次 Tauri 应用启动时由 Rust 端动态生成（>=256-bit 随机熵）；
- Token 仅在内存中通过安全 Tauri Command 传递给前端，严禁持久化到磁盘、写入日志或通过 URL 参数传输；
- 鉴权校验采用常量时间比较（Constant-Time Compare），未授权返回 HTTP 401。

### 1.2 CORS & Origin 保护
API 严格拒绝通配符 `*`，仅允许以下精确 Origin：
- `tauri://localhost`
- `http://tauri.localhost`
- `https://tauri.localhost`
- `http://localhost:5173` / `http://127.0.0.1:5173` (Vite Dev)
- `http://localhost:1420` / `http://127.0.0.1:1420` (Tauri Dev)

非白名单 Origin 请求直接返回 HTTP 403 `FORBIDDEN_ORIGIN`。

### 1.3 数据库只读约束
API 服务以严格只读模式（`query_only=ON`, `busy_timeout=10000`）连接 SQLite。禁止在 API 服务中执行任何写操作、DDL、数据库迁移或核算重建。

---

## 2. Standard Error Envelope

所有错误均遵循统一结构化 JSON 格式：
```json
{
  "error": {
    "code": "NO_COMPLETED_ACCOUNTING_RUN",
    "message": "No completed accounting run found in database"
  }
}
```

常用错误码：
- `UNAUTHORIZED`: 缺少或无效的 Bearer Token (HTTP 401)
- `FORBIDDEN_ORIGIN`: 未允许的跨域 Origin (HTTP 403)
- `INVALID_TIMESTAMP`: 时间戳格式错误，非 RFC3339 (HTTP 400)
- `INVALID_ROUTE`: 路由类型无效，必须为 PROXY, DIRECT, REJECT, ALL (HTTP 400)
- `CONNECTION_NOT_FOUND`: 连接不存在 (HTTP 404)
- `NO_COMPLETED_ACCOUNTING_RUN`: 尚未存在有效的核算轮次 (HTTP 404)
- `DB_UNAVAILABLE`: 数据库文件不存在或无法打开 (HTTP 503)
- `QUERY_FAILED`: 查询执行异常 (HTTP 500)

---

## 3. Endpoints

### 3.1 Health Check
`GET /healthz` (免鉴权)
- **Response**: `{"status": "ok"}`

### 3.2 System & Session Meta
`GET /api/v1/meta`
- **Response**:
```json
{
  "apiVersion": "v1",
  "appVersion": "0.7.0-phase3a",
  "dbState": "READY",
  "schemaVersion": 9,
  "maxBinarySchemaVersion": 9,
  "latestCollectorSession": {
    "sessionId": "sess-xxx",
    "status": "running",
    "startedAt": "2026-08-25T10:00:00Z"
  },
  "latestAccountingRun": {
    "runId": "run-xxx",
    "algorithmVersion": "v1-conservative-relay",
    "status": "completed"
  },
  "freshness": {
    "sourceJournalSequenceMax": 1500,
    "currentJournalSequenceMax": 1500,
    "lagEvents": 0,
    "isFresh": true
  }
}
```

### 3.3 Analytics Summary
`GET /api/v1/analytics/summary?from=<rfc3339>&to=<rfc3339>&route=<PROXY|DIRECT|REJECT|ALL>`
- **Response**: 返回 `UsageSummary` 结构体（包含 rawObserved, uniqueObserved, proxy, direct, reject, missingAttribution, ambiguousRelay, samplingResidual, coverage, freshness 等维度）。

### 3.4 Top Dimension Breakdown
- `GET /api/v1/analytics/top/processes?from=...&to=...&route=...&limit=20`
- `GET /api/v1/analytics/top/hosts?from=...&to=...&route=...&limit=20`
- `GET /api/v1/analytics/top/final-proxies?from=...&to=...&route=...&limit=20`
- `GET /api/v1/analytics/protocols?from=...&to=...&route=...&limit=20`

- **通用维度 Response**:
```json
{
  "items": [
    {
      "key": "chrome.exe",
      "route": "PROXY",
      "uploadBytes": 1048576,
      "downloadBytes": 10485760,
      "totalBytes": 11534336,
      "connectionCount": 42,
      "exactUploadBytes": 1048576,
      "exactDownloadBytes": 10485760,
      "estimatedUploadBytes": 0,
      "estimatedDownloadBytes": 0
    }
  ],
  "limit": 20
}
```

- `GET /api/v1/analytics/top/rules?from=...&to=...&route=...&limit=20`
  - **专用规则维度 Response**:
```json
{
  "items": [
    {
      "rule": "DomainSuffix",
      "rulePayload": "google.com",
      "route": "PROXY",
      "uploadBytes": 1048576,
      "downloadBytes": 10485760,
      "totalBytes": 11534336,
      "connectionCount": 42,
      "exactUploadBytes": 1048576,
      "exactDownloadBytes": 10485760,
      "estimatedUploadBytes": 0,
      "estimatedDownloadBytes": 0
    }
  ],
  "limit": 20
}
```

### 3.5 Monitoring Coverage
`GET /api/v1/coverage?from=<rfc3339>&to=<rfc3339>`
- **Response**: 返回 `CoverageSummary`（包含 coverageRatio, coveredDurationMs, uncoveredDurationMs, mergedGaps 等）。

### 3.6 Connection List
`GET /api/v1/connections?from=...&to=...&route=...&process=...&host=...&destinationIp=...&network=...&rule=...&limit=50&offset=0`
- **Response**:
```json
{
  "items": [...],
  "limit": 50,
  "offset": 0,
  "hasMore": false
}
```

### 3.7 Audit Intelligence Review Findings (Phase 4A / Phase 4C1)

`GET /api/v1/intelligence/findings?from=<rfc3339>&to=<rfc3339>&limitPerKind=20`

- `from` and `to` are required RFC3339 timestamps and use `[from,to)` semantics;
- the endpoint is fixed to `route=PROXY` and reads the reconciled accounting authority;
- `limitPerKind` defaults to 20 and is bounded to 1–50;
- response items are deterministic, structured detector facts; no score or severity is returned;
- interval-derived portions are separated from exact accounted bytes;
- `cataloged_background_process_proxy` is an additive kind. Matching items may
  include `subject.processPath` and a provenance `knowledge` object; the
  top-level `knowledgeCatalogVersion` identifies the embedded catalog;
- the endpoint is GET-only, Bearer/CORS protected, and uses the standard `NO_COMPLETED_ACCOUNTING_RUN` / `QUERY_FAILED` error envelope.

```json
{
  "from": "2026-09-06T00:00:00Z",
  "to": "2026-09-07T00:00:00Z",
  "route": "PROXY",
  "accountingVersion": "v2-incremental",
  "knowledgeCatalogVersion": "background-processes-v1",
  "countsByKind": {
    "match_fallback_proxy": 1,
    "broad_udp_proxy": 1,
    "ip_only_proxy_target": 1,
    "large_proxy_connection": 1,
    "cataloged_background_process_proxy": 1
  },
  "items": [],
  "limitPerKind": 20
}
```

For a catalog finding, `subject.processPath` is the latest matching observed
path used for display, not part of finding identity. `knowledge` contains
`catalogVersion`, `entryId`, `category`, `publisher`, `family`,
`matchBasis=process_name_and_path`, and source publisher/title/URL metadata.
The object is provenance context, not a trust, signature, safety, severity, or
routing decision. The endpoint does not fetch source URLs.

### 3.8 Temporal Process Changes (Phase 4B1)

`GET /api/v1/intelligence/process-changes?baselineFrom=<rfc3339>&baselineTo=<rfc3339>&recentFrom=<rfc3339>&recentTo=<rfc3339>&limitPerKind=20`

- all four boundaries are required RFC3339 timestamps, normalized to UTC,
  aligned to exact UTC-hour boundaries, at least one hour long, and
  chronologically ordered so `baselineTo <= recentFrom` (adjacent windows and
  same-shaped windows separated by a gap are allowed);
- `limitPerKind` defaults to 20 and is bounded to 1–50;
- the endpoint reads the active v2 or latest completed legacy hourly process
  dimensions through the normal read-only API boundary; it does not write
  SQLite, contact a Controller, or start a Runtime;
- both windows must have zero `futureDurationMs`, zero
  `outsideKnownScopeMs`, zero `uncoveredDurationMs`, and
  `coverageRatio === 1`;
- incomplete monitoring coverage is returned as HTTP 200 with an explicit
  `status` and an empty `items` array, not as a successful zero-change result;
- accounting readiness is checked independently for each effective window:
  the active v2 `published_journal_sequence` or legacy completed run's
  `source_journal_sequence_max` must cover every `event_journal` row whose
  `journal_sequence` is later than that boundary and whose `observed_at` is
  inside the window; otherwise the endpoint returns
  `baseline_accounting_incomplete` or `recent_accounting_incomplete`;
- a legacy authority without a reliable source journal boundary returns
  `accounting_boundary_unavailable` rather than being treated as complete;
- invalid/missing boundaries or limits return HTTP 400 using the standard
  error envelope; missing accounting authority returns
  `NO_COMPLETED_ACCOUNTING_RUN`.

The supported statuses are `ready`, `baseline_outside_known_scope`,
`baseline_has_monitoring_gaps`, `baseline_future`,
`baseline_accounting_incomplete`, `recent_outside_known_scope`,
`recent_has_monitoring_gaps`, `recent_future`,
`recent_accounting_incomplete`, and `accounting_boundary_unavailable`. A
ready response contains only the two deterministic
detectors `process_newly_observed_on_proxy` and `process_proxy_growth`.
Process findings include PROXY/DIRECT/REJECT route evidence and exact versus
estimated byte fields; growth findings additionally include
`baselineProxyBytesPerHour`, `recentProxyBytesPerHour`, `deltaBytesPerHour`,
and `growthRatio`, where
`growthRatio = (recentProxyBytesPerHour - baselineProxyBytesPerHour) /
baselineProxyBytesPerHour`; `0.5` means a `+50%` increase. No anomaly score,
severity, risk, connection count, or inferred intent is part of this contract.

```json
{
  "status": "ready",
  "accountingVersion": "v2-incremental",
  "baseline": {
    "from": "2026-09-05T00:00:00Z",
    "to": "2026-09-06T00:00:00Z",
    "durationMs": 86400000,
    "coveredDurationMs": 86400000,
    "uncoveredDurationMs": 0,
    "outsideKnownScopeMs": 0,
    "futureDurationMs": 0,
    "coverageRatio": 1
  },
  "recent": {
    "from": "2026-09-06T00:00:00Z",
    "to": "2026-09-07T00:00:00Z",
    "durationMs": 86400000,
    "coveredDurationMs": 86400000,
    "uncoveredDurationMs": 0,
    "outsideKnownScopeMs": 0,
    "futureDurationMs": 0,
    "coverageRatio": 1
  },
  "items": [],
  "countsByKind": {
    "process_newly_observed_on_proxy": 0,
    "process_proxy_growth": 0
  },
  "limitPerKind": 20
}
```

### 3.9 Temporal Findings Bundle (Phase 4B2A)

`GET /api/v1/intelligence/temporal-findings?baselineFrom=<rfc3339>&baselineTo=<rfc3339>&recentFrom=<rfc3339>&recentTo=<rfc3339>&limitPerKind=20`

- uses the same four explicit UTC-hour, at-least-one-hour, non-overlapping
  bounds as the Phase 4B1 process endpoint; `baselineTo <= recentFrom` is
  required;
- uses one active v2 or completed legacy accounting authority and the shared
  monitoring-plus-window-publication readiness contract;
- returns HTTP 200 with an explicit unavailable status and empty
  `processItems`/`hostItems` when either effective window is not complete;
- `processItems` preserves the Phase 4B1 process detector semantics;
- `hostItems` contains only exact recorded hosts with baseline DIRECT bytes
  greater than zero, baseline PROXY bytes equal to zero, and recent PROXY bytes
  greater than zero. Recent DIRECT/REJECT evidence is retained, and mixed
  recent routing is explicitly described by the UI;
- `limitPerKind` is applied independently to each detector kind, while
  `countsByKind` reports the unbounded match count;
- the endpoint is GET-only, Bearer/CORS protected, read-only, and never starts
  a Runtime or contacts a Controller.

```json
{
  "status": "ready",
  "accountingVersion": "v2-incremental",
  "baseline": { "from": "2026-09-05T00:00:00Z", "to": "2026-09-06T00:00:00Z" },
  "recent": { "from": "2026-09-06T00:00:00Z", "to": "2026-09-07T00:00:00Z" },
  "processItems": [],
  "hostItems": [
    {
      "id": "host_gained_proxy_after_direct_baseline:example.com",
      "kind": "host_gained_proxy_after_direct_baseline",
      "host": "example.com",
      "baseline": {
        "proxy": { "uploadBytes": 0, "downloadBytes": 0, "totalBytes": 0, "exactUploadBytes": 0, "exactDownloadBytes": 0, "estimatedUploadBytes": 0, "estimatedDownloadBytes": 0 },
        "direct": { "uploadBytes": 0, "downloadBytes": 1000, "totalBytes": 1000, "exactUploadBytes": 0, "exactDownloadBytes": 1000, "estimatedUploadBytes": 0, "estimatedDownloadBytes": 0 },
        "reject": { "uploadBytes": 0, "downloadBytes": 0, "totalBytes": 0, "exactUploadBytes": 0, "exactDownloadBytes": 0, "estimatedUploadBytes": 0, "estimatedDownloadBytes": 0 }
      },
      "recent": {
        "proxy": { "uploadBytes": 0, "downloadBytes": 2000, "totalBytes": 2000, "exactUploadBytes": 0, "exactDownloadBytes": 2000, "estimatedUploadBytes": 0, "estimatedDownloadBytes": 0 },
        "direct": { "uploadBytes": 0, "downloadBytes": 300, "totalBytes": 300, "exactUploadBytes": 0, "exactDownloadBytes": 300, "estimatedUploadBytes": 0, "estimatedDownloadBytes": 0 },
        "reject": { "uploadBytes": 0, "downloadBytes": 0, "totalBytes": 0, "exactUploadBytes": 0, "exactDownloadBytes": 0, "estimatedUploadBytes": 0, "estimatedDownloadBytes": 0 }
      }
    }
  ],
  "countsByKind": {
    "process_newly_observed_on_proxy": 0,
    "process_proxy_growth": 0,
    "host_gained_proxy_after_direct_baseline": 1
  },
  "limitPerKind": 20
}
```

### 3.10 Connection Detail & Traffic
- `GET /api/v1/connections/{sessionId}/{epochId}/{connectionId}`
  - **三元组唯一身份检索**: 严格使用 `(session_id, epoch_id, connection_id)` 定位物理连接；
  - **核算权威**: 与 Analytics/Review 共用解析器；优先读取 active v2 generation 的 `accounted_traffic_v2`，只有不存在 active generation 时才回退 latest completed legacy run。不得因为某连接在 v2 中无行而回退 legacy。
  - **完整事件时序列表**: 返回所选权威下该连接的所有 `accountingEvents[]`，按 `observed_at ASC, source_event_id ASC` 排序，并从同一事件列表计算 `accountingSummary`。v2 不产生零字节重复派生行；原始证据仍由 `/traffic` 提供。
  - **兼容字段**: `runId` 在 v2 中表示 `generation_id`，legacy 中表示 `run_id`。尚未发布核算结果或该连接无核算事件时，详情仍返回 HTTP 200，事件为空、summary 不存在；查询错误仍返回 HTTP 500，不伪装为空。
  - **Response 结构**:
  ```json
  {
    "connection": { ... },
    "accountingEvents": [
      {
        "runId": "run-...",
        "sourceEventId": "ev-...",
        "observedAt": "2026-08-25T...",
        "precision": "exact",
        "route": "PROXY",
        "rawUpload": 1000,
        "rawDownload": 5000,
        "accountedUpload": 1000,
        "accountedDownload": 5000,
        "accountingClass": "known_application",
        "process": "chrome.exe",
        "host": "google.com",
        "rule": "DomainSuffix",
        "finalProxy": "Node-HK-01"
      }
    ],
    "accountingSummary": {
      "runId": "run-...",
      "accountingClass": "known_application",
      "route": "PROXY",
      "rawUploadTotal": 1000,
      "rawDownloadTotal": 5000,
      "accountedUploadTotal": 1000,
      "accountedDownloadTotal": 5000,
      "latestProcess": "chrome.exe",
      "latestHost": "google.com",
      "latestFinalProxy": "Node-HK-01"
    }
  }
  ```
- `GET /api/v1/connections/{sessionId}/{epochId}/{connectionId}/traffic`
  - 返回该连接的所有原始流量增量时序帧列表。

