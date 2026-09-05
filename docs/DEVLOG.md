# ProxyLens Development Log

> 重要开发里程碑的简洁历史记录。不是每日流水账，也不替代 Git commit history。
> 当前状态与下一步请看 `STATUS.md`；长期计划请看 `ROADMAP.md`。

---

## 2026-09-05 — Phase 3E-2B2B Settings & Installed Product Polish

**Scope:** 在 3E-2B2A installed ownership 基线上完成最小 Settings utility、
secure runtime config apply、installed owner facts 与 mock-only product acceptance；
不引入 Test Connection、Mihomo 自动配置、tray、Service、MSI/updater 或 Phase 4。

**Completed:**

- 新增 sidebar secondary utility Settings dialog，支持 Controller URL、secure
  Controller Secret、Windows login autostart 与 Supervisor/Runtime/Task Scheduler
  事实状态；保持现有 Design System、Light/Dark 与 English / 中文 1:1 契约；
- 新增严格 bounded stdin config apply，支持 Secret keep / replace / clear、验证、
  atomic non-secret config、secure-store/task rollback 与 saved-pending-restart；
- 保持 Controller/Secret effective-source precedence 可见且诚实；environment/process
  override 不伪装成 persisted setting；developer checkout 不得修改 production task；
- Controller/Secret 变化走 exact stop → owner rebootstrap，autostart-only 变化不停止
  当前采集；Query API 与历史 authority DB 在重启过程中保持 read-only 可读；
- 加固 E2E Task Scheduler wrapper，使随机 task action 重新建立临时 DB/config、
  random WinCred target 与 mock Controller 环境，不继承产品默认 DB/Controller。

**Validation state:**

- 90 UI tests / 19 suites、affected Go tests 与 go vet、Rust cargo fmt/test、
  TypeScript/Vite build、Windows NSIS build：PASS；
- mock-only installed product acceptance：PASS；随机 loopback mock、随机
  ProxyLens/Test UUID credential、随机 ProxyLens-Test UUID task、temporary DB；
  Secret A → Secret B、new Secret Runtime authentication、autostart false → true、
  UI-close owner survival 与 same-DB preservation 均通过，默认 exact cleanup 完成；
- 本阶段未启动、停止、重启或修改真实 FLClash/Mihomo/TUN/系统代理，也未连接
  真实 9090/7988；本地 PASS 不表述为 GitHub CI PASS。

---

## 2026-09-04 — Phase 3E-2B2A Installed Runtime Ownership & NSIS Lifecycle

**Scope:** 在 3E-2B1 Supervisor / secure config 基线上完成 Windows V1 安装版运行时
ownership，不引入 Service、tray、Settings UI、MSI 或 updater。

**Completed:**

- 通过 non-elevated feasibility gate；安装版使用 current-user、interactive、limited-
  privilege Task Scheduler，固定生产 task 为 `\ProxyLens\Background Supervisor`，并配置
  LogonTrigger + 无限 `PT1M` repetition。E2E 只使用随机 `\ProxyLens-Test\<UUID>` task 与 harmless
  fixture，不枚举或触碰生产 task；
- 新增 exact-task `install` lifecycle CLI、per-authority-DB Supervisor presence probe、
  `Local\ProxyLens.Supervisor.Stop.v1.<sha256(normalized-db-path)>` graceful stop event，
  以及 v2 `runtime.json` 的 `autostartEnabled` / v1 migration；
- Tauri 安装版优先调用 Go lifecycle CLI 复用/ensure owner；无已注册 owner 的开发路径
  保留 direct Supervisor fallback。NSIS `currentUser` hooks 在 install/upgrade/uninstall
  前后执行 exact unregister、graceful stop 与 owner reconcile；upgrade 保留 DB/config/
  Controller/credential，uninstall 只清理程序与 ownership；
- 使用 isolated Tauri product identity 构建 Package A/B，完成实际 installed layout、fresh
  install → upgrade → disabled preference → uninstall acceptance；mock Controller、DB、
  config、WinCred target 与 task identity 全部隔离。

### Validation state

- Task Scheduler feasibility gate：PASS；未请求 elevation，测试后 exact random task 已清理；
- `go test` affected packages、`go vet`、Rust `cargo fmt --check` / `cargo test`、UI test/build
  与 Windows NSIS build：PASS；
- `node tools/runtime/run-phase3e2b2a-task-owner.mjs`：PASS；harness 确认
  `IsElevated=false`，E2E TimeTrigger 激活第一次运行，精确终止 PID A，下一次
  production-equivalent `PT1M` Task Scheduler repetition 拉起 PID B，且完成 exact cleanup；
- `node tools/runtime/run-phase3e2b2a-installed-lifecycle.mjs`：PASS；isolated Package A →
  Package B → uninstall、UI-close persistence、disabled autostart 与 data/credential
  preservation：PASS；
- 本阶段没有启动、停止、重启或修改真实 FLClash/Mihomo/TUN/系统代理，没有连接真实
  `127.0.0.1:9090` 或 `127.0.0.1:7988`；本地 PASS 不表述为 GitHub CI PASS。

---

## 2026-09-04 — Phase 3E-2B1 Supervisor & Secure Runtime Configuration

**Scope:** 在 3E-2A Runtime ownership / ensure-start 基线上完成 3E-2B1：新增独立
`proxylens-supervisor`，接管 per-authority-DB 的 Runtime process continuity、已有
Runtime observation、bounded crash restart 与 exact `STOP\n` graceful lifecycle；新增
非敏感 `runtime.json`、Windows Credential Manager Secret storage、Tauri ensure
Supervisor，以及 Query-only UI lifecycle。

**Completed:**

- Supervisor 与 Runtime 使用独立但同一 DB identity 的 Windows named mutex；presence
  probe 不保留 ownership，race 仍由 Runtime writer mutex 最终裁决；
- Controller URL 采用 `--controller` → environment → persisted config → product
  default（E2E 只允许显式随机 `127.0.0.1` mock），Secret 采用 `MIHOMO_SECRET` →
  Credential Manager → empty；Secret 不写 JSON、argv、handshake 或日志；
- Tauri 改为 detached Supervisor bootstrap，UI close 仅停止 Query，Supervisor/Runtime
  保活并支持 reopen reuse；build/bundle 纳入三个 Go binary；
- 未实现 2B2 的 login/autostart、Windows Service、tray、installer/upgrade/uninstall
  lifecycle、最终 Settings UI 或真实 FLClash/Mihomo acceptance。

### Validation state

- `go test ./pkg/runtime/... ./pkg/runtimeconfig/...`：PASS；
- `go test ./test -run '^TestSupervisorSubprocessUsesMockControllerAndSecureCredential$' -count=1 -v`：PASS；
- `cargo fmt --check --manifest-path src-tauri\Cargo.toml` 与
  `cargo test --manifest-path src-tauri\Cargo.toml`：PASS；
