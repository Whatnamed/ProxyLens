# ProxyLens 项目状态

> **用途：当前状态与 Agent handoff 的唯一快速入口。**
> 动态进度写在这里，不写进 `AGENTS.md`。长期阶段计划见 `ROADMAP.md`，重要历史里程碑见 `DEVLOG.md`。

---

## Current State

- **当前阶段**：Phase 3 Audit UI — C 组 Directed UI 已完成工程实现与独立审查 Closure；focused UI polish、EN / 中文 locale、overlay accessibility、确定性 typography asset 与 compact inline alignment Closure 已完成，仍等待完整真实多状态视觉验收。
- **活动分支**：`experiment/qwen38max-directed-ui`
- **当前代码线**：以当前 Git branch HEAD 与对应远端分支为准；本文件不固定容易漂移的 commit SHA。
- **C 组原始交付**：`96b08cb`，保留不改写，用于保留实验原始结果。
- **Closure**：`96b08cb` 之后的代码、测试、文档与 focused UI polish 修复均已保留；工程/语义 Gate 已通过。
- **当前唯一阻塞项**：完整真实 Tauri 多状态视觉验收，不是代码架构、产品语义或本轮 UI polish 实现本身。

### 已实现的正式 UI

- App Shell 与 V1 顶层导航：Overview / History / Coverage；
- Light + Dark semantic token / typography / density / interaction foundation；
- Overview：流量汇总、Evidence Trust、Top Processes / Rules / Hosts / Final Proxies；
- History：冻结 snapshot、受支持的过滤、显式分页、键盘可选连接行；
- Connection Inspector：Causal Path、Traffic Accounting、Evidence Quality、Lifecycle、Accounting Events、Raw Traffic Frames；
- Coverage：Coverage summary、Gap timeline、Controller / Collector provenance、Outside Monitored History、Inspect Around Gap；
- System Status：Collector / Accounting / DB 状态，heartbeat stale 规则与后端一致；
- ErrorBoundary 与 Query/API 可恢复状态。
- Design System family overlays：自定义 date/time picker、network/page-size listbox，共用 surface / border / radius / selected / hover / focus / shadow 语言；
- Overlay accessibility：Select 使用单一 listbox focus + `aria-activedescendant`；DatePicker 使用 `grid → row → gridcell` 与单一 roving day focus，支持方向键跨月移动和 focus-out close；
- Compact inline alignment：固定高度控件统一 optical center；多列、因果链和事件时间线让 key/timestamp 与 primary content 共用 first-baseline；Status 的 marker 对齐 primary label，Causal Path / Accounting Events 的 marker 对齐 primary 首行，secondary 内容独立下沉，连接线位于 marker 下方；不使用 optical-shift token，共用 `--pl-leading-control` 与 compact label/text-box progressive enhancement；
- 全局 UI locale：English / 中文即时切换、`localStorage` 持久化、`document.lang` 同步、locale-aware date/time formatting；原始技术证据值保持不翻译。
- 确定性 typography：正式产品继续 bundled IBM Plex Sans SC、JetBrains Mono 的 400/500 WOFF2；Narrative 以 Latin/CJK script axis 组合，正式默认两轴均为 IBM，系统字体只作最后 fallback，不要求系统安装；DEV-only Font Lab 可独立预览 Manrope Latin + OPPO Sans 4.0 CJK，不进入正式产物。

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
- TypeScript / frontend build：本轮本地 PASS；
- UI regression coverage：42 tests（本地执行结果），包含 locale dictionary parity、静态 translation-key audit 与 calendar navigation utilities；
- Locale dictionary parity：EN / 中文各 370 个 key，静态 UI translation keys 无缺失；
- Typography asset build：4 个 WOFF2、7,982,696 bytes（约 7.98MB / 7.61MiB，其中 IBM SC 约 7.80MB）；无 TTF/WOFF/italic 或额外字重；
- Overlay native-control audit：官方产品页面不再使用 native `<select>` 或 `datetime-local`；
- Compact inline alignment：Time Range / Route / badge / chip / network token / status / legend，以及 Causal Path / Accounting Events 的 primary-first-line marker 与 first-baseline 规则已在 Light / Dark、EN / 中文浏览器 fallback 中核验；
- Temporary Typography Lab：仅 DEV + `?fontlab=1` 动态加载；production build 不包含面板、候选字体或本地字体目录引用；
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
- 本轮正式产物字体渲染：EN / 中文标题与控件均由 CDP 识别为 `IBM Plex Sans SC Medium`，技术时间证据为 `JetBrains Mono Regular`；字体加载状态为 `loaded`。
- 本轮 DEV script-pair QA：`?fontlab=1` 同时应用 `Manrope` Latin 与 `OPPO Sans 4.0` CJK；CDP Rendered Fonts 在混合样本中分别识别两套 family，技术样本仍为 `JetBrains Mono`；OPPO 变量轴按官方包核验为 `100–700`，400/500 为命名实例；OPPO release packaging 仍 pending。
- 本轮 1280×800 与 1600×1000 的 Overview / History / Coverage、EN / 中文、Light / Dark 共 24 个截图无页面级横向溢出；EN / 中文共用 body 1.45、heading 1.35、caption 1.45、helper 1.52、mono 1.40 与标题副标题 6px 节奏，未保留 locale-specific structural typography。
- 本轮 primary-first-line alignment correction：不是 1px polish；DatePicker、network/page-size listbox、segmented controls、RouteBadge / EvidenceChip / Network token / StatusIndicator / Coverage legend、Causal Path 与 Accounting Events 均通过实际渲染截图与首行关系检查；marker 不再由 key/timestamp 或整块内容决定，未新增 locale-specific 或 font-specific offset。
- 本轮 marker closure：Causal Path 的 Process / Destination secondary path/IP 不影响 marker，Rule / Top policy / Proxy chain / Egress 保持单一 primary；Accounting Events 的规则、代理、流量与 evidence chip 独立下沉；连接线按 primary 首行 marker center 分段，hollow marker 的 surface fill 遮蔽连接线。
- Temporary Typography Lab：DEV URL `http://127.0.0.1:1420/?fontlab=1` 可独立选择 `Latin Narrative` / `CJK Narrative`、Reset 和加载本地 `.ttf/.otf/.woff/.woff2`；本轮已从官方来源实际准备 10 个 local candidates，浏览器 `document.fonts` 全部加载成功，Alt+↑ / Alt+↓ 只循环当前聚焦脚本轴并跳过 unavailable；production build 的 `?fontlab=1` 未显示面板，产物仅保留正式 IBM Plex Sans SC 与 JetBrains Mono 字体。

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

- `applyTimeRange()` 在 Overview/Coverage 应用 quick range 时会提前生成 History snapshot；这不会造成显示范围与查询范围不一致，但与最初“进入 History 才 freeze”的措辞存在轻微行为差异。当前视为产品行为选择，待真实使用后决定是否调整。
- UI Design System 仍为 Draft，不应在最终视觉验收前标记 Frozen。

---

## Next Step

1. 对 C 分支做真实 Tauri 多状态、多尺寸视觉验收：`healthy / gaps / stale / empty / scaled`，至少覆盖 1280×800 与 1600×1000，并检查 Light / Dark。
2. 根据真实视觉问题做最小、系统性的 token / density / typography / pattern 调整；若 Design System 规则改变，同步 `docs/design/`。
3. 视觉验收通过后，决定是否 Freeze Design System v1，并规划 C 线进入正式主线的方式。
4. 后续功能开发按 `ROADMAP.md` 未完成项继续，不重新实现已经完成的 Phase 3 UI 基础能力。
