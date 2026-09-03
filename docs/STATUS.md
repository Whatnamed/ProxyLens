# ProxyLens 项目状态

> **用途：当前状态与 Agent handoff 的唯一快速入口。**
> 动态进度写在这里，不写进 `AGENTS.md`。长期阶段计划见 `ROADMAP.md`，重要历史里程碑见 `DEVLOG.md`。

---

## Current State

- **当前阶段**：Phase 3 Audit UI — C 组 Directed UI 已完成工程实现、独立审查与前端交互/语义收口（严格执行 `PROXYLENS_C_FRONTEND_INTERACTION_CLOSURE_PLAN_2026-09-03.md` Stage 1 ~ Stage 7 100% 验收收口）；UI 单元测试达 62 个（15 个 Suite 全部 PASS），Tauri Release E2E 探针冒烟与 100,000 事件性能基准均 100% PASS。
- **活动分支**：`experiment/qwen38max-directed-ui`
- **当前代码线**：以当前 Git branch HEAD 与对应远端分支为准；本文件不固定容易漂移的 commit SHA。
- **C 组原始交付**：`96b08cb`，保留不改写，用于保留实验原始结果。
- **Closure**：`96b08cb` 之后的代码、测试、文档、focused UI polish 与 Frontend Interaction Closure 均已保留；工程/语义 Gate 全部通过。
- **当前视觉 Freeze 前剩余事项**：
  1. Full Tauri multi-fixture visual acceptance。

### 已实现的正式 UI

- App Shell 与 V1 顶层导航：Overview / History / Coverage；
- Light + Dark semantic token / typography / density / interaction foundation；
- Overview：流量汇总（全局与分流作用域隔离）、Evidence Trust、Top Processes / Rules / Hosts / Final Proxies、Unknown Route 异常展示；
- History：双重时间模型（Live Analysis Range vs Frozen History Snapshot，30s 低频向前推进，关闭区间稳定，独立 `refreshHistory()` 刷新）、即时筛选保留、显式分页、键盘可选连接行；
- 调查上下文（Investigation Context）：Overview 钻取清空旧条件并重置选择、侧边栏切回保留现有调查、Coverage 缺口检查清空旧条件并带入 ±15min 区间；
- Connection Inspector：Causal Path、Traffic Accounting、Evidence Quality、Lifecycle、Accounting Events、Raw Traffic Frames；增加前一条/后一条边界受控切换（Prev/Next 与 J/K/[,/] 快捷键，输入框避让保护，表格行自动滚动视口同步）；
- Coverage：Coverage summary、Gap timeline、Controller / Collector provenance、Outside Monitored History、Inspect Around Gap；Future 灰色斜纹占位与条件摘要/图例；Gap 物理流量估算芯片；Timeline ↔ Gap List 双向 hover 与点击平滑滚动联动；
- System Status：紧凑状态指示器，点击呼出系统与运行诊断对话框（System & Runtime Diagnostics），展示 Collector 会话与心跳、Accounting 引擎与 Freshness 落后、数据库状态与 Schema 版本，异常时提供针对性修复建议并支持一键复制诊断 JSON；
- ErrorBoundary 与 Query/API 可恢复状态。
- Design System family overlays：自定义 date/time picker、network/page-size listbox、系统诊断弹窗，共用 surface / border / radius / selected / hover / focus / shadow 语言；
- Overlay accessibility：Select 使用单一 listbox focus + `aria-activedescendant`；DatePicker 使用 `grid → row → gridcell` 与单一 roving day focus，支持方向键跨月移动和 focus-out close；Dialog 具备焦点管理、Escape 监听与 backdrop 关闭；
- Compact inline alignment：固定高度控件统一 optical center；多列、因果链和事件时间线让 key/timestamp 与 primary content 共用 first-baseline；Status 的 marker 对齐 primary label，Causal Path / Accounting Events 的 marker 对齐 primary 首行，secondary 内容独立下沉，连接线位于 marker 下方；不使用 optical-shift token，共用 `--pl-leading-control` 与 compact label/text-box progressive enhancement；
- 全局 UI locale：English / 中文即时切换、`localStorage` 持久化、`document.lang` 同步、locale-aware date/time formatting；原始技术证据值保持不翻译；严格保持 1:1 键名对齐。
- 确定性 typography：正式产品 bundled Manrope 400/500、Sarasa Gothic UI SC Regular 与 SemiBold→CSS 500、JetBrains Mono 400/500 WOFF2；Narrative 以 Latin/CJK script axis 组合，locale 不改变字体或几何，系统字体只作最后 fallback；正式 Inspector surface 为 Warm Paper（Light `#f8f7f4` / Dark `#191817`）。