- `npm.cmd test`、`npm.cmd run build`、`npm.cmd run sidecar:build` 与
  `npm.cmd run tauri:build`：PASS；
- `node tools/runtime/run-phase3e2b1-supervisor.mjs`：PASS，覆盖 secure credential、
  Tauri A/B、UI-close survival、Runtime crash/restart、duplicate/different DB、
  existing Runtime takeover 与 exact cleanup；所有 Controller 连接均为随机端口 mock，
  DB/config 均为临时目录。

---

## 2026-09-04 — Phase 3E-2A Windows Runtime Ownership & Tauri Ensure-Start (historical baseline)

**Scope:** 在 Phase 3E-1 Runtime Core 之上完成按 authority DB path 的 Windows Runtime ownership、startup handshake 与 Tauri ensure-start；Query API 仍由 UI 独立持有，UI close 不停止 Runtime。

**Closure:** 当时使用随机 loopback mock Controller 与临时数据目录完成 first launch → `Started`、UI close 后 Runtime 保活、同 DB duplicate → `AlreadyRunning`、reopen reuse、different-DB concurrency 及 GET-only Controller lifecycle smoke。随后 3E-2B1 已将 Tauri 的直接 Runtime ensure-start 扩展为 Supervisor ensure/observe/restart 与 secure config；登录/自启动、安装版 ownership 与真实 FLClash/Mihomo 验证仍延期到 3E-2B2/Deferred。

---

## 2026-09-04 — Phase 3E-1 Desktop Runtime Core

**Scope:** 完成 Desktop Runtime Core 的 Go 侧运行时整合与桌面交付契约：抽取可复用 live Collector runner，新增 standalone `proxylens-runtime` 与 30s scheduled Accounting，确定 Windows canonical local data path，并让 Tauri bundle 同时包含 Query API 与 Runtime binary。Runtime 与 Query API 共享 authority DB path，但 Tauri 本阶段仍不自动管理 Runtime lifecycle。

### Completed

- **Collector reuse**：`collector run` 保留原有 flags、signal/stdin STOP、validation sink 与 summary，业务 loop 改由无 `os.Exit`/全局 signal 的 `pkg/runtime` runner 承担；
- **Scheduled Accounting**：实现 no-events / skip-if-fresh、non-reentrant、failure non-fatal、retry 与 cancellation 语义；
- **Runtime composition**：mock controller + temporary DB E2E 验证采集、Accounting freshness、read-only query、DB reopen 与 clean session closure；
- **Desktop contract**：正式路径为 `%LOCALAPPDATA%\ProxyLens\data\proxylens.db`，env override precedence 固定；Tauri resolver 保持只读 `DB_NOT_READY` 语义，bundle 同时构建两个 Go binary；
- **验证边界**：本阶段正式验收不使用真实 FLClash/Mihomo lifecycle、真实 Controller validation、Windows background supervisor 或 Phase 4。
- **验证卫生记录**：初次全量测试发现旧 crash smoke 曾使用 `127.0.0.1:9090`，该次只读连接结果明确排除出验收；测试随后改用 mock controller。最终 safety guard 递归扫描整个 collector test tree，拒绝真实 Controller endpoint，并要求 subprocess `run` 显式提供 `--controller`；无真实 FLClash/Mihomo 生命周期或配置写操作。

---

## 2026-09-04 — Final targeted cleanup: History inspector retention and Overview route focus state isolation

**Scope:** 针对性解决两个核心交互状态边界：删除 HistoryPage 冗余 mount effect，严格由 AuditContext 的 `setPage` 权威负责翻页重置，确保用户在 History 选中连接后切往 Overview/Coverage 再原样返回时，selected connection 与 Inspector 能够完整保留；收敛 Overview 页面级 Loading / Error 由 `allSummaryQ` 单一权威决定，Route Focus 切换不再导致整页骨架屏闪烁或错误覆盖，`scopedSummaryQ` 仅局部控制 Missing Attribution 与 Ambiguous Relay，数据未就绪时返回 `null` 并在 UI 呈现局部 loading/unavailable，杜绝假 0 回退。

### Completed

- **History 选区保留契约恢复**：移除 `HistoryPage.tsx` 中每次重新 mount 都会执行的 `useEffect(() => setSelected(null), [page])`，由 `AuditContext` 的 `setPage(p)` 统一权威负责分页时的选区清空；用户切出切回 History 时，只要调查上下文未变，选中连接与 Inspector 完美保留；
- **Overview 页面级状态与 Scoped 状态解耦**：页面级 Empty / Loading / Error 仅由 `allSummaryQ` 控制；Route Focus 切换时全局事实（Traffic Summary、Coverage、Sampling Residual、Gap Physical Traffic、Unknown Route）稳定维持，不发生整页骨架屏闪烁；
- **证据字段防假 0 与局部过渡**：`deriveOverviewEvidence` 在 `scopedSummary` 为 `undefined` 时返回 `null`（而不是回退为 0 B）；`OverviewPage` 中对 Missing Attribution 和 Ambiguous Relay 提供局部 `…` 加载与 `common.notAvailable` 错误状态，保留 RouteBadge 标识；
- **回归测试覆盖**：新增/更新 5 项针对性回归测试（覆盖翻页清选区、原样返回保留选区、新 Drill 清选区、Route Focus 局部状态与 null 安全、页面级与局部错误隔离），前端测试集达 85 项（18 个 Suite 全部本地 PASS），生产构建 0 错误。

---

## 2026-09-04 — Final closure cleanup: Coverage visual modifiers, selection decoupling, and history page reset

**Scope:** 完成 Interaction Closure 后的收尾清理。补齐 Coverage Legend 图例各 swatch modifier 与 mixed gap 双色条纹样式（严格沿用现有 `--pl-status-offline` 与 `--pl-status-gap` 语义色彩，彩色/灰阶/暗黑均清晰可辨），解耦 Gap Selection 与数据源 Provenance 视觉表现；修正 History 翻页时连接选择项同步清空、彻底消除跨页 Inspector 上下文残留；精确修正 System Status 核算纳入事件数与 Coverage Gap helper 文案；清理未使用的 6 组 i18n 键值（字典 390 键严格 1:1 对齐）与遗留 gapIndex/originalIndex 代码。

### Completed

