# ProxyLens 开发与验证路线图

> 本文件记录长期阶段计划和 milestone 状态，不承担当前分支 handoff。当前状态与下一步以 `docs/STATUS.md` 为准。

---

## 路线图原则

1. **验证导向**：先确认真实数据语义，再扩大产品能力。
2. **正确性优先**：归因、生命周期、double counting、重启与 Monitoring Gap 高于视觉丰富度。
3. **阶段验收优于主观排期**：只把有证据的实现标为完成。
4. **已实现不等于已验收**：工程实现、语义 Closure、视觉验收可以是不同状态。
5. **不为路线图补齐而发明能力**：未有后端/产品契约支持的筛选、排序、评分或控制能力保持 Deferred。

---

## 阶段概览

```text
Phase 0  Mihomo 数据源验证                         [COMPLETED]
   ↓
Phase 1  Collector 原型                           [COMPLETED]
   ↓
Phase 2  持久化、核算与运行时验证                  [COMPLETED]
   ↓
Phase 3  审计 UI                                  [COMPLETED — CORE V1]
   ↓
Phase 3E Desktop Runtime Integration              [CORE COMPLETE — real-environment validation deferred]
   ↓
Phase 3S Production Storage & Accounting Scale    [COMPLETED]
   ↓
Phase 4  Audit Intelligence                       [IN PROGRESS — 4A FOUNDATION COMPLETE]
   ↓
Later    长期增强                                 [PLANNED]
```

---

## Phase 0 — Mihomo Data Source Discovery [COMPLETED]

目标：在真实 Windows 11 + Mihomo + FLClash 环境确认 External Controller 数据语义、字段覆盖、连接生命周期、代理链顺序与断线/恢复行为。

已完成核心成果：

- 真实场景验证 DIRECT / PROXY / REJECT、TCP / UDP / QUIC、长短连接；
- 明确 Documented / Observed / Inferred 证据等级；
- 形成 `docs/research/mihomo-data-source.md` 与相关验证资产；
- Mihomo First 成为后续 Collector 的事实基础。

---

## Phase 1 — Collector Prototype [COMPLETED]

目标：构建轻量、只读、低开销 Collector 原型与确定性连接状态机。

已完成核心成果：

- Mihomo External Controller 采集；
- Active Connection 状态与流量 delta；
- 出现 / 更新 / 消失生命周期；
- Controller 断线 / 重连；
- 重启与不可安全续算区间的显式表达；
- 第一轮真实性能与稳定性验证。

---

## Phase 2 — Local Storage, Accounting & Runtime Validation [COMPLETED]

### Phase 2A — Storage Foundation & Event Journal [COMPLETED]

- [x] SQLite + WAL；
- [x] `event_journal` 与 `collector_sessions` 双事实权威；
- [x] `connections` / `connection_traffic` / `monitoring_gaps` 等投影；
- [x] `RebuildProjections`；
- [x] Storage inspect / gaps / rebuild CLI；
- [x] 真实 NTP、短请求、持续下载、离线 Gap 验证。

### Phase 2B1 — Accounting, Lifecycle & Aggregations [COMPLETED]

- [x] 连接观察生命周期语义；
- [x] `accounting_runs` / `relay_relations` / `accounted_traffic`；
- [x] Conservative Relay Reconciliation v1；
- [x] `usage_hourly_dimensions` 物化聚合；
- [x] Coverage interval union / Known Scope；
- [x] AnalyticsService 与 accounting/analytics CLI。

### Phase 2B2 — Runtime Ops & Validation [COMPLETED]

- [x] `journal_sequence` 核算边界；
- [x] Freshness / Staleness；
- [x] Collector heartbeat 与 runtime liveness gap；
- [x] Safe Derived Retention；
- [x] WAL / integrity 运维；
- [x] 多 cadence、rebuild、soak 与 PRODUCT A–E 验收。

---

## Phase 3 — Audit UI [COMPLETED — CORE V1]

### Phase 3A — UI Platform Foundation & Local Query API [COMPLETED]

- [x] Tauri v2 + React 19 + TypeScript + Vite；
- [x] Go Local Query API，只读 loopback + Bearer Token + CORS；
- [x] SQLite read-only / `query_only=ON`；
- [x] `docs/ui-api-contract-v1.md` 与 API 测试；
- [x] Tauri Sidecar 生命周期；
- [x] React 类型化 Query API client；
- [x] Windows 原生桌面构建验证。

### Phase 3B — Visual System & Primary Audit UI [COMPLETED — DESIGN SYSTEM FROZEN V1]

