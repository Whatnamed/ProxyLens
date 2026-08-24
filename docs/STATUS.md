# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 1 — Collector Prototype (Package C.1.2 Final Gate: PASS)`
- **代码状态**：Phase 1 生产原型终局闭环全部达成（Go 1.24+）。完成工业级 `gorilla/websocket` 客户端集成（支持 RFC 6455 握手校验、ReadDeadline Watchdog、TLS/WSS 与 Context 优雅停机）；实现阻塞 Backpressure 队列、单 Worker 串行 Ingestion 与 Fail-Stop 停机；实现 50 次事件级确定性 Golden Replay 比特级一致；完成全字段元数据演化与路由突变检测；Stage F5 机械 Live Shadow 3.0（100% 零 Fallback PASS）与 Stage F6 600s 生产 Soak 测试全部通过。
- **环境资产清单 (Environment Inventory)**：
  - OS: Windows 11 (AMD64) / 12th Gen Intel Core i5-12400 (12 cores)
  - 客户端: FLClash (PID 13436) + FlClashCore (PID 20320) 运行中
  - TUN 状态: 启用 (`device: FlClash`, `find-process-mode: always`, `enhanced-mode: fake-ip`, `mode: rule`)
  - Live 运行时内核: `Mihomo Meta v1.10.0` (GET `/version` 返回)
- **基准测试与 10 分钟生产 Soak 实测 (Benchmark 3.0 & Soak Evidence)**：
  - 1000ms: CPU 单核等效 0.026% (整机容量 0.0022%), RSS Peak 9.01 MB
  - 500ms: CPU 单核等效 < 0.03%, RSS Peak 9.03 MB
  - 250ms: CPU 单核等效 < 0.03%, RSS Peak 8.96 MB
  - 600s Soak (250ms 采样): RSS 起始 8.82 MB，峰值 10.79 MB，斜率 0.20 MB/min，活跃状态无泄漏；
  - 机器可读凭证：`docs/benchmarks/phase1-collector-benchmark.json`。

---

## Confirmed Decisions

1. **产品定位**：ProxyLens 是代理流量审计与分流优化辅助工具，核心是解释“谁、去哪、为什么、走哪里、多少”，不是单纯总流量计费器。
2. **审计优先**：默认关注真实代理流量，同时正确区分 DIRECT、PROXY、REJECT 等结果。
3. **Unknown 必须可解释**：缺进程、缺域名、仅 IP、缺规则、缺代理链等必须分别表达。
4. **Monitoring Gap 独立建模**：Collector / Controller 中断不能伪装成 Unknown Traffic。
5. **Collector 与 UI 解耦**：允许轻量 Collector 后台运行；UI 随用随开。
6. **旁路只读**：ProxyLens 不修改 Mihomo 配置、规则、节点、TUN、系统代理或路由，不阻断或限速。
7. **本地优先**：不上传网络历史，不保存 HTTP 正文、Cookie、Token、密码、TLS 明文或其他 Payload。
8. **Mihomo First**：第一阶段先使用 Mihomo External Controller；只有实测证明存在不可接受盲区时，才评估第二观测数据源。
9. **正确性优先于 UI**：先解决字段语义、connection diff、double counting、重启与缺口，再推进完整 UI。
10. **Collector 语言选型 (ADR 0001)**：选定 **Go (v1.24+)** 作为生产 Collector 开发语言（兼顾 4,000+ 帧/秒极高算力余量、Goroutine/Channel 并发模型与 Windows 纯 Go SQLite 零 CGO 单二进制分发）。
11. **Relay 结构配对去重规约 (Relay Pairing Model)** `[Scoped Observed / Provisional]`：仅在存在确凿 1-to-1 配对应用连接（时间重叠、流量高度吻合、链路结构包含关系）时才判定为底层中继去重；若出现 1-to-N 或 N-to-1 歧义则保守保留在 candidate，不执行去重扣减；$\text{Residual} = \Delta(\text{uploadTotal}) - \text{UniqueObserved}$。
12. **代理链拓扑因果顺序规约 (Hop Order Semantics)** `[Scoped Observed / Provisional: 当前测试的策略选择组与出站拓扑]`：`chains[0]` 为最终物理出站节点，`chains[last]` 为顶层规则分流策略组，UI 渲染按 `chains.slice().reverse()` 呈现。
13. **连接历史不可变性 (Routing Immutability)** `[Scoped Observed / Provisional: 当前受控长连接与测试拓扑]`：存活连接绑定创建时出站路径，受控实测显示节点切换不篡改已有存活连接的历史节点路径。
14. **Gap 恢复与 Bootstrap 规约**：冷启动/重连首帧已有连接记录为 baseline（delta=0），跨 Gap 存活连接增量归属为 Gap 期间累积流量；Gap 期间若发生 Epoch Break 则废弃跨 Gap 增量并新建 Epoch。
15. **推荐默认采样间隔 (Sampling Cadence)**：在测试的 Windows 11 环境下推荐默认 **`250ms`**（单核等效 CPU 占用仅 ~0.13%，Working Set < 11MB，最大化短连接捕获率），并允许用户自由配置为 500ms 或 1000ms。

