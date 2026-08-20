# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 0 — Documentation & Discovery (Work Package A: C3-C, C6, C4 Static Completed)`
- **代码状态**：尚未进入正式业务开发，技术栈未最终确定。
- **环境资产清单 (Environment Inventory)**：
  - OS: Windows 11 (AMD64)
  - 客户端: FLClash (PID 13436) + FlClashCore (PID 20320) 运行中
  - TUN 状态: 启用 (`device: FlClash`, `find-process-mode: always`, `enhanced-mode: fake-ip`, `mode: rule`)
  - Live 运行时内核: `Mihomo Meta v1.10.0` (GET `/version` 返回)
- **Controller 验证状态 (Controller Validation Status)**：
  - `Phase 0C-0 Gate: PASS` `[Observed]`：通过受控网络请求与进程相关性比对（curl.exe / 日常连接），100% 证明 Probe 成功接入正在承载 TUN 日常流量的 live FlClashCore (127.0.0.1:9090)。
  - `Phase 0C-1 DIRECT / PROXY Baseline: COMPLETED` `[Observed]`：成功捕获典型 DIRECT 样本 (`chains: ["DIRECT"]`) 与 PROXY 样本 (`chains: [出站节点, 策略组...]`)。
  - `Phase 0C-2 UDP / NTP / QUIC: COMPLETED` `[Observed]`：验证了 Windows TUN 下 UDP 进程归因完整性、受控 NTP 48B/48B 流量精确性与 UDP Pseudo-connection 留存现象（6秒以上）。
  - `Phase 0C-3A & 3B Connection Counter Semantics: COMPLETED` `[Observed]`：实测证明稳态长连接单调非递减计数与冷启动基线（Bootstrap vs Steady-State）。
  - `Phase 0C-3C Snapshot Cadence & Capture-Rate Matrix: COMPLETED` `[Observed]`：
    - 实测证明 Mihomo 原生支持 `?interval=<ms>`（250ms / 500ms / 1000ms），吞吐量在 236 连接下分别为 577 KB/s / 277 KB/s / 132 KB/s；
    - 执行 12 轮独立试验矩阵（N=600），量化短连接捕获盲区（1000ms 下短连接漏抓 84%~91%，250ms 下捕获率提升 3.5 倍至 56%），证明单帧捕获占 75%~100%。
  - `Phase 0C-6 Global Accounting & Reconciliation: COMPLETED` `[Observed]`：
    - 实测证明 `/traffic` 速率积分与全局计数器增量高度一致（误差 < 2.7%）；
    - 发现并实测证明链式代理/多跳底层连接（Underlying Relay Hop）会在连接层产生 200% 重复计数，确立了应用层连接与中继连接去重规约；
    - 250ms 采样下上传残差收敛至 0.6%（稳态长连接残差仅 0.05%）。
  - `Phase 0C-4 (Static Part) Routing Inventory: COMPLETED` `[Observed]`：
    - 盘点 27 类路由模式，确立 `chains` 数组逆向拓扑语义（`chains[0]` 为最终物理出口，`chains[last]` 为顶层分流规则组）。
- **当前项目级 Skill**：`.agents/skills/mihomo-data-source-validation/SKILL.md`。

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
10. **不提前锁死技术栈**：Go / Rust、SQLite、Tauri / Web UI 等均需要由前序验证推动决定。
11. **多跳中继去重规约 (Multi-hop Relay Deduplication)**：必须在连接层过滤/分离底层中继连接（`rule === "" && process === ""`），杜绝应用流量被双重计算。
12. **代理链拓扑顺序规约 (Hop Order)**：`chains[0]` 为物理出口节点，UI 渲染按 `chains.slice().reverse()` 呈现因果链路。
13. **快照采样推荐**：短连接敏感场景推荐 `/connections?interval=250` 或 `500`。

---

## Open Questions

### Mihomo 数据源与后续验证项 (Phase 0C-5 / Phase 0D)

- Mihomo 重载 / 重启、Controller 断开后 connection id 和全局计数的变化与恢复状态机 (C5)？
- 动态切换节点时的代理链迁移与流量割接行为？
- MetaCubeXD 同场实测对比验证。

### 实现选型

这些问题在 Phase 0 结束后再决定：

- Collector：Go vs Rust；
- Storage：SQLite + WAL 是否正式确认；
- UI：Tauri vs 本地 Web UI 等；
- UI 与 Collector：共享数据库读取 vs 本地 IPC / HTTP API；
- 历史明细保留与聚合策略。

---

## Current Risks

1. **短连接采样盲区**：实测证明轮询机制下短连接存在物理盲区，必须在架构上支持 Residual 残差表达。
2. **多跳代理 Double Counting**：多层代理会产生底层连接，已建立去重规约。
3. **连接生命周期语义**：不能在实测前假设“从下一帧消失”永远等同于正常关闭（需记录 `possible_unobserved_tail`）。
4. **流量正确性**：长连接、WebSocket 重连、Mihomo 重启、Collector 重启都可能造成 double counting 或漏记。
5. **敏感样本**：原始网络历史可能包含隐私信息，必须默认留在 `tmp/` 等 Git 忽略目录并在提交前脱敏。

---

## Next Step

准备进入 Work Package B（Phase 0C-5 内核重启 / Controller 断线重连与动态切换测试）：
- Mihomo 重启与 Controller 断开状态机实测；
- 动态代理节点切换流量追踪。

---

## Recent Changes

### 2026-08-20 / 2026-08-21 (Big Work Package A)

- **Stage A0**：修正研究报告中的 Connection ID 稳定性范围、长连接尾部 +24B 及 `disappeared_from_snapshot` 状态机语义。
- **Stage A1 (/connections Interval & Cadence)**：实测验证 Mihomo 原生支持 250ms/500ms/1000ms 快照间隔并评估吞吐量。
- **Stage A2 & A3 (Capture-Rate Matrix)**：
  - 新增短请求 Ground Truth 工具与匹配分析器；
  - 自动化执行 12 轮独立试验（N=600），量化短连接捕获盲区（1000ms 漏抓 84%~91%，250ms 捕获率 56%），证明单帧捕获占 75%~100%。
- **Stage A4 (Global Accounting & Reconciliation)**：
  - 新增对账分析工具 `tools/discovery/analyze-accounting.mjs`；
  - 证实 `/traffic` 速率积分与全局计数器误差 < 2.7%；
  - 发现并实测证明链式代理底层连接双重计数问题，建立 Multi-hop 去重规约；
  - 上传残差在 250ms 快照下收敛至 0.6%（稳态 0.05%）。
- **Stage A5 (Static Routing Inventory)**：
  - 新增路由拓扑盘点工具 `tools/discovery/summarize-chains.mjs`；
  - 盘点 27 类路由模式，明确 `chains` 数组逆向拓扑语义（`chains[0]` 为最终物理出口）。
- **Stage A6 (Final Docs Integration & Review)**：
  - 整合研究报告 `docs/research/mihomo-data-source.md`、`docs/STATUS.md` 与 `docs/ARCHITECTURE.md`。