- [x] Design System v1：Light / Dark semantic tokens、Typography、Density、Interaction；
- [x] 正式 App Shell 与 Overview / History / Coverage 导航；
- [x] Overview：Traffic Summary、Evidence Trust、Top Processes / Rules / Hosts / Final Proxies；
- [x] Time Range、Route Focus、Freshness / System Status；
- [x] Temporary Diagnostics 退出默认产品入口；
- [x] UI semantic Closure、English / 中文 runtime locale + persistence 与 80 个前端回归测试；
- [x] 确定性 typography assets：Manrope Latin 400/500、Sarasa Gothic UI SC Regular 与 SemiBold→CSS 500、JetBrains Mono 400/500 WOFF2；Narrative 以 script-aware 轴组合，locale 不改变字体或几何；
- [x] healthy / gaps / stale / empty / scaled 全 fixture 真实 Tauri query-only 视觉验收；
- [x] 1280×800 / 1440×900 / 1600×1000 + Light / Dark + EN / 中文视觉验收；
- [x] 根据验收结果 Freeze Design System v1；

### Phase 3C — History, Search & Connection Detail [CORE COMPLETE — OPTIONAL/DEFERRED ENHANCEMENTS]

已实现：

- [x] 历史连接列表，newest-first；
- [x] Time / Route / Process / Host / Destination IP / Network 等当前 Query API 支持的筛选（保持即时筛选）；
- [x] 冻结 History snapshot 与 Live Analysis Range（30s 低频推进）；
- [x] 显式 offset pagination，不伪造 total count；
- [x] Connection Inspector；
- [x] Causal Path；
- [x] Traffic Accounting、Evidence Quality、Lifecycle；
- [x] Accounting Event Timeline；
- [x] Raw Traffic Frames advanced disclosure；
- [x] 行级键盘选择与可恢复 render error boundary；
- [x] Select / DatePicker composite keyboard navigation、focus-out close 与 ARIA semantics；
- [x] Connection Inspector 边界受控前/后连接切换（Prev/Next 按钮、↑/↓/Esc 快捷键与输入框避让保护）；
- [x] 调查上下文管理（Overview 钻取清空无关筛选、侧边栏保留当前调查、Coverage 缺口检查清空旧条件并带入 ±15min）；

后续增强（不阻塞 Phase 3 core closure，且不得在无后端契约时伪造）：

- [x] Phase 4A narrow exact Rule filter for Review investigation；Final Proxy / Port 与更丰富的 Rule/Payload 搜索仍需单独 read-only API contract；
- [ ] 大规模 History 的虚拟化/滚动策略，仅在真实数据量证明需要时实施；
- [ ] 更完整的跨页面键盘导航与真实 Tauri 可访问性 integration test。

### Phase 3D — Coverage, Gaps, Performance & Polish [CORE COMPLETE — OPTIONAL/DEFERRED ENHANCEMENTS]

已实现：

- [x] Coverage summary 与物理缺口流量估算芯片；
- [x] Covered / Controller Gap / Collector Offline / Outside Monitored History timeline；
- [x] Future 未来区间（中性斜纹）与条件摘要/图例；
- [x] merged gap provenance（含 mixed）；
- [x] Gap list 与 Timeline 双向 hover / 点击平滑滚动联动；
- [x] Inspect Around Gap → History（带入 ±15min 自定义区间并冻结快照）；
- [x] Collector heartbeat stale semantics 与 Coverage 后端一致；
- [x] System 状态渐进式披露对话框（完整 Meta、会话心跳、核算新鲜度落后、纯事实状态对话框与焦点闭环）；
- [x] Coverage / History 在 scaled fixture 下的真实交互性能验收；
- [x] 最终视觉 polish 与 Design System freeze。

后续增强（不阻塞 Phase 3 core closure）：

- [ ] 必要时针对大数据量做虚拟化或渲染优化；
  - [ ] 桌面应用图标与启动性能最终打磨；

### Phase 3E — Desktop Runtime Integration [CORE COMPLETE — REAL-ENVIRONMENT VALIDATION DEFERRED]

Phase 3E runtime and installed lifecycle core is complete. Real FLClash/Mihomo and
real-data validation remains a separate deferred acceptance boundary.

已完成 Phase 3E-1：

- [x] reusable live collector runner；
- [x] standalone `proxylens-runtime`；
- [x] scheduled accounting（30s default、skip-if-fresh、non-reentrant、failure non-fatal）；
- [x] canonical `%LOCALAPPDATA%\ProxyLens\data\proxylens.db` 与 `PROXYLENS_DB_PATH` / `PROXYLENS_DATA_DIR` override contract；
- [x] Query API / Tauri read-only path contract；
- [x] mock controller + temporary DB runtime end-to-end test；
- [x] desktop bundle includes Query API 与 Runtime binaries。

#### Phase 3E-2A — Windows Runtime Ownership & Tauri Ensure-Start [COMPLETED]