- **Coverage 图例与时间线视觉补全**：在 `shell.css` 中补齐 `.pl-legend__swatch--covered`、`--controller`、`--collector`、`--mixed`、`--outside`、`--future`；在 `components.css` 中实现 `.pl-timeline__seg--mixed`，通过 `--pl-status-offline` 与 `--pl-status-gap` 双色条纹与纯 Controller / 纯 Collector 明确区分，避免复用 Collector 视觉表现；
- **Gap Selection 与 Provenance 解耦**：移除 `.pl-gap-row.pl-row--selected` 强制覆盖的 `--pl-status-gap` 左边框，统一使用 Design System 的 neutral/accent selection 边框与背景色；使 Hover、Focus、Selected 与 Provenance 徽标保持层级清晰；
- **History 翻页 Inspector 即时关闭**：在 `AuditContext` 的 `setPage` 与 `HistoryPage` 中增加选区重置保护，翻页时同步清空 selected connection，避免出现“新页内容 + 旧页详情面板”短暂错位；
- **系统状态与文案对齐**：将 `status.journalEvents` 修正为“核算纳入事件数 / Events in Run”；将 `coverage.gapsSub` 修正为“见上方汇总 / see summary above”；清理无用 advice 键值（中英各 390 项对齐）；
- **代码清理与测试扩充**：清理 `CoveragePage` 中遗留的 `gapIndex` / `originalIndex` 字段与无效 DOM identity，Timeline React key 使用稳定 segment/gap identity；新增 2 项精准回归测试，前端测试集达 80 项（18 个 Suite 100% 本地 PASS），生产构建 0 错误。

---

## 2026-09-03 — Final semantic closure: rangeKey guard and History query isolation

**Scope:** 彻底解决查询缓存语义边界：在 `keepLiveTickOnly` 中引入 `rangeKey` 与 `isLiveRangeKey` 语义约束，将 live tick 保留限定为滚动 live 窗口（today/7d/30d），彻底消除手动修改 Custom 范围被误判为 live tick 的漏洞；移除 `useConnectionsQuery` 的 `keepPreviousData`，确保用户更改过滤条件、路由或分页时，History 立即进入明确骨架屏加载状态，绝不呈现陈旧连接记录；补全针对性回归测试（测试集扩充至 78 项全部 PASS）。

### Completed

- **Time Range 语义身份校验**：在 `queries.ts` 中引入 `isLiveRangeKey` 并把 `rangeKey`（如 `today`, `7d`, `30d`, `custom:from..to`）注入分析查询；只有同一种滚动 live 范围且 `to >= prevTo` 时才允许保留旧数据；用户手动拉长 Custom 时间范围或切换范围种类，均立即触发显式 loading 过渡，与 Interaction Framework 规范 100% 对齐；
- **History 连接证据隔离**：移除 `useConnectionsQuery` 的 `placeholderData: keepPreviousData`；在过滤条件（host/process/destIp）、Route Focus、分页改变时，表格立即进入轻量骨架屏加载，彻底杜绝上一页或上一个过滤条件的数据在当前新条件下方短暂逗留冒充；
- **精准回归测试**：在 `queries.test.ts` 中增加对 Custom 手动拉长拦截、Today → Custom 切换拦截、Yesterday 拦截以及 History 查询 key 隔离的严格断言；前端单元测试扩充至 78 tests（18 个 Suite 全部通过）；
- **生产构建验证**：TypeScript 编译与 Vite 生产构建 0 错误。

---

## 2026-09-03 — Independent review precision fixes 2 (Semantic scope guard & stable gap identity)

**Scope:** 对 C 组前端第二轮独立审查提出的新问题实施精准修复。将 `keepPreviousData` 严格收敛为 `keepLiveTickOnly` 语义作用域防护函数（防止旧作用域证据冒充新作用域数据）；重构 Coverage Gap 选取为基于真实证据的稳定身份（`source(s) + startedAt + endedAt`），彻底杜绝 live 重新查询引发的选择漂移；修正 Inspector 的 `.pl-date-picker__popover` typo 与 `data-date-picker` 守卫；新增 10 项精准回归测试（测试集扩充至 73 项全部通过）；同步交互框架文档删除 sidebar anchored popover 误导文案。

### Completed

- **分析查询作用域守卫（`keepLiveTickOnly`）**：定义并应用语义作用域守卫函数。只有在 `from` 相同、`routeFocus` 相同且 `to >= prevTo` 的单调 live 刷新时保留上一轮数据；当用户切换 Route Focus（如 PROXY → DIRECT）或调整时间窗口时立即清空旧数据并触发显式 loading 过渡，彻底避免旧路由证据冒充新路由数据；
- **Coverage Gap 稳定证据身份（`gapIdentity`）**：定义 `gapIdentity(gap)` 为 `sorted(sources) + startedAt + endedAt`；Timeline 色块与 Gap 表格行改用 `gapId` 绑定与持久选中；增加 live 刷新掉出窗口时的安全清理逻辑，保证多次重查期间高亮绝对稳定；
- **Inspector DatePicker 让位修复**：修正 `.pl-date-picker__popover` typo，并在 DatePicker 根节点标记 `data-date-picker`，Inspector 键盘监听在任何日期弹层展开或交互时严格让位；
- **交互框架文档修正**：更新第 6 节明确 `keepLiveTickOnly` 作用域守卫语义；更新第 9 节 Gap 稳定身份选择规范；更新第 10 节将 System Status 真实描述为轻量安静的居中 Dialog，删除未实现的 sidebar-anchored 描述；
- **测试扩充与验证**：新增 `queries.test.ts`、扩充 `coverageSegments.test.ts` 与 `AuditContext.test.ts`，前端单元测试增至 73 tests（17 suites 100% PASS），TypeScript / Vite 编译 0 错误。

---

## 2026-09-03 — Independent review precision fixes and boundary restraint

**Scope:** 对 C 组前端交互收口进行独立审查后实施精准修复。修正快照冻结时机、消除 30s Live Range 刷新引发的骨架屏闪烁、收敛 Inspector 快捷键并增加复合组件冲突避让、实现 Coverage Gap 真实双向持久选中与语义化容器重构、收敛 System Status 视觉和内容（去除伪诊断与 Copy，补齐打开聚焦、Tab 循环与焦点返还全闭环）、将 App.tsx 生产探针受控于 E2E 模式、修正 Overview 副标题语义歧义并全面同步交互框架文档。

### Completed

- **快照冻结时机**：在 Overview / Coverage 切换时间范围不再预先生成快照（重置为待冻结），仅在真正切入 History 时刻冻结当前瞬时快照，或在 History 内修改时间范围时立即生效；
- **防闪烁平滑更新**：为 `useSummaryQuery`、`useTopProcessesQuery`、`useTopHostsQuery`、`useTopRulesQuery`、`useTopFinalProxiesQuery`、`useProtocolsQuery`、`useCoverageQuery` 配置 `placeholderData: keepPreviousData`，30s 低频时钟推进时在后台静默更新，绝不闪现 Skeleton；
- **Inspector 快捷键安全**：移除非标准 J/K/[,/] 快捷键，仅保留 ↑/↓/Esc；检测到页面存在打开的下拉菜单、日期选择器或弹窗时，或焦点处于复合交互控件时，严格让位不拦截；
- **Coverage Gap 双向持久选择**：重构 GapRow 与 Timeline 联动为持久选中与取消选中；Timeline 容器重构为 `role="region"`，色块赋予 `role="button"` 与 `aria-pressed`；明确区分 Hover、Focus 与 Selected 三态；
- **System Status 纯粹事实收敛**：去除 45% black backdrop 与 blur，换用轻量 Dialog 表面；移除预测性质的 Advice 与 Copy Diagnostics 按钮，彻底杜绝空 Callout bug；补全初始聚焦、Tab Focus Trap 与关闭返还焦点的完整闭环；
- **生产环境 Probe Gate**：调用 `get_e2e_mode`，仅在显式开启 `PROXYLENS_E2E_MODE=1` 时执行探针，普通用户正常运行零开销；
- **Overview 副标题语义**：将副标题更新为 `Routing outcome totals across all routes · reconciled accounted bytes`，清晰表明汇总卡片反映全局事实；
- **文档同步**：同步更新 `PROXYLENS_UI_PRODUCT_INTERACTION_FRAMEWORK_v1.md`，真实对齐 Live Analysis、History Snapshot、Future 分段与 Gap 选择交互规范；
- **测试覆盖**：UI 单元测试达 63 项（15 个 Suite 全部通过），Go 收集器测试与 Phase 0 回归通过；诚实维持 Design System 为 Draft，视觉验收待多 fixture 实测。