---

## Open Questions

### 实现选型 (Phase 2 & Phase 3 决策项)

- Storage：SQLite + WAL（纯 Go `modernc.org/sqlite` 批量事务写入性能与阶段性 Checkpoint 策略）；
- UI：Tauri vs 本地 Web UI；
- UI 与 Collector：共享 SQLite 读取 vs 本地轻量 IPC / HTTP 查询端点。

---

## Current Risks

1. **短连接采样盲区**：实测证明轮询机制下短连接存在物理盲区，已在状态机中通过 Residual 残差显式建模。
2. **多跳代理 Double Counting**：多层代理底层连接已建立 1-to-1 保守 Relay 结构去重与歧义保留规约。
3. **敏感样本**：原始网络历史必须默认留在 `tmp/` 等 Git 忽略目录并在提交前脱敏。

---

## Next Step

进入 **Phase 2 — 本地存储、SQLite 模型与聚合引擎**：
1. 实现符合 `docs/phase2-storage-handoff.md` 的 SQLite + WAL 批量持久化消费者；
2. 设计 Connections 明细表、分时 Aggregates 表、Monitoring Gaps 审计表与 Residuals 表；
3. 验证长连接分段 checkpointing 与进程崩溃恢复。

---

## Recent Changes

### 2026-08-24 (Big Work Package C.1.2 — Phase 1 Final Evidence Closure)

- **Stage F1 (Production WebSocket)**：引入 `github.com/gorilla/websocket` 成熟库，支持 RFC 6455 握手校验、ReadDeadline Watchdog、TLS/WSS、Ping/Pong 与 Context 优雅停机；
- **Stage F2~F3 (Queue & State Hardening)**：完善全字段元数据演化与路由链突变健康告警；实现基于单调时钟的 Gap 持续时间计算；修复 Critical Push 错误检查；
- **Stage F4 (50x Canonical Replay)**：提交 Git 跟踪的 Golden Fixture，验证 50 次全字段事件流 Replay 哈希与 EventID 100% 绝对一致；
- **Stage F5 (Live Shadow 3.0)**：移除所有 Fallback 匹配，实现 NTP、短请求、持续下载（30 帧）的严格 Local Port 机械核验与故障重连注入测试（100% PASS）；
- **Stage F6 (Benchmark 3.0 & Soak)**：基于 Git 跟踪固件执行 60s+ 真实 Cadence 与 600s 生产 Soak 测试，RSS 稳定在 8.8~10.8MB 且无泄漏；
- **Stage F7~F8 (Final Gate)**：同步 RFC、ADR、Handoff 与 STATUS，生成 `docs/phase1-final-gate.json`，正式达成 Phase 1 Final Gate。