- [x] per-authority-DB Windows Runtime ownership 与 path-keyed single instance；
- [x] `READY` / `ALREADY_RUNNING` startup handshake；
- [x] Tauri ensure-start、first-run writable DB ordering 与 existing-only Query path；
- [x] UI close 后 Runtime 保持运行、reopen 复用既有 Runtime；
- [x] random-port mock Controller + temporary data directory lifecycle smoke。

#### Phase 3E-2B1 — Background Supervisor & Secure Runtime Configuration [COMPLETED]

- [x] independent `proxylens-supervisor` process-continuity owner；
- [x] per-authority-DB Supervisor single-instance 与 Runtime named-mutex presence/observation；
- [x] bounded whole-process Runtime crash restart、existing Runtime takeover 与 exact `STOP\n` lifecycle contract；
- [x] non-secret `runtime.json` config path/schema/atomic write 与 Controller precedence；
- [x] Windows Credential Manager Generic Credential、random E2E target isolation 与 `MIHOMO_SECRET` explicit override；
- [x] Tauri ensure Supervisor → Runtime，Query-only UI lifecycle，Supervisor/Runtime UI-close survival 与 reopen reuse；
- [x] mock-only secure credential / crash-restart / Tauri lifecycle acceptance。

#### Phase 3E-2B2A — Installed Runtime Lifecycle [COMPLETED]

- [x] current-user Windows Task Scheduler owner、LogonTrigger login start 与无限 `PT1M` repetition-based Supervisor recovery；
- [x] exact installed control/status、per-DB stop event 与 config v2 autostart preference；
- [x] NSIS current-user fresh install、upgrade quiesce/reconcile、disabled preference preservation 与 uninstall cleanup；
- [x] actual installed binary layout 与 isolated Package A → Package B → uninstall acceptance；

#### Phase 3E-2B2B — Settings & Installed Product Polish [COMPLETED]

- [x] sidebar secondary utility Settings dialog、Controller URL / secure Secret / Windows login autostart 与 installed owner facts；
- [x] effective-source metadata、strict bounded stdin config apply、keep / replace / clear Secret contract、safe rollback 与 saved-pending-restart state；
- [x] developer checkout installed-layout gate、exact lifecycle rebootstrap、autostart-only non-disruption 与 EN / 中文、Light / Dark utility-dialog states；
- [x] mock-only installed product acceptance：random mock Controller、random WinCred/task identities、Secret replacement、autostart false → true、UI-close survival 与 same-DB preservation；
- [ ] real FLClash/Mihomo validation 与 real-data visual acceptance（Deferred；synthetic query-only visual acceptance 已完成）。

---

## Phase 3S — Production Storage & Accounting Scale Closure [COMPLETED]

目标：在真实生产库规模（~1.5M journal events / 5.2GB）下，修正三处规模化缺陷——零增量原始证据密度、周期性全量核算重建、WAL 增长策略——且不削弱任何审计语义，并以 E: 盘生产规模验收加生产库短时 revalidation 收口。

背景：只读 root-cause measurement 确认真实生产库 97.63% 的 `ConnectionDelta` 原始行是零增量重复证据（wall-clock 口径），且常规 30s 核算 tick 在 stale 时会触发全历史重建，WAL 靠每 tick TRUNCATE 压制。

已完成核心成果：