---

## 2026-09-03 — Frontend interaction, time model, and semantic completeness closure

**Scope:** 完整实施并收口 `PROXYLENS_C_FRONTEND_INTERACTION_CLOSURE_PLAN_2026-09-03.md` 全部 7 个 Stage。涵盖 Overview 语义基准（全局与分流作用域隔离）、双重时间模型（Live Analysis Range 随 30s 低频时钟推进 vs Frozen History Snapshot 稳定快照）、调查上下文无损保护与即时筛选、Coverage Future 灰色斜纹与条件摘要/图例/缺口流量估算、Inspector 上下条边界切换与 J/K 快捷键避让、Coverage Timeline 与 Gap 列表双向平滑滚动联动、以及系统状态渐进披露对话框与诊断复制。

### Root cause & Motivation

- Overview 顶栏卡片此前受 routeFocus 过滤，导致顶栏与全局流量构成脱节；
- 缺少 Live Analysis Range 与 Snapshot 分离机制，导致快照提前生成或实时推进逻辑混乱；
- 钻取到 History 时未清理无关历史筛选，造成无结果或混杂状态；
- Coverage 缺少对查询窗口超出当前时刻的 Future 状态表达，容易误导用户以为存在真实监控空洞；
- Connection Inspector 缺少就近浏览与单键快捷键，多连接审查效率偏低；
- Coverage Timeline 概览与下方 Gap List 明细缺乏双向高亮与平滑滚动导航；
- 侧边栏底部系统状态过于简略，缺少对会话心跳、核算滞后和排查建议的完整受控披露。

### Completed

- **Stage 1 (语义基准)**：分离 Overview 全局卡片（全量 route 查询）与分流策略卡片；隔离 Evidence 范围；Unknown Route 作为异常 chip 展示；
- **Stage 2 (时间模型)**：定义 `isLiveRangeKind`，Overview/Coverage 采用随 30s 低频时钟自增的 `resolvedRange`，History 采用独立冻结的 `snapshot`，关闭区间（yesterday/custom）严格不推进，`refreshHistory()` 显式刷新仅对 live 范围推进上限；
- **Stage 3 (调查上下文)**：Overview 钻取清空旧筛选并重置分页与选中，侧边栏切回 History 保留现有调查，Coverage 缺口检查带入 ±15min 区间并清空旧筛选；保持即时筛选零按钮摩擦；
- **Stage 4 (语义完整性)**：Coverage 增加中性 45° 斜纹 Future segment，仅在 `futureDurationMs > 0` 时展示摘要与图例；计算并展示窗口级 Gap 流量估算芯片；
- **Stage 5 (调查效率)**：Inspector 增加上一条/下一条操作与 J/K/[,/] 快捷键（输入框安全避让），表格行平滑滚动同步；Coverage Timeline 缺口块与 Gap 列表行双向 hover 与点击平滑滚动联动；
- **Stage 6 (渐进披露)**：底部状态栏升级为可交互弹窗触发器，点击呼出 System & Runtime Diagnostics 弹窗，完整披露 Collector、Accounting、DB Schema 元数据与排查建议，支持一键复制诊断数据；
- **Stage 7 (全面验证与收口)**：UI 单元测试从 42 增至 62 个（15 个 Suite 100% PASS）；Go 收集器测试通过；Phase 0 回归 18/18 通过；Tauri Release E2E 探针冒烟 100% PASS；100k 数据集查询基准 100% PASS；i18n 396 键严格 1:1 对齐。

### Validation state

- `npm test`：62 tests, 15 suites, 0 failures, 0 skips;
- `npm run build`：Vite production build clean in 1.16s;
- `go test ./pkg/... ./test/...`：all passed (0.2s ~ 9.9s);
- `node --test tools/discovery/test/*.test.mjs`：18 tests passed;
- `node tools/benchmark/run-phase3a-smoke.mjs`：100% PASS;
- `node tools/benchmark/run-pre-ui-query-sanity.mjs`：100% PASS.

---

## 2026-09-03 — Inspector surface selection and visual closure

**Scope:** 完成 Inspector 正式 surface 定色，并处理一项 History 与一项
Coverage 的低对比视觉收口；删除 DEV-only Surface Lab。不改变字体、marker
alignment、Sidebar、segmented controls、compact tag geometry、Overview、Inspector
layout 或 backend / Query API / Collector / Storage。

### Root cause

- Surface Lab 的人工比较已经完成，但正式 `--pl-inspector` 仍停留在 Baseline，
  实验组件、动态入口与 preset 数据也不应继续留在产品线；
- Light selected History row 的 neutral layer 与内部 route、evidence、network
  tags 的 tonal separation 不够明确；
- Coverage legend 的小尺寸、低对比和 transparent/pattern swatches 使用
  `--pl-border-muted`，边界在两套主题下不够稳定。

### Completed

- 正式 Inspector surface 选择 Warm Paper：Light `#f8f7f4`、Dark `#191817`，
  写入正式 `--pl-inspector`；Neutral Veil（`#f8f8f6` / `#171917`）与 Soft
  Greige（`#f6f6f2` / `#191a17`）作为人工筛选通过的设计备选保留在本历史记录，
  不增加永久 token；
- 删除 SurfaceLab component、CSS、`?surfacelab=1` 动态加载逻辑和全部临时
  preset 数据；DEV 与 production 均不再保留 Surface Lab；
- Light `--pl-row-selected` 调整为 `#dce0dc`，Dark `#292d29` 与 route、evidence、
  NetworkToken、左侧 2px selection accent 保持不变；
- Coverage `.pl-legend__swatch` 改用 `--pl-border-strong`，保持 swatch 尺寸、
  radius、stripe/fill/texture、label 与 spacing 不变；
