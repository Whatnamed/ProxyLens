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
Phase 3  审计 UI                                  [IN PROGRESS — core UI implemented]
   ↓
Phase 3E Desktop Runtime Integration              [IN PROGRESS — 3E-1/3E-2A implemented; 3E-2B pending]
   ↓
Phase 4  Audit Intelligence                       [PLANNED]
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

## Phase 3 — Audit UI [IN PROGRESS]

### Phase 3A — UI Platform Foundation & Local Query API [COMPLETED]

- [x] Tauri v2 + React 19 + TypeScript + Vite；
- [x] Go Local Query API，只读 loopback + Bearer Token + CORS；
- [x] SQLite read-only / `query_only=ON`；
- [x] `docs/ui-api-contract-v1.md` 与 API 测试；
- [x] Tauri Sidecar 生命周期；
- [x] React 类型化 Query API client；
- [x] Windows 原生桌面构建验证。

### Phase 3B — Visual System & Primary Audit UI [IMPLEMENTED — VISUAL ACCEPTANCE PENDING]

- [x] Draft Design System v1：Light / Dark semantic tokens、Typography、Density、Interaction；
- [x] 正式 App Shell 与 Overview / History / Coverage 导航；
- [x] Overview：Traffic Summary、Evidence Trust、Top Processes / Rules / Hosts / Final Proxies；
- [x] Time Range、Route Focus、Freshness / System Status；
- [x] Temporary Diagnostics 退出默认产品入口；
- [x] UI semantic Closure、English / 中文 runtime locale + persistence 与 80 个前端回归测试；
- [x] 确定性 typography assets：Manrope Latin 400/500、Sarasa Gothic UI SC Regular 与 SemiBold→CSS 500、JetBrains Mono 400/500 WOFF2；Narrative 以 script-aware 轴组合，locale 不改变字体或几何；
- [ ] healthy / gaps / stale / empty / scaled 全 fixture 真实视觉验收；
- [ ] 1280×800 / 1600×1000 + Light / Dark 完整视觉验收；
- [ ] 根据验收结果决定是否 Freeze Design System v1；

### Phase 3C — History, Search & Connection Detail [PARTIALLY IMPLEMENTED]

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

仍待后续、且不得在无后端契约时伪造：

- [ ] 评估真正需要的额外搜索维度（如 Rule / Final Proxy / Port），必要时单独设计 read-only API extension；
- [ ] 大规模 History 的虚拟化/滚动策略，仅在真实数据量证明需要时实施；
- [ ] 更完整的跨页面键盘导航与真实 Tauri 可访问性 integration test。

### Phase 3D — Coverage, Gaps, Performance & Polish [PARTIALLY IMPLEMENTED]

已实现：

- [x] Coverage summary 与物理缺口流量估算芯片；
- [x] Covered / Controller Gap / Collector Offline / Outside Monitored History timeline；
- [x] Future 未来区间（中性斜纹）与条件摘要/图例；
- [x] merged gap provenance（含 mixed）；
- [x] Gap list 与 Timeline 双向 hover / 点击平滑滚动联动；
- [x] Inspect Around Gap → History（带入 ±15min 自定义区间并冻结快照）；
- [x] Collector heartbeat stale semantics 与 Coverage 后端一致；
- [x] System 状态渐进式披露对话框（完整 Meta、会话心跳、核算新鲜度落后、纯事实状态对话框与焦点闭环）；

仍待后续：

- [ ] Coverage / History 在 scaled fixture 下的真实交互性能验收；
- [ ] 必要时针对大数据量做虚拟化或渲染优化；
  - [ ] 桌面应用图标、安装版生命周期与启动性能最终打磨；
  - [ ] 最终视觉 polish 与 Design System freeze。

### Phase 3E — Desktop Runtime Integration [IN PROGRESS — Phase 3E-1 and 3E-2A complete / 3E-2B pending]

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

#### Phase 3E-2B — Installed Runtime & Secure Configuration [PLANNED]

- [ ] independent installed background supervisor / continuous whole-process crash restart；
- [ ] login/autostart policy；
- [ ] secure Mihomo Controller Secret provisioning/persistence；
- [ ] installer lifecycle、upgrade ownership 与 real installed-data-path validation；
- [ ] real FLClash/Mihomo validation 与最终 real-data visual acceptance。

---

## Phase 4 — Audit Intelligence [PLANNED]

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