### 独立审查与交互收口已完成

1. **语义基准收口**：修复 Overview 顶部全局卡片在 routeFocus 激活时不当受限的问题，顶栏保持反映全部实际流量构成；严格核查证据范围，Unknown Route 在存在时作为异常 chip 展示；
2. **时间模型收口**：实现 Live Analysis Range（Overview/Coverage 随 30s 低频时钟推进）与 Frozen History Snapshot 分离；关闭区间（yesterday/custom）严格不推进；History 独立 refresh 仅推进 live shortcut，关闭区间保持 window 不变；
3. **调查上下文保护**：从 Overview 钻取到 History 时清空无关旧条件、重置分页与选择并冻结新 snapshot；通过侧边栏切回 History 保持现有 snapshot 与条件；从 Coverage 钻取清空旧条件并冻结 ±15min 自定义区间；History 保持原生即时筛选无额外 Apply 负担；
4. **语义完整性补充**：Coverage 支持 Future 区间（中性 45° 斜纹）、动态摘要与图例仅在存在未来区间时显示；窗口级 Gap 流量估算以 `pl-evidence-chip--estimated` 标出；
5. **调查效率提升**：Inspector 增加上一条/下一条导航按键与 J/K/[,/] 快捷键，边界自动禁用，选中时表格行平滑居中；Coverage Timeline 缺口块与 Gap 列表行建立双向 hover 高亮与点击滚动关联；
6. **系统状态渐进披露**：侧边栏状态栏升级为可交互触发器，点击展开 System & Runtime Diagnostics 弹窗，显示完整 Meta、会话心跳、核算落后、Schema 版本并给出异常诊断建议与诊断数据复制；
7. **自动化测试与端到端验证**：前端单元测试从 42 项扩充至 62 项（覆盖时间演进、快照冻结、钻取重置、Future 分段 7 种边界、核算落后 vs 失败、DB 不兼容等），15 个 Suite 全部通过；Phase 0 测试 18/18 通过；Go 收集器测试全部通过；Tauri Release 可执行文件端到端冒烟 100% 通过；100k 数据集查询基准全部通过。

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
- TypeScript / frontend build：本轮本地 PASS；
- UI regression coverage：62 tests（本地执行结果），包含 locale dictionary parity、静态 translation-key audit、calendar navigation utilities、time snapshot、drill reset、future segments 与 system diagnostics；
- Locale dictionary parity：EN / 中文各 396 个 key，静态 UI translation keys 严格 1:1 对齐无缺失；
- Typography asset build：6 个 WOFF2、17,066,080 bytes（约 17.07MB / 16.28MiB）；Manrope/Sarasa/JetBrains 均为本地正式资产，无 TTF/WOFF/italic 或额外字重；Sarasa SemiBold 源以 CSS 500 角色加载；
- Overlay native-control audit：官方产品页面不再使用 native `<select>` 或 `datetime-local`；
- Compact inline alignment：Time Range / Route / badge / chip / network token / status / legend，以及 Causal Path / Accounting Events 的 primary-first-line marker 与 first-baseline 规则已在 Light / Dark、EN / 中文浏览器 fallback 中核验；
- Focused interaction/contextual token implementation：PASS；Inspector surface implementation 与 final surface visual selection（Warm Paper）：PASS；
- Bundled font licensing：`LICENSES.md` 与完整 `OFL-1.1.txt` 已纳入源码；Tauri bundle 显式映射到应用 `licenses/` resources；
- 当前分支无远端 CI status，不能把本地 PASS 表述为 GitHub CI PASS。