- Design System、Token Reference、Components/Patterns、Implementation/QA、
  STATUS 已同步正式 surface、selected-row tonal separation、legend outline 规则；
  STATUS 不再将 Surface Lab 或 Inspector surface selection 作为待办，完整
  Tauri multi-fixture visual Freeze 仍保持 PENDING。

### Validation state

- `npm.cmd test`（42 项）与 `npm.cmd run build` 通过；production 产物不包含
  Surface Lab component、CSS、preset 文案/hex 或 `?surfacelab=1` 入口；
- Light / Dark History selected row、route/network/evidence tags、Coverage 四个
  legend swatch 边界与 History + Inspector 的 Warm Paper / marker mask 完成渲染
  回归检查；完整 Tauri multi-fixture visual Freeze 仍待后续验收。

---

## 2026-09-03 — Second-round Inspector surface exploration

**Scope:** 只扩展 DEV-only Inspector surface comparison，并同步当前视觉
Freeze 状态；不改变正式 `--pl-inspector`、既有 UI、字体、交互、布局、产品语义或
backend / Query API / Collector / Storage。

### Root cause

- 既有 Surface Lab 将候选平铺为单一列表，且主要是同一 neutral hue 的明度变化，
  无法有效比较用户更关注的 warm stone、greige、mushroom、cool porcelain、
  blue-gray fog 与 green-gray ash 等低 chroma 方向；
- STATUS 将 Inspector final surface selection 留为 PENDING，却仍把完整 Tauri
  视觉验收写成“唯一阻塞项”，未准确表达 Freeze 前的两个剩余事项。

### Completed

- Surface Lab 改为 Neutral、Warm、Earth / Gray、Cool 四个 family，每组 3 个，
  共 12 个 DEV-only 低 chroma 候选；保留 Baseline、Neutral Layered 与 Stone 方向的
  比较价值，并以更浅的 Light Stone（`#f8f6f3`）替代旧 Stone 候选；
- 所有候选仍只覆盖运行时 `--pl-inspector`，默认保持 Baseline，Light / Dark 使用
  同一 preset 的对应值，hollow marker mask 继续跟随 Inspector surface；
- 复核候选与 PROXY / DIRECT / REJECT、Fresh / Offline / Estimated / Missing 的
  语义区分；未发现需要因 collision 删除的候选；
- 将 Components / Patterns 与 Implementation / QA 的 Surface Lab 描述改为耐久的
  grouped low-chroma candidate 规则，不把实验 hex 列表写入正式 Token Reference；
- STATUS 改为列出 Inspector final surface human selection 与 Full Tauri
  multi-fixture visual acceptance 两项剩余事项。

### Validation state

- DEV Surface Lab 默认收起，按四个 family 展开比较 12 个候选；每个候选仍显示
  name、Light / Dark swatch 与对应 hex；
- 未修改正式 `--pl-inspector` Baseline、其它 palette、页面布局或上一轮 OFL /
  Tauri bundle resource 方案；`npm.cmd test`（42 项）与 `npm.cmd run build` 均通过。

---

## 2026-09-03 — Inspector surface candidate expansion and licensing closure

**Scope:** 只扩展 DEV-only Inspector surface comparison，修正文档状态并补齐
字体许可证发行资源；不改变正式 `--pl-inspector`、字体、交互、布局、产品语义或
backend / Query API / Collector / Storage。

### Root cause

- 既有 Surface Lab 只有 Baseline / Soft / Layered / Defined 四个同一 neutral hue
  方向的明度候选，无法比较 cool-gray、slate、sage-gray 与 warm-stone 等低饱和
  hue direction；
- 正式 `--pl-inspector` 仍有意保持 Baseline，Surface Lab 只是人工选择前的运行时
  预览，不应被当前状态文档写成最终 Closure；
- `LICENSES.md` 仅存在于源码字体目录，Vite 不会自动复制未被 import 的文本资源，
  且 Tauri bundle 尚未声明资源映射。

### Completed

- Surface Lab 保留 Baseline 并扩展为八个 DEV-only 候选：Neutral Soft、Neutral
  Layered、Cool Mist、Slate Mist、Sage Gray、Stone、Defined Neutral；所有候选均
  保留在实验工具中，正式 token 不增加 preset 变体；
- Surface Lab 继续使用一个 preset ID，Light / Dark 只切换该 preset 的对应值，
  默认仍为 Baseline；面板在小窗口下具备内部滚动保护；
- 加入完整标准 SIL Open Font License 1.1 正文 `OFL-1.1.txt`，并在 `LICENSES.md`
  说明许可文本及发行位置；Tauri bundle 将两份文本映射到 `licenses/` resources；
- 将 Inspector final surface visual selection 明确标记为 `PENDING HUMAN SELECTION`，
  保留 Design System 对“workspace-compatible neutral contextual surface”的长期规则，
  不把八个候选写入正式 Token Reference。

### Validation state

- 八个候选均通过 Light / Dark Surface Lab 运行时切换与 semantic-color collision 快速
  检查后保留；在人工选择前 production default 仍为 Baseline；
- `npm.cmd test`、`npm.cmd run build` 与 Tauri bundle resource inspection 通过；
- production bundle 不包含 Surface Lab UI、preset array、dev CSS 或 `?surfacelab=1`
  入口；完整 Tauri 多 fixture visual Freeze 以及最终 Inspector surface 选择仍 PENDING。

---

## 2026-09-03 — Final visual-system and production typography closure

**Scope:** 严格按 C 线最终视觉修整计划，在不改变产品结构、数据语义、Query API、
Collector 或 Storage 的前提下，完成正式字体落地、contextual surface 修整、compact
tag 几何统一与临时 Surface Lab；完整 Tauri 多状态视觉 Freeze 仍单独保留为后续验收门。

### Root causes

- 通用 `.pl-section + .pl-section` 选择器作用到 Overview 两列 rankings grid，导致第二列出现半截分隔线；
- History 选中行复用通用 selected surface，和 Route / Evidence 语义色在深色主题下形成低区分度；
- RouteBadge、EvidenceChip、NetworkToken 各自声明几何，导致 `unique` 等短标签出现视觉尺寸不一致；
- Inspector 复用 workspace surface，因果 hollow marker 也使用 raised surface，无法为 Inspector 做可控的中性层级预览；
- 正式 Narrative 仍使用旧字体，且旧 Typography Lab 与候选资产继续存在于开发入口。

### Completed