- [x] Raw delta density 修正：StateEngine 共享 emission contract（steady-state 与 reconnect-recovery 同路径），零字节帧不再 emit 全量 `ConnectionDelta`，改为 `ConnectionPresenceCheckpoint` 稀疏在场证据（30s/active connection named constant）；非零 delta 全保留；metadata/rule/chain/counter/relay/gap 证据语义不变；presence 只更新 durable liveness/last-observed，不产生 `connection_traffic`；`ConnectionDisappeared` 携带 engine 内存中的精确 final presence（`lastObservedAt` + counters）由投影确定性落库；不删除任何历史 raw 行；
- [x] Incremental Accounting v2（migration 008，additive）：generation-based 增量推进，常规 30s tick 只处理 `(publishedBoundary, newBoundary]` 的 frame-aligned 有界区间，派生写入与 boundary 推进单事务原子发布（失败/取消零残留、幂等重试）；full rebuild 仅限显式 seed/repair/migration；v2 未激活时 Query/API 回退 legacy；relay/dedup 语义与 legacy 共享同一分类/分配/行构造代码路径；
- [x] WAL / failed-run hygiene：取代 `2bfd8c5` 每 tick TRUNCATE——稳态 PASSIVE checkpoint + 结构化 `Busy/LogFrames/CheckpointedFrames` 遥测；TRUNCATE 仅在 shutdown/maintenance 安全边界以 fresh non-canceled context 执行；失败/暂存 derived 行 bounded cleanup；Collector ingestion 优先于 accounting maintenance；
- [x] 同 epoch 重观测投影契约修复：`ConnectionNew` 由纯 INSERT 改为 re-observation-aware upsert（重开终态行、保留原始 `first_observed_at`、journal 双事件保留、rebuild 重放确定性一致），并使 baseline 计数器与 Bootstrap 契约一致；
- [x] E: 盘生产规模验收（`proxylens-scale-acceptance` 工具，全部 PASS）：zero-heavy density（97.62% `ConnectionDelta` 零增量行削减、字节和精确；该比例仅针对 delta 行，非整体 journal 行数削减）、真实生产库 E-copy 迁移+seed（1.55M events、61 chunks、456.6s、WAL 峰值 8.5MB、authority 字节不变、quick_check ok）、crash/cancel/publish-boundary/checkpoint 竞争/失败 generation 清理、50K vs 1.57M 常数成本（0.42s vs 0.34s 同批增量）、30min 连续 soak（Collector+增量核算+只读 Query 负载+WAL 遥测）；
- [x] 生产 C: 库 15–30min 短时 revalidation（真实 Controller 只读 GET/WS、migration 008 + auto-seed、密度/WAL/lag/磁盘遥测采样），结束后再次停止 collection，不恢复 24/7 常驻；
- [x] ADR 0010（`docs/decisions/0010-production-scale-storage-and-incremental-accounting.md`）记录全部决策与验证证据。
- [x] Correctness Closure（独立 review 11 blocker）：重观测 counter-diff 续算（tombstone）、v2 lifecycle 显式 lastObs/terminal 契约与 authoritative boundary frame time、bounded dirty closure 高基数规模门（cardinality phase：writer-hold 7.4→38.7ms 有界，prep off-lock）、两阶段 chunk writer-lock 契约与 boundary race 修复、migration 009 单活跃 generation invariant、equivalence fixture 修正、shutdown flush 有界 catch-up、scale harness 卷位 fail-closed、只读容量证据 + disk guard fail-safe（floor=max(1GiB,15% DB size)，breach 显式证据 + clean stop）、automatic background seed 契约文档化、生产 generation 受控 supersede/reseed 验证为结果逐字节一致（无需 repair）。

明确不做（本阶段边界）：

- Final Full Tauri synthetic query-only visual acceptance 已完成；Phase 4 不属于本 Phase 3S closure 的验收范围，后续 4A 已单独收口；真实 FLClash/Mihomo / real-data validation 仍 Deferred；Design System v1 已 Frozen；
- 不删除/重写/VACUUM 真实 authority DB；不改变 raw authority 语义；不修改 Mihomo/FLClash 任何状态。

---

## Phase 4 — Audit Intelligence [IN PROGRESS — PHASE 4A COMPLETE]

目标：在可靠历史之上，用透明、可解释的规则筛选“值得检查的代理流量”，辅助用户优化分流。

初始候选：

- 首次走代理的后台进程；
- Windows / 安全软件后台服务走代理；
- `MATCH` 兜底的大流量；
- `NETWORK,udp` 等宽泛 UDP 规则；
- 只有 IP、缺少域名的大流量；
- 单连接或单进程异常增长；
- 过去长期 DIRECT、近期变成 PROXY 的目标。

约束：

- 所有判断必须解释“为什么被标记”；
- 不引入不可解释的黑盒 Trust Score；
- 规则建议可复制但不自动应用；
- 不修改 Mihomo 配置、节点或系统网络状态。

### Phase 4A — Audit Intelligence Foundation [COMPLETED]

- [x] active v2 / completed legacy accounting authority resolution；
- [x] fixed PROXY-only Review endpoint with bounded `limitPerKind` and `[from,to)` semantics；
- [x] deterministic `MATCH` fallback、canonical broad UDP、IP-only target 与 large physical connection detectors；
- [x] exact / interval-derived evidence split、named 100 MiB threshold、stable subject IDs；
- [x] Review workspace between Overview and History、shared Time Range、read-only Rule investigation filter、EN / 中文、Light / Dark、keyboard-safe controls；
- [x] synthetic review fixture、10k/100k timing evidence 与 8-state query-only Tauri acceptance；
- [x] no score/severity/black-box inference and no Controller/network lifecycle side effects。

### Phase 4B / 4C — Deferred

- [ ] historical route-change comparison、first-seen/background-service candidates、suggested rules or scoring；
- [ ] richer rule/final-proxy/port dimensions、real FLClash/Mihomo and real-data validation；
- [ ] any automatic configuration or network action remains explicitly out of scope。

---

## Later — Long-term Enhancements [PLANNED]

核心审计链稳定并完成 Phase 3 验收后再评估：

- 机场套餐周期与用户自定义重置日；
- 节点倍率和机场计费估算；
- 更长历史的聚合 / 清理策略；
- CSV / JSON 导出；
- 多 Mihomo GUI 兼容验证；
- Linux / macOS；
- 若长期实测证明 Mihomo 数据源存在不可接受盲区，再评估第二观测数据源。