### Rendered visual QA

已人工确认：

- healthy fixture 下基本运行链路；
- 7d 切换后 History 正常出现；
- Connection Inspector 可完整打开、关闭，无已知 console render error；
- Custom 编辑器打开不会静默改变 Applied Range。

本轮 focused polish 已通过本地健康 synthetic fixture 的真实浏览器 fallback 验证：

- Light / Dark：Overview、History、Coverage、History Inspector 基本渲染；
- EN / 中文：导航、标题、筛选、日期控件、分页、状态、空/错误/加载文案与日期格式同步；
- Custom date/time：日期选择、24 小时输入、非法时间提示、Apply 前后状态、日期 grid 方向键/跨月移动与 Tab 离开关闭；
- Network / page size：统一 listbox 打开、选中、`aria-activedescendant` 更新、Tab 离开关闭与值更新；
- 824px 保底窗口无页面级横向溢出，1280px 桌面窗口完成布局几何检查。
- 本轮正式产物字体渲染：EN / 中文标题与控件分别由 CDP Rendered Fonts 识别为 Manrope 与 Sarasa Gothic UI SC，技术时间证据为 JetBrains Mono；所有正式 face 加载状态为 `loaded`，Sarasa CSS 500 实际命中 SemiBold 资源。
- 本轮 Inspector final surface visual selection：Warm Paper；Light `#f8f7f4` / Dark `#191817`，因果 hollow marker 遮罩继续跟随 `--pl-inspector`。
- 本轮 1280×800 与 1600×1000 的 Overview / History / Coverage、EN / 中文、Light / Dark 共 24 个截图无页面级横向溢出；EN / 中文共用 body 1.45、heading 1.35、caption 1.45、helper 1.52、mono 1.40 与标题副标题 6px 节奏，未保留 locale-specific structural typography。
- 本轮 primary-first-line alignment correction：不是 1px polish；DatePicker、network/page-size listbox、segmented controls、RouteBadge / EvidenceChip / Network token / StatusIndicator / Coverage legend、Causal Path 与 Accounting Events 均通过实际渲染截图与首行关系检查；marker 不再由 key/timestamp 或整块内容决定，未新增 locale-specific 或 font-specific offset。
- 本轮 marker closure：Causal Path 的 Process / Destination secondary path/IP 不影响 marker，Rule / Top policy / Proxy chain / Egress 保持单一 primary；Accounting Events 的规则、代理、流量与 evidence chip 独立下沉；连接线按 primary 首行 marker center 分段，hollow marker 的 surface fill 遮蔽连接线。

仍待人工验收：

- `healthy / gaps / stale / empty / scaled` 全 fixture；
- 完整 Tauri 运行时下的 1280×800 / 1600×1000；
- Light / Dark 两套主题在 History + Inspector、Overview、Coverage 上的完整视觉一致性；
- 本轮仍未用完整 Tauri 多 fixture 取代浏览器 fallback；因此不把 focused QA 表述为最终视觉 Freeze。

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
- 安装版长期数据路径规约（后续安装打包阶段确定）。

---

## Known Issues / Non-blocking Notes

- 交互模型收口已完成：Overview / Coverage 使用动态推进的 Live Analysis Range，History 使用确定性冻结快照，已解决快照提前生成与展示范围语义漂移问题。
- UI Design System 仍为 Draft，不应在最终视觉验收前标记 Frozen。

---

## Next Step

1. 对 C 分支做真实 Tauri 多状态、多尺寸视觉验收：`healthy / gaps / stale / empty / scaled`，至少覆盖 1280×800 与 1600×1000，并检查 Light / Dark。
2. 根据真实视觉问题做最小、系统性的 token / density / typography / pattern 调整；若 Design System 规则改变，同步 `docs/design/`。
3. 视觉验收通过后，决定是否 Freeze Design System v1，并规划 C 线进入正式主线的方式。
4. 后续功能开发按 `ROADMAP.md` 未完成项继续，不重新实现已经完成的 Phase 3 UI 基础能力。