- 正式 Narrative 改为 script-aware、locale-independent 的 Manrope Latin + Sarasa Gothic UI SC Han/CJK，JetBrains Mono 保持 Technical / Evidence；仅保留 400/500 产品角色，Sarasa SemiBold 源映射为 CSS 500，并启用 `font-synthesis: none`；
- 删除旧 IBM Plex Sans SC 正式资产与完整 Typography Lab，增加可重复的 `build-production-fonts.py`，在 `LICENSES.md` 记录官方来源、版本、源哈希、OFL、转换命令与产物尺寸；
- 增加且仅增加六个 contextual alias：`--pl-inspector`、`--pl-row-selected`、`--pl-sidebar-hover`、`--pl-sidebar-selected`、`--pl-control-selected`、`--pl-control-selected-border`；
- rankings grid 仅使用 spacing 分隔，History selected row / Sidebar / Time Range / Route / Locale 各自使用正确的中性层级；RouteBadge / EvidenceChip / NetworkToken 统一为 18px / 7px / 2px / 10px 几何，语义色保持独立；
- 新增 DEV-only `?surfacelab=1`，默认收起，提供 Baseline / Soft / Layered / Defined 四档 Inspector Light/Dark 预设；marker mask 通过 `--pl-marker-mask-surface: var(--pl-inspector)` 跟随当前 Inspector surface。

### Validation state

- `npm.cmd test`：42 项通过；`npm.cmd run build`：通过；
- production bundle 不包含旧候选实验入口，也不包含 Surface Lab 动态代码；正式字体资源为 6 个本地 WOFF2；
- Light / Dark、EN / 中文与 1280×800 / 1600×1000 的 Overview、History + Inspector、Coverage 视觉回归继续以真实浏览器与 synthetic Query API fixture 验证；完整 Tauri 多状态 Freeze 仍 PENDING。

---

## 2026-08-31 — Focused CJK Font Lab candidate supplement

**Scope:** 在保持正式 Manrope + OPPO Sans 4.0 评估配对、脚本轴模型、默认值与既有 Design System 规则不变的前提下，补充 4 个 CJK 候选；不进入正式产品构建，不改变业务数据或技术证据呈现。

### Completed

- 从官方 Glow Sans v0.93 release 增加 Glow Sans SC Normal 与 Condensed，均使用真实 `Regular 400` / `Book 500` OTF face，字体文件按 SIL Open Font License 1.1 记录；
- 从官方 Sarasa Gothic v1.0.41 release 增加 Sarasa Gothic UI SC，使用 UI SC TTF 的真实 `Regular 400` / `SemiBold 600` face，诚实保留无 500 face 的事实，字体文件按 SIL Open Font License 1.1 记录；
- 从官方 Alibaba Fonts CDN 增加 Alibaba PuHuiTi 3.0，使用真实 `Regular 400` / `Medium 500` WOFF2 face；现有 setup downloader 仅增加可选 request headers 透传以满足官方 `Referer` 要求；官方站点的商用说明已记录，但 standalone redistribution / bundling 条款仍不够明确，因此保持 dev-only，不提交二进制；
- 新候选仅进入 CJK selector；Latin 默认仍为 Manrope，CJK 默认仍为 OPPO Sans 4.0，JetBrains Mono 继续固定用于 Technical / Evidence；未修改 marker alignment、layout 或正式 Design System。

### Validation state

- `npm.cmd run fontlab:setup`：14 个 local candidates 准备完成，新增字体文件均留在 ignored `ui/.font-lab/`；
- Playwright CLI：4 个新候选均可选择，`document.fonts` status 为 `loaded`，Han glyph probe 通过，实际 faces 分别为 `400 / 500`、`400 / 500`、`400 / 600`、`400 / 500`，`font-synthesis` 为 `none`；Manrope Latin 与 JetBrains Mono technical family 保持不变；
- Alt+Down / Alt+Up 从 OPPO Sans 4.0 往返经过 4 个新增候选；EN / 中文、Light / Dark 与 390×844 / 1280×800 viewport 均能渲染；
- `npm.cmd test`：42 项通过；`npm.cmd run build`：通过；production `dist` 不包含新候选、Font Lab manifest 或 `.font-lab` 资源。

---

## 2026-08-31 — Script-aware narrative pairing and primary-first-line marker alignment

**Scope:** 在既有 Directed UI 与正式 IBM Plex Sans SC / JetBrains Mono 基线之上，
完成一轮 typography script pairing，并校正 Causal Path / Accounting Events / Status
的 marker 对齐模型；不改变产品语义、布局方向、Query API、Collector 或 Storage。
本轮 marker 工作是 alignment model correction，不是 1px polish。

### Root causes

- Font Lab 仍按 UI locale 切换整套 Narrative family，English / Chinese selector
  与 locale 绑定，无法真实评估 Latin 与 CJK 的独立脚本配对；OPPO Sans 4.0
  manifest 还把变量字体错误声明成单一 400 face；
- Causal Path 与 Accounting Events 的 marker 被放在次级 key/timestamp 行盒中，
  因而没有稳定地跟随真正代表证据的 primary 首行；secondary path/IP、wrapped
  evidence 与 changed chip 会让旧模型产生错误的视觉锚点。
- Status、Causal Path 与 Accounting Events 需要明确区分 primary 内容和 secondary
  内容，避免用整块高度或次级标签推导 marker 位置。

### Completed

- Narrative token 改为 Latin / CJK 两个逻辑轴与一个 composed family；正式产品
  默认两轴均为 IBM Plex Sans SC，DEV Font Lab 默认用 Manrope Latin + OPPO Sans
  4.0 CJK，JetBrains Mono 保持 Technical / Evidence 固定；locale 不再驱动字体；
- Font Lab 改为两个始终同时生效的脚本 selector，Alt+↑ / Alt+↓ 只循环当前
  focused axis；状态通过实际字体加载与代表性 Latin/CJK glyph 渲染检查，卸载/失败
  候选不进入 cycle；
- 依据官方 OPPO 4.0 包 metadata 修正变量范围为 `100–700`，明确 Regular 400、
  Medium 500 为同一变量轴命名实例；正式 release packaging 仍 pending；
- Status 保持 `marker + primary label`；Causal Path 改为首行
  `marker | key | primary value`，path/IP 进入 value 列下方的 secondary；Rule、
  Top policy、Proxy chain、Egress 保持单一 primary；
- Accounting Events 改为首行 `marker | timestamp | primary event content`，
  rule/proxy/bytes/evidence chip 独立下沉；marker 与 connector 只依据 primary
  首行的结构性行高，不被 secondary 或整块 multiline 高度移动；
- Design System、token reference、component pattern、implementation QA、
  `STATUS.md` 与本日志同步为 primary-first-line 规则；保留无 `text-box` 的
  fallback，不增加 locale/font/string-specific offset，也不改变字体冻结与 OPPO
  license 状态。

### Validation state

- `npm.cmd test`：42 项通过；`npm.cmd run build`：通过；
- `npm.cmd run fontlab:setup`：10 个 local candidates 准备完成；
- Playwright CLI + CDP Rendered Fonts：混合样本分别识别 Manrope / OPPO Sans 4.0，
  technical sample 识别 JetBrains Mono；Light / Dark、EN / 中文切换保持脚本配对，
  `font-synthesis` 为 `none`；
