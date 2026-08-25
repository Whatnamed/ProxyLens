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
  "schemaVersion": 7,
  "maxBinarySchemaVersion": 7,
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
- `GET /api/v1/analytics/top/rules?from=...&to=...&route=...&limit=20`
- `GET /api/v1/analytics/top/final-proxies?from=...&to=...&route=...&limit=20`
- `GET /api/v1/analytics/protocols?from=...&to=...&route=...&limit=20`

- **Response**:
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

### 3.5 Monitoring Coverage
`GET /api/v1/coverage?from=<rfc3339>&to=<rfc3339>`
- **Response**: 返回 `CoverageSummary`（包含 coverageRatio, coveredDurationMs, uncoveredDurationMs, mergedGaps 等）。

### 3.6 Connection List
`GET /api/v1/connections?from=...&to=...&route=...&process=...&host=...&destinationIp=...&network=...&limit=50&offset=0`
- **Response**:
```json
{
  "items": [...],
  "limit": 50,
  "offset": 0,
  "hasMore": false
}
```

### 3.7 Connection Detail & Traffic
- `GET /api/v1/connections/{sessionId}/{epochId}/{connectionId}`
  - 返回连接元数据与其在最新 Accounting Run 中的归因记录。
- `GET /api/v1/connections/{sessionId}/{epochId}/{connectionId}/traffic`
  - 返回该连接的所有流量增量时序帧列表。
