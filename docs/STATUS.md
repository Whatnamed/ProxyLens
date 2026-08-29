# ProxyLens 项目状态

> **用途：当前状态与 Agent handoff 的唯一快速入口。**
> 动态进度写在这里，不写进 `AGENTS.md`。长期阶段计划见 `ROADMAP.md`，重要历史里程碑见 `DEVLOG.md`。

---

## Current State

- **当前阶段**：Phase 3 Audit UI — C 组 Directed UI 已完成工程实现与独立审查 Closure，等待真实多状态视觉验收。
- **活动分支**：`experiment/qwen38max-directed-ui`
- **当前远端 HEAD（本次状态记录前）**：`a1b4d2b`
- **C 组原始交付**：`96b08cb`，保留不改写，用于保留实验原始结果。
- **Closure**：`96b08cb` 之后 6 个代码/测试修复提交 + 1 个状态文档提交；工程/语义 Gate 已通过。
- **当前唯一阻塞项**：真实渲染视觉验收，不是代码架构或产品语义 Closure。

### 已实现的正式 UI

- App Shell 与 V1 顶层导航：Overview / History / Coverage；
- Light + Dark semantic token / typography / density / interaction foundation；
- Overview：流量汇总、Evidence Trust、Top Processes / Rules / Hosts / Final Proxies；
- History：冻结 snapshot、受支持的过滤、显式分页、键盘可选连接行；
- Connection Inspector：Causal Path、Traffic Accounting、Evidence Quality、Lifecycle、Accounting Events、Raw Traffic Frames；
- Coverage：Coverage summary、Gap timeline、Controller / Collector provenance、Outside Monitored History、Inspect Around Gap；
- System Status：Collector / Accounting / DB 状态，heartbeat stale 规则与后端一致；
- ErrorBoundary 与 Query/API 可恢复状态。

### 独立审查 Closure 已完成

1. 修复 History Inspector 白屏根因：`qualityFlags` wire shape 兼容 `string[]` 与 boolean map，并增加 ErrorBoundary；
2. 时间范围变更统一走 `applyTimeRange`：重新冻结 History snapshot，并重置 page / selected；
3. Route Focus 变更重置分页与 selection；Custom 编辑草稿与 Applied Range 分离，只有 Apply 生效；
4. Coverage gap provenance 对齐后端：仅 `controller_stream` 为 Controller gap，其余 collector-side source 为 Collector offline，并支持 mixed；
5. Sidebar heartbeat stale 阈值对齐后端：`max(3 × heartbeatIntervalMs, 15000ms)`；
6. Overview `exact reconciled bytes` 修正为 `reconciled accounted bytes`，避免把 interval-derived 流量宣称为 Exact；
7. 新增 25 个 UI 回归测试，共 32 个测试；补齐 `npm test` 脚本。

---

## Current Validation

### Engineering / semantics

- C 组原始交付保留：PASS；
- Architecture boundary：PASS；
- Product / Query semantics：PASS；
- History snapshot / pagination state：PASS；
- Coverage provenance：PASS；
- System Status heartbeat consistency：PASS；
- Inspector crash resilience：PASS；
- UI regression coverage：32 tests（本地执行结果）；
- TypeScript / frontend build：Closure 报告为本地 PASS；
- 当前分支无远端 CI status，不能把本地 PASS 表述为 GitHub CI PASS。

### Rendered visual QA

已人工确认：

- healthy fixture 下基本运行链路；
- 7d 切换后 History 正常出现；
- Connection Inspector 可完整打开、关闭，无已知 console render error；
- Custom 编辑器打开不会静默改变 Applied Range。

仍待人工验收：

- `healthy / gaps / stale / empty / scaled` 全 fixture；
- 1280×800；
- 1600×1000；
- Light / Dark 两套主题在 History + Inspector、Overview、Coverage 上的完整视觉一致性；
- 字体是否升级为确定性产品资产，而不是依赖系统 fallback。

---

## Stable Product / Architecture Decisions

以下为当前实现仍必须遵守的核心事实：

1. ProxyLens 是代理流量审计与分流优化辅助工具，不是普通流量计费器、VPN Controller 或 Firewall。
2. 核心因果链：`Process → Destination → Rule → Policy / Proxy Chain → Final Physical Egress → Bytes`。
3. Unknown 必须可解释；Monitoring Gap 必须独立建模。
4. 产品 V1 只读，不修改 Mihomo 配置、规则、节点、TUN、系统代理或路由。
5. 本地优先，不上传网络历史或敏感 Payload。
6. 双事实权威：`event_journal`（网络观测）+ `collector_sessions`（采集生命周期）。
7. 查询架构：`React → Go Local Query API → read-only SQLite`；React 不直接读 SQLite，Rust 不复制 Go analytics/accounting。
8. Hop Order：`chains[0]` 为最终物理出站，`chains[last]` 为顶层策略组；历史不得被当前活动节点状态回写。
9. Gap / bootstrap / Accounting 的完整长期规则以 `ARCHITECTURE.md` 与 ADR 为准。

---

## Open Questions

- 最终视觉验收后，当前 Draft Design System v1 是否 Freeze；
- Public Sans / JetBrains Mono 是否作为确定性产品字体资产随应用交付，还是继续允许系统 fallback；
- 安装版长期数据路径规约（后续安装打包阶段确定）。

---

## Known Issues / Non-blocking Notes

- `applyTimeRange()` 在 Overview/Coverage 应用 quick range 时会提前生成 History snapshot；这不会造成显示范围与查询范围不一致，但与最初“进入 History 才 freeze”的措辞存在轻微行为差异。当前视为产品行为选择，待真实使用后决定是否调整。
- UI Design System 仍为 Draft，不应在最终视觉验收前标记 Frozen。

---

## Next Step

1. 对 C 分支做真实 Tauri 多状态、多尺寸视觉验收：`healthy / gaps / stale / empty / scaled`，至少覆盖 1280×800 与 1600×1000，并检查 Light / Dark。
2. 根据真实视觉问题做最小、系统性的 token / density / typography / pattern 调整；若 Design System 规则改变，同步 `docs/design/`。
3. 视觉验收通过后，决定是否 Freeze Design System v1，并规划 C 线进入正式主线的方式。
4. 后续功能开发按 `ROADMAP.md` 未完成项继续，不重新实现已经完成的 Phase 3 UI 基础能力。