- 实际截图与布局测量：Status marker 与 primary label 对齐；Causal marker 与
  primary value 首行对齐而不跟随 key；Accounting Event marker 与 primary event
  首行对齐而不跟随 timestamp；secondary path/IP、wrapped evidence 与 changed chip
  不改变 marker 的首行位置；
- 完整 Tauri 多 fixture visual Freeze 仍 PENDING；OPPO 生产打包仍需单独 license
  review。

---

## 2026-08-29 — Phase 3B/C Directed UI Engineering Closure

**Branch:** `experiment/qwen38max-directed-ui`  
**Raw C delivery:** `96b08cb`（保留不改写）  
**Closure status checkpoint:** `a1b4d2b`

### Completed

- Draft ProxyLens UI Design System v1：Light / Dark semantic tokens、Typography、Density、Interaction、Components & Product Patterns；
- 正式 App Shell：Overview / History / Coverage；
- Overview：Traffic Summary、Evidence Trust、Top Processes / Rules / Hosts / Final Proxies；
- History：受支持筛选、冻结 snapshot、显式 pagination、Connection selection；
- Connection Inspector：Causal Path、Accounting、Evidence Quality、Lifecycle、Accounting Event Timeline、Raw Traffic Frames；
- Coverage：Monitoring completeness、Gap timeline、Controller / Collector / Outside History semantics；
- System Status 与 Query API / backend heartbeat semantics 对齐。

### Independent-review Closure

在保留 `96b08cb` 原始交付的前提下完成：

1. `qualityFlags` boolean-map / string-list wire-shape normalization，修复 Inspector 白屏根因；
2. 增加 ErrorBoundary，避免单个 render exception 造成空白窗口；
3. Time Range → History snapshot / page / selected 的状态同步；
4. Route Focus → page / selected reset；
5. Custom editor draft 与 applied range 分离；
6. Coverage provenance：仅 `controller_stream` 为 Controller gap，其余 collector-side source 为 Collector offline，并支持 mixed；
7. Sidebar heartbeat threshold 对齐后端 `max(3 × heartbeatIntervalMs, 15000ms)`；
8. Overview route totals 文案从 `exact reconciled bytes` 改为 `reconciled accounted bytes`；
9. History row 增加 Tab + Enter / Space selection；
10. 新增 25 个回归测试，前端总计 32 tests，并加入正式 `npm test` script。

### Validation state

- Engineering / semantics：PASS；
- Architecture boundary：PASS；
- C raw delivery preservation：PASS；
- Remote GitHub CI：未配置/无 status，不把本地测试描述成 CI；
- Real rendered visual acceptance：PENDING。

### Remaining

- healthy / gaps / stale / empty / scaled 全 fixture 的真实 Tauri 视觉验收；
- 1280×800 / 1600×1000；
- Light / Dark；
- History + Inspector / Overview / Coverage；
- 视觉验收后决定 Design System v1 是否 Freeze；
- 决定 Public Sans / JetBrains Mono 是否作为确定性产品资产交付。

---

## 2026-08-29 — Focused UI polish and locale foundation

**Scope:** C 分支前端视觉一致性与 EN / 中文界面语言能力；不涉及 Query API、Collector 或 Storage。

### Completed

- 用受控组合式 date/time picker 替换 custom range 的 native `datetime-local` 控件；
- 用共享 listbox/menu primitive 统一 History network 与 page-size 选择器；
- 将菜单、日历、选择态、hover、focus、overlay shadow 对齐同一套 Design System token；
- 在 sidebar footer 的 Theme 同级 utility 区增加紧凑 EN / 中 segmented locale switch，并持久化到 `pl-locale`；
- 覆盖正式页面、Gate、ErrorBoundary 与开发诊断界面的可见 UI 文案，并让本地日期时间随 locale 呈现；
- 增加 locale、date/time utility 回归测试；本地测试总计 38 项，TypeScript / Vite build 通过。

### Validation state

- 健康 synthetic fixture 的本地真实浏览器 fallback：Light / Dark / EN / 中文、日期交互、network/page-size listbox、窄窗口无页面级横向溢出：PASS；
- 完整 Tauri 多状态（healthy / gaps / stale / empty / scaled）与最终 Design System Freeze：仍 PENDING。

---

## 2026-08-29 — UI polish accessibility closure and product sync

**Scope:** 针对独立审查反馈的最小前端 closure；不涉及 Query API、Collector 或 Storage。

### Root causes fixed

- Select option 与 DatePicker day cell 原先分别占用 Tab stop，且 overlay 只处理 pointer/Escape，焦点离开后不会关闭；
- `SelectMenu` 改为单一 focusable listbox + `aria-activedescendant`，DatePicker 改为 `grid → row → gridcell`、单一 roving day focus，并支持方向键跨月、Home/End 与 focus-out close；
- Connection Inspector 的缺失出口 fallback 改为 locale-aware UI copy，避免中文界面出现硬编码 `(unknown)`；
- locale dictionary parity 与静态 translation-key audit 固化进测试；
- `STATUS` 去除易漂移的远端 HEAD，PRODUCT / ROADMAP 补齐 V1 locale policy 与 Phase 3 keyboard/accessibility 状态。

### Validation state

- `npm.cmd test`：42 项通过；
- `npm.cmd run build`：TypeScript / Vite build 通过；
- 健康 synthetic fixture 浏览器 fallback：日期 grid 方向键/跨月、Tab 离开关闭、Select `aria-activedescendant`、Tab 离开关闭均通过；
- 完整 Tauri 多状态视觉验收仍 PENDING。

---

## 2026-08-30 — Deterministic typography delivery

**Scope:** C 分支字体实际渲染审计与 Design System typography consistency；不涉及布局结构、产品语义、Query API、Collector 或 Storage。

### Root cause

- 既有 CSS 只声明 Public Sans / JetBrains Mono 字体栈，没有 `@font-face` 或仓库字体资产；Windows Chromium 实际渲染为 `Segoe UI Semibold`、`Microsoft YaHei Bold` 与 `Cascadia Mono`，因此不同机器无法得到同一套 typography。

### Completed

- bundled Public Sans、IBM Plex Sans SC、JetBrains Mono 的 400 Regular / 500 Medium WOFF2；
- 增加 narrative / heading / body / control / caption / helper / technical typography roles；
- 通过 `html[lang="zh-CN"]` 集中设置中文字体、leading、标题字重、中文字距与标题副标题节奏；
- 将 diagnostics 页面残留的系统字体、硬编码颜色和 600 字重归入现有 token；
- 保持现有 control、navigation、history row、inspector、sidebar 与 toolbar 几何不变。

### Validation state

- `npm.cmd test`：42 项通过；`npm.cmd run build`：通过；
- 真实 Chromium CDP：EN 使用 `Public Sans Medium`，ZH 使用 `IBM Plex Sans SC Medium`，technical evidence 使用 `JetBrains Mono Regular`；
- 1280×800 与 1600×1000 下 Overview / History / Coverage 的 EN/ZH、Light/Dark 共 24 张截图无页面级横向溢出；日期日历、network/page-size listbox 交互回归通过；
- 构建产物字体总量为 8,049,984 bytes（约 8.05MB / 7.68MiB），未引入字体 npm 运行时依赖；完整 Tauri 多 fixture 视觉 Freeze 仍 PENDING。

