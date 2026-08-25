# ADR 0005: UI Platform Architecture, Query Boundary, and Sidecar Lifecycle

- **Status**: Confirmed
- **Date**: 2026-08-25
- **Deciders**: ProxyLens Core Team
- **Consulted**: Phase 3A UI Platform Team

---

## 1. Context & Problem Statement

在 Phase 2 完成了不可变事件流水存储（`event_journal`）、双事实权威源、版本化核算（`accounting_runs`, `accounted_traffic`, `relay_relations`）与分时聚合（`usage_hourly_dimensions`）后，ProxyLens 需要进入 Phase 3 实现本地桌面审计可视化。

在设计 UI 架构与数据访问边界时，存在以下核心原则与权衡：
1. **防止双重实现与语义漂移**: 核算、流量归因、中继对账、缺口分析与时区半开区间聚合在 Go 存储层中已有严密的单测和保证。严禁在 UI 端（React 或 Rust）重新用 SQL 实现这些复杂业务逻辑。
2. **前后端解耦与 Schema 演进**: 前端不应直接与 SQLite 表结构或迁移版本产生紧耦合，避免未来 Storage 重构导致前端报废。
3. **独立后台采集**: UI 仅作为按需开启的可视化观察端，关闭 UI 绝不能影响后台 Collector 的持续常驻采集；Collector 也不能被绑死在 UI 进程树中。
4. **本地安全性**: UI 与本地查询服务之间的通信必须防止本地其他恶意程序或浏览器跨站窃取敏感审计数据。
5. **系统资源与交付体积极简**: 避免捆绑笨重的 Chromium 运行时，优先利用系统级现代 WebView。

---

## 2. Decision Drivers

- **权威统一**: Go `AnalyticsService` 与 `QueryService` 是唯一的查询与核算语义事实源；
- **只读隔离**: UI 查询路径必须是 100% 只读的，严禁触发数据库写入、自动重建或 schema 变更；
- **平台体验**: Windows 原生桌面体验（窗口管理、随开随关、系统托盘潜力）；
- **安全第一**: 绑定本地 Loopback、临时端口、内存级单次会话高熵 Bearer Token 与严格 CORS；
- **解耦独立**: Collector 是常驻守护进程，Query API 是随 UI 启动的短期 Sidecar。

---

## 3. Key Technical Decisions

### 3.1 总体架构与数据流 (Locked Architecture)
```text
Mihomo
   ↓
Go Collector  ──────────────→ SQLite + WAL (Authority DB)
                                 ↑
                                 │ read-only (query_only=ON)
                          Go Local Query API (Sidecar)
                                 ↑
                         127.0.0.1 : ephemeral port (HTTP JSON)
                                 ↑ (Authorization: Bearer <token>)
                       React + TypeScript + Vite
                                 ↑
                         Tauri v2 (Desktop Shell)
```

### 3.2 职责边界分工
- **Tauri / Rust**:
  - 管理桌面原生窗口与生命周期；
  - 启动并在 UI 退出时销毁 Go Query API Sidecar 子进程；
  - 生成单次启动专用的高熵 Session Token（>=256-bit），通过安全内存桥接传递给前端；
  - **严禁** 在 Rust/Tauri 中编写任何 Analytics SQL、中继计算、归因或存储逻辑。
- **Go Local Query API (`proxylens-query-api`)**:
  - 以只读模式（`query_only=ON`, `busy_timeout=10000`）打开 ProxyLens SQLite 数据库；
  - 监听 `127.0.0.1` 上的随机临时端口；
  - 校验 Bearer Token 与精确 CORS Origin，为前端提供标准 RESTful JSON `/api/v1/*` 接口；
  - 完全复用 `storage.AnalyticsService` 与 `storage.QueryService`。
- **React / TypeScript**:
  - 纯 Web 前端，使用 TanStack React Query 管理数据请求与缓存；
  - 仅通过 HTTP JSON 访问 Go Local Query API，**严禁** 直接访问 SQLite 数据库或调用任意 shell 命令。

### 3.3 进程生命周期契约
```text
UI 启动 → Tauri 生成 Token → 启动 Go Query API Sidecar → 前端就绪 → 查询展示
UI 关闭 → Tauri 终止 Go Query API Sidecar → 释放端口与资源
Collector 守护进程保持独立常驻，不受 UI 启停任何影响
```

### 3.4 安全与网络边界规范
1. **Loopback Only**: Query API 仅监听 `127.0.0.1`，拒绝任何外网或 `0.0.0.0` 绑定；
2. **Ephemeral Port**: 每次随机选择可用高位端口，标准输出第一行吐出就绪 JSON；
3. **Session Token**: 每次启动动态生成，仅存在于 Rust 与前端内存中，不持久化、不打印日志、不在 URL 中传递；
4. **CORS 保护**: 仅允许 `tauri://localhost`、`http://tauri.localhost` 及本地 Vite 开发源，严禁通配符 `*`；
5. **只读保护**: 数据库连接显式开启 `query_only=ON`，拒绝任何写操作。

---

## 4. Consequences & Verification

- **正面收益**:
  - 保留了 Phase 2 所有经过严格验证的单测、核算边界与守恒定律；
  - 前端 UI 技术选型演进完全不影响存储核心；
  - 桌面应用安装包轻量且启动迅速（依托 Windows WebView2）。
- **验证手段**:
  - Go API 单元测试（`httptest`）覆盖鉴权、CORS、只读断言与结构化错误；
  - Tauri Sidecar 启动握手与关机冒烟测试；
  - 保持全量 Phase 0 与 Phase 2 测试 100% 通过。
