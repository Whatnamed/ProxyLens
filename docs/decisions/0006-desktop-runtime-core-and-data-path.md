# ADR 0006: Desktop Runtime Core, Scheduled Accounting, and Canonical Local Data Path

- **Status**: Confirmed / Implemented
- **Date**: 2026-09-04
- **Deciders**: ProxyLens Core Team
- **Scope**: Phase 3E-1 Desktop Runtime Core

This ADR records the Phase 3E-1 core boundary. The subsequent Windows
ownership and Tauri ensure-start extension is recorded in ADR 0007.

---

## 1. Context

Phase 2 已经分别完成了只读 Collector、SQLite/WAL 原始事实、版本化 Accounting、Freshness 与 Query API。此前 live Collector 生命周期仍主要由 `collector run` CLI 持有，Accounting 需要手工 rebuild，Tauri 也只能通过显式 `PROXYLENS_DB_PATH` 找到数据库。

这使产品缺少一个可持续运行、可测试且不依赖 UI 生命周期的 Go Runtime Core。本阶段需要补齐采集与自动核算的组合，同时为 Windows 桌面交付确定长期本地数据路径；Windows 后台托管和安装生命周期仍另行处理。

## 2. Decisions

### 2.1 独立 Runtime Core

新增 `proxylens-runtime` executable，并在 `collector/pkg/runtime` 提供可复用的 `CollectorRunner`、`AccountingScheduler` 与 `Runtime` 组合：

```text
Mihomo External Controller (read-only)
                 ↓
       proxylens-runtime (foreground core)
         ├─ CollectorRunner
         └─ AccountingScheduler
                 ↓
          SQLite + WAL authority DB
                 ↑ read-only
        proxylens-query-api → React → Tauri
```

`collector run` 保留为开发/验证 CLI wrapper，负责 flags、OS signal、stdin STOP、summary 与 exit code；业务 loop、队列、worker、session close 不再复制在 CLI 中。

`proxylens-runtime` 当前是可独立前台运行的 executable，不 daemonize，不注册 Windows Service，不自启动，不实现托盘或 detached-process ownership。

### 2.2 Collector 生命周期边界

`CollectorRunner` 只接受 caller-owned `context.Context`，不创建全局 signal handler、不读取 stdin、不调用 `os.Exit`。它保证 queue worker join、SQLite session end/close 顺序和 fatal error 返回。

Reusable library boundary additionally requires an explicit, non-empty `ControllerURL`。`NewCollectorRunner` 对空或全空白的 `ControllerURL` 直接返回错误，不得静默从 `config.DefaultConfig()` 继承真实 Controller 默认值。正式 CLI 可以自行解析其兼容默认值，但必须将解析后的 URL 显式传入；测试和其他 library caller 必须提供自己的受控 URL，Runtime 集成测试使用 mock controller。

Controller 侧只使用 `GET /version` 与 `GET/WS /connections`。Validation fault injection 仍是显式 options，不进入 Runtime 的默认路径；Secret 只来自内存/options 或 `MIHOMO_SECRET`，不写入日志。

### 2.3 Scheduled Accounting

`AccountingScheduler` 默认间隔为 `30s`，可由 `--accounting-interval` 调整。启动时立即执行一次 freshness check，随后按周期串行执行：

- 无 journal event：`skipped_no_events`，不制造空 run；
- `LagEvents == 0`：`skipped_fresh`，不重复 rebuild；
- `LagEvents > 0`：执行一次边界受控的 `RebuildAccounting`；
- 同一个 scheduler 永远不并发执行两个 rebuild；
- freshness 查询或单次 rebuild 失败只记录诊断结果，Collector 继续采集并在下一周期重试；
- cancellation 停止 tick，并把同一可取消 context 传入正在进行的 rebuild。

Accounting 继续复用 ADR 0004 的 snapshot boundary、短事务和 raw authority 保护，不在 Runtime 层重写核算语义。

### 2.4 Runtime 与 UI lifecycle 解耦

Runtime 与 Query API 使用同一正式 DB path，但各自拥有职责边界：Runtime 写入原始事实与派生核算，Query API 以 `mode=ro` + `query_only=ON` 只读查询。Phase 3E-1 的 Tauri 只 spawn/stop `proxylens-query-api`，绝不自动 spawn 或 stop `proxylens-runtime`；Phase 3E-2A 的当前扩展见 ADR 0007。

Runtime 收到 caller cancellation 时按 scheduler-first 顺序收尾，再停止 Collector；Collector fatal 会让 Runtime 返回非零错误，而 Accounting 失败不会杀死 Collector。

### 2.5 Canonical Windows local data path

Windows V1 authority DB 的默认路径确定为：

```text
%LOCALAPPDATA%\ProxyLens\data\proxylens.db
```

环境解析契约为：

1. `PROXYLENS_DB_PATH`：精确 DB 文件 override；
2. `PROXYLENS_DATA_DIR`：DB 为 `<dir>\proxylens.db`；
3. `LOCALAPPDATA` 默认路径：`%LOCALAPPDATA%\ProxyLens\data\proxylens.db`。

Runtime writer 可以创建缺失目录和 DB。Tauri/Query API resolver 是只读路径，显式 override 缺失时报错，data-dir/default DB 缺失报告 `DB_NOT_READY`，不创建空 DB、不猜测 repo fixture。Runtime CLI 的 `--db` 是本次进程调用的直接 explicit path；未提供时使用上述环境契约。

Go 与 Rust 各自保留小型 resolver，并由 deterministic tests 锁定相同的 path segments 与 precedence。

## 3. Consequences

- Collector 可在不打开 UI 的情况下持续运行，Accounting 可自动追平而不阻塞 ingestion；
- 稳定运行时不会每个周期生成内容相同的 completed accounting run；
- UI close 仍只影响 Query API，不能据此推断 Runtime 已停止；
- 安装目录、Roaming Profile、TEMP 和源码目录都不再是正式长期 authority DB 位置；
- Secure Controller Secret provisioning、single-instance、ensure-start、crash restart、login/autostart、Windows Service、tray、installer ownership 与 detached background lifecycle 留给 Phase 3E-2；
- 本阶段的正式验收路径不使用真实 FLClash/Mihomo lifecycle 或 real-data visual acceptance。

## 4. Verification

- `collector/pkg/runtime` unit tests cover cancellation, skip-if-fresh, no-events, retry, non-overlap and path precedence;
- `NewCollectorRunner` unit tests reject empty/blank `ControllerURL`; the safety guard recursively scans the collector test tree and requires every subprocess `run` call to pass an explicit `--controller`;
- `collector/test/runtime_core_test.go` uses only an `httptest.Server`, mock WebSocket and `t.TempDir()` SQLite DB to verify collection, scheduled accounting, freshness, read-only query and clean reopen;
- subprocess crash regression also uses a mock controller rather than the conventional real Controller port;
- UI tests cover the Tauri resolver path contract, and the binary build script produces both `proxylens-query-api-<target>` and `proxylens-runtime-<target>`;
- Tauri does not spawn the Runtime binary in this phase.