---

## 2026-08-30 — Marker optical alignment and usable Font Lab closure

**Scope:** C 分支 Directed UI 的 inline marker optical alignment 与 DEV-only typography comparison；不涉及普通 typography、布局结构、产品语义、Query API、Collector 或 Storage。

### Root causes

- Status、Causal Path 与 Accounting Events 原先各自用不同的 dot primitive，marker 的布局位置受 baseline、整块 multiline 高度或默认 line box 影响；Causal connector 也按列表整体对称留白，首行/末行高度不同时末端会漂移；
- Font Lab 的候选只声明系统 family name，且重构后选择状态没有写回 `--pl-narrative-override`，因此浏览器虽能改变选择值，主界面仍未实际切换字体。

### Completed

- 以共享 optical marker primitive 统一 Status、Causal Path、Accounting Events：marker 进入首行 slot，使用单一相对 optical correction；Causal connector 按相邻 step 的共享 center anchor 分段，位于 marker 下方，hollow surface 遮罩连接线；
- 增加 DEV-only deterministic Font Lab manifest / setup：官方 local candidates 进入 `ui/.font-lab/`，加载失败明确标记 Unavailable，不可进入 cycle；
- 补上 EN / ZH 独立与 Link 模式下的 Alt+↑ / Alt+↓ quick-cycle，并将当前 locale 的选择实际应用到 narrative CSS override；production build 不包含 Font Lab UI、setup 或临时字体。

### Validation state

- `npm.cmd test`：42 项通过；`npm.cmd run build`：通过；
- `npm.cmd run fontlab:setup`：10 个 local candidates、全部 loaded；真实 Chromium `document.fonts` 与 computed family 验证 EN / ZH 切换和 Link cycle；
- Light / Dark、EN / 中文真实浏览器截图验证 Status、Causal Path（单行/多行）与 Accounting Events；完整 Tauri 多 fixture 视觉 Freeze 仍 PENDING。

## 2026-08-30 — Shared Narrative typography unification

**Scope:** C 分支已交付 typography asset 的最小修正；只统一 EN / 中文的
Narrative family 与 structural metrics，不涉及布局、颜色、产品语义、Query
API、Collector 或 Storage。

### Root cause

- EN 与中文原先分别使用 Public Sans 和 IBM Plex Sans SC；`html[lang="zh-CN"]`
  还额外覆盖了 leading、tracking 与标题副标题 gap；sidebar locale switch
  误用了 Mono，因此语言切换会带来不必要的字体与节奏差异。

### Completed

- EN / 中文普通 UI 统一使用 IBM Plex Sans SC；JetBrains Mono 继续只用于
  technical/evidence；
- 删除中文专属 typography root override，保留 400 / 500、body 1.45、heading
  1.35、caption 1.45、helper 1.52、mono 1.40 与 title/subtitle 6px；
- 移除 Public Sans `@font-face`、两份 WOFF2 与当前字体 license 索引条目；
- locale switch 改回 Narrative，未改变 control height 或其它 geometry；
- 未新增 `--pl-leading-control`：统一字体与 metrics 后 Time Range 已视觉居中。

### Validation state

- `npm.cmd test`：42 项通过；`npm.cmd run build`：通过；
- Chromium/CDP 实际渲染：EN / 中文标题与普通控件均为 `IBM Plex Sans SC Medium`，
  technical evidence 为 `JetBrains Mono Regular`；4 个 WOFF2 总计
  7,982,696 bytes；
- 1280×800、1600×1000 的 Overview / History / Coverage EN/ZH 几何复核：
  shell、nav、footer、page header、context bar、Time Range 的 top/height/bottom
  均为 0px 差异；Overview 1280 的 Evidence trust 仅因中文文案自然换行，1600
  已一致；完整 Tauri 多 fixture visual Freeze 仍 PENDING。

Design System 仍保持 Draft，未标记 Freeze。

---

## 2026-08-30 — Compact inline alignment and temporary typography lab

**Scope:** 在既有 Directed UI 与共享 Narrative typography 基线之上，完成一轮
compact control / baseline alignment polish，并增加仅开发态的临时字体对比工具；不改变
产品语义、Query API、Collector、Storage 或正式 Design System 的视觉方向。

### Root cause

- 字体 family 已统一，但 compact 控件仍依赖匿名文本节点与 `normal` line box；同一行的
  segmented、badge、chip、status、network token 与 legend 因此出现轻微 optical center 差异；
- Causal Path 与 Accounting Events 使用 marker 的固定 `top` 偏移，换行后 marker 不能稳定
  对齐首行；Network token 仍有独立的 inline-block padding 观感。

### Completed

- 新增 `--pl-leading-control: 1.20` 与 `.pl-compact-label`，统一 compact label 的 leading、
  optical center 与 `text-box` progressive enhancement；保留无 `text-box` 浏览器的 fallback，
  未增加控件高度；
- 固定高度控件使用 optical center，多列 / timeline 使用 first-baseline，因果链与事件 marker
  对齐内容首行；移除 Causal Path / Accounting Events 的 per-location `top` / padding offset；
- DatePicker、network/page-size listbox、segmented、badge / chip / token / status / legend
  落入同一套 compact typography 与 surface family；
- 增加 DEV-only `?fontlab=1` Typography Lab：EN / ZH 独立选字体、Link、Reset、加载本地字体，
  只覆盖 Narrative font-family；technical / evidence 始终保持 JetBrains Mono，候选字体不入库；
- 真实浏览器 fallback 下完成 Light / Dark、EN / 中文的 Overview / History / Inspector /
  Coverage 与 overlay focused QA；production build 未包含 Font Lab 面板或候选字体。

### Validation state

- `npm.cmd test`：42 项通过；`npm.cmd run build`：通过；
- 浏览器 CDP / screenshot：compact 控件 optical center、Causal Path / Accounting Events
  first-baseline、marker 首行、Coverage legend 与 Light / Dark / EN / 中文状态均通过；
- production preview `?fontlab=1`：面板数量为 0；`dist` 仅包含正式 IBM Plex Sans SC 与
  JetBrains Mono WOFF2；
- 完整 Tauri 多 fixture visual Freeze 仍 PENDING；Design System 仍保持 Draft，未标记 Freeze。

---

## Earlier milestones

更早的 Phase 0–2 与 Phase 3A 过程已有 `ROADMAP.md`、`STATUS.md` 历史版本、`docs/decisions/`、专项 handoff 与 Git commit 记录支撑。

从本文件创建起，只记录新的重要阶段、Closure、关键验证或 handoff checkpoint；不回填逐日开发流水账。
