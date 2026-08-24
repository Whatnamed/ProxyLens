# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 1 — Collector Architecture, Prototype & Benchmark (Package C.1 Correctness Closure: PASS)`
- **代码状态**：Phase 1 生产原型及其正确性漏洞已全部闭环（Go 1.24+）。实现单一有序 Ingestion 通道、Gap 序列化与跨 Gap Epoch Break 检测、Relay 结构去重生产核算、完整审计元数据保留、计数器语义分离与 Fail-Safe Sink；Stage D7 机械 Shadow Validation（100% PASS）与 Stage D8 真实 Windows 进程基准测试全部完成。
- **环境资产清单 (Environment Inventory)**：
  - OS: Windows 11 (AMD64) / 12th Gen Intel Core i5-12400 (12 cores)
  - 客户端: FLClash (PID 13436) + FlClashCore (PID 20320) 运行中
  - TUN 状态: 启用 (`device: FlClash`, `find-process-mode: always`, `enhanced-mode: fake-ip`, `mode: rule`)
  - Live 运行时内核: `Mihomo Meta v1.10.0` (GET `/version` 返回)
- **基准测试与资源实测 (Measured Process Benchmark)**：
  - 1000ms 快照：Process CPU 0.172s / 1000 帧（单帧 0.172ms CPU），RSS Peak 14.52 MB
  - 500ms 快照：Process CPU 0.156s / 1000 帧（单帧 0.156ms CPU），RSS Peak 14.58 MB
  - 250ms 快照：Process CPU 0.297s / 1000 帧（单帧 0.297ms CPU），RSS Peak 14.68 MB
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
11. **Relay 结构配对与残差模型**：仅在存在确凿配对应用连接（时间重叠、流量高度吻合、链路结构包含关系）时才判定为底层中继去重；$\text{Residual} = \Delta(\text{uploadTotal}) - \text{UniqueObserved}$（其中 $\text{UniqueObserved} = \text{KnownApp} + \text{UnpairedMissingAttr} + \text{OtherUnique}$）。
12. **代理链拓扑因果顺序规约 (Hop Order Semantics)**：`chains[0]` 为最终物理出站节点，`chains[last]` 为顶层规则分流策略组，UI 渲染按 `chains.slice().reverse()` 呈现。
13. **连接历史不可变性 (Routing Immutability)**：存活连接绑定创建时出站路径，受控实测显示节点切换不篡改已有存活连接的历史节点路径。
14. **Gap 恢复与 Bootstrap 规约**：冷启动/重连首帧已有连接记录为 baseline（delta=0），跨 Gap 存活连接增量归属为 Gap 期间累积流量；Gap 期间若发生 Epoch Break 则废弃跨 Gap 增量并新建 Epoch。
15. **推荐默认采样间隔 (Sampling Cadence)**：推荐默认 **`250ms`**（在 Windows 单核 CPU 占用 < 0.3%、RSS < 15MB 的极低开销下最大化短连接捕获率），并允许用户自由配置为 500ms 或 1000ms。

---

## Open Questions

### 实现选型 (Phase 2 & Phase 3 决策项)

- Storage：SQLite + WAL（纯 Go `modernc.org/sqlite` 批量事务写入性能与阶段性 Checkpoint 策略）；
- UI：Tauri vs 本地 Web UI；
- UI 与 Collector：共享 SQLite 读取 vs 本地轻量 IPC / HTTP 查询端点。

---

## Current Risks

1. **短连接采样盲区**：实测证明轮询机制下短连接存在物理盲区，已在状态机中通过 Residual 残差显式建模。
2. **多跳代理 Double Counting**：多层代理底层连接已建立 Pair-specific Relay 结构去重与未配对保留规约。
3. **敏感样本**：原始网络历史必须默认留在 `tmp/` 等 Git 忽略目录并在提交前脱敏。

---

## Next Step

进入 **Phase 2 — 本地存储、SQLite 模型与聚合引擎**：
1. 实现符合 `docs/phase2-storage-handoff.md` 的 SQLite + WAL 批量持久化消费者；
2. 设计 Connections 明细表、分时 Aggregates 表、Monitoring Gaps 审计表与 Residuals 表；
3. 验证长连接分段 checkpointing 与进程崩溃恢复。

---

## Recent Changes

### 2026-08-24 (Big Work Package C.1 — Phase 1 Correctness Closure)

- **Stage D1 (Ordered Continuity & Gap Fix)**：实现统一有序 Ingestion Item 通道；修复 Gap Opened 乱序；修复启动未连上误判 Gap；修复连续重试重写 gap_start 问题；实现 Gap 期间 Epoch Reset 识别与安全恢复。
- **Stage D2 (Attribution & Safe Residual)**：将 Relay 结构去重真正接入状态机核算，严格分离 `UniqueObserved` 与 `Residual` 计算；收紧 Initial Attribution 规则。
- **Stage D3 (Event Contract & Full Audit Fields)**：补齐 17 个原始审计字段；分离 Raw Counter 与 Monitored Cumulative；引入确定性 `EventID` 与序列号；实现 Fail-Safe Sink 检查。
- **Stage D4~D5 (Queue, Memory & Client Hardening)**：引入 `ProductionStatsSink` 杜绝内存泄漏；实现阻塞 Push 超时保护与过载降级；补齐 WebSocket TLS/WSS 与 Ping/Pong 支持。
- **Stage D6 (Deterministic Tests)**：新增 Gap 期间 Epoch Reset、Relay 去重核算、Fail-Safe Sink、Mock Controller 重连等全面测试（Go 测试 100% PASS）。
- **Stage D7 (Mechanical Shadow Validation)**：完成同场只读验证，实现 NTP UDP、持续下载与断线重连等机械门禁 100% PASS。
- **Stage D8 (Real Process Benchmark)**：实测 Windows 进程 CPU 与 Working Set（250ms 下 CPU < 0.3%, RSS 14.68MB），生成 `docs/benchmarks/phase1-collector-benchmark.json`。
- **Stage D9 (Phase 2 Handoff)**：重写 `docs/phase2-storage-handoff.md`，正式通过 Phase 1 Final Gate。





