# ProxyLens 项目状态

> **用途：当前状态与 Agent handoff 的唯一快速入口。**
> 动态进度写在这里，不写进 `AGENTS.md`。长期阶段计划见 `ROADMAP.md`，重要历史里程碑见 `DEVLOG.md`。

---

## Current State

- **当前阶段**：Phase 3E Desktop Runtime Integration — Phase 3E-1 Runtime Core、Phase 3E-2A Windows ownership / ensure-start、Phase 3E-2B1 Supervisor + secure runtime configuration 与 Phase 3E-2B2A installed lifecycle complete；Phase 3E-2B2B pending。Phase 3 UI 核心能力与交互收口已完成，Design System 继续保持 Draft；Final Full Tauri multi-fixture / real-data visual acceptance 仍 Deferred。
- **代码线**：以当前 checkout 的 Git HEAD 及其相对 `origin/main` 的关系为准；活动分支名和短期 SHA 不在此处硬编码。
- **C 组原始交付**：`96b08cb`，保留不改写，用于保留实验原始结果；远端 `origin/experiment/qwen38max-directed-ui` 保留作为选定 UI 实验方案快照。
- **Closure**：`96b08cb` 之后的代码、测试、文档、focused UI polish、Frontend Interaction Closure 与 Review Fixes 均已保留并合入 `main`；工程/语义 Gate 全部通过。
- **当前状态与待办**：
  1. Phase 3E-2B2B minimal Settings/autostart surface、installed startup/status polish 与 product acceptance。
  2. Final Full Tauri multi-fixture / real-data visual acceptance（Deferred：当前不以真实 FLClash/Mihomo lifecycle validation 作为验收路径）。
  3. UI Design System 继续保持 Draft。

### Phase 3E-1 Runtime Core (Complete)

- 已实现可复用 Go live Collector runner；`collector run` 保持为薄 CLI wrapper；
- 已实现 standalone `proxylens-runtime` 与 30s configurable scheduled Accounting，包含 skip-if-fresh、no-events skip、non-reentrant、failure non-fatal 与 graceful cancellation；
- 已确定 `%LOCALAPPDATA%\ProxyLens\data\proxylens.db`，并实现 `PROXYLENS_DB_PATH` → `PROXYLENS_DATA_DIR` → default precedence；Runtime 可创建 DB，Tauri/Query API 只读且 DB 缺失返回 `DB_NOT_READY`；
- 桌面 binary build/bundle contract 在 3E-1 阶段包含 `proxylens-query-api` 与 `proxylens-runtime`；后续桌面 bundle 已加入 `proxylens-supervisor`；当前 Tauri 先 ensure Supervisor，再由 Supervisor ensure Runtime，最后启动 Query API，且 UI close 不停止 Supervisor/Runtime；
- Runtime integration 使用 mock controller + temporary DB，未将真实 Mihomo 纳入验收路径。

### Phase 3E-2A Windows Runtime Ownership & Ensure-Start (Complete)

- Runtime 按归一化 authority DB path 使用 Windows crash-safe named-mutex ownership；同一 DB 的重复候选返回 `ALREADY_RUNNING`，不同 DB 可并行；
- `proxylens-runtime` 输出无 secret/token 的 `READY` / `ALREADY_RUNNING` JSON handshake；`MIHOMO_SECRET` 仅从 inherited environment 传到 `CollectorOptions.Secret`，不进入 argv、日志或 SQLite；
- Tauri 使用 bundled Supervisor ensure-start，Supervisor 使用同一 authority DB 的 Runtime mutex 观察/启动/重启 Runtime；先完成 Supervisor ownership handshake/DB 就绪，再解析 existing-only Query path 并启动只读 Query API；
- UI close 只清理 Query API；Supervisor 与其 Runtime/Collector 保持运行，重开 UI 通过 `AlreadyRunning` 复用既有 ownership；bootstrap status 对外仅报告 `Started`、`Starting`、`AlreadyRunning`、`Failed` 或 `NotAttempted` 事实；
- `proxylens-supervisor config` 提供 non-secret `runtime.json` 的 Controller URL 管理与 stdin-only Secret 管理；生产 Secret 仅写 Windows Credential Manager，E2E 只使用随机 `ProxyLens/Test/<UUID>` target，`MIHOMO_SECRET` 优先作为显式环境 override；
- Runtime whole-process / child crash 时由当前 Supervisor 按 bounded backoff 重启整个 Runtime；Supervisor 自身退出后的无 UI 自动恢复在本阶段曾属于后续 installed ownership，现由 Phase 3E-2B2A 的 Task Scheduler 提供；Supervisor 不读取 Mihomo、不写 SQLite 业务数据，Runtime 仍是唯一 writer authority；
- mock-only Windows lifecycle smoke 已验证 first launch、UI-close persistence、duplicate candidate、reopen reuse、different-DB concurrency 与 GET-only mock Controller。

### Phase 3E-2B1 Supervisor & Secure Runtime Configuration (Complete)

- 已实现独立 `proxylens-supervisor` executable：per-authority-DB Supervisor single-instance、Runtime mutex presence probe、已有 Runtime observation、exact child monitoring 与 bounded crash restart；
- Supervisor 取得 per-DB ownership 后先报告 `runtimeState=starting-retrying`；即使 Runtime 暂时启动失败，Tauri 也保留这个 exact Supervisor，并由同一 Supervisor 继续 bounded retry，已有 DB 仍可走 Query read-only fallback；
- 已实现 `STOP\n` lifecycle contract：仅 exact `STOP` 取消，stdin EOF/其它行忽略；Supervisor graceful stop 只停止自己拥有的 Runtime，外部观察到的 Runtime 不被停止；
- 已实现 `%LOCALAPPDATA%\ProxyLens\config\runtime.json` v2 non-secret config 与 `PROXYLENS_CONFIG_DIR` test/dev override，包含 v1 → v2 lossless migration、原子保存、schema/URL validation、autostart preference 与 DB path separation；
- 已实现 Windows Credential Manager Generic Credential：固定 production target、`CRED_PERSIST_LOCAL_MACHINE`、random E2E test target isolation；Secret 不进入 JSON、argv、handshake 或日志；
- Tauri 已从直接 Runtime ensure-start 切换为 detached Supervisor ensure-start；Query API 继续由 UI 独立持有，UI close 后 Supervisor/Runtime 保活，reopen 复用；build/bundle 同时包含 Query API、Runtime、Supervisor；
- `node tools/runtime/run-phase3e2b1-supervisor.mjs`：PASS，覆盖 secure credential、Tauri A/B、UI-close survival、Runtime exact crash/restart、duplicate/different DB、existing Runtime observation/takeover 与 exact cleanup；
- `go test ./test -run '^TestSupervisorSubprocessUsesMockControllerAndSecureCredential$' -count=1 -v`：PASS；Controller 为随机 `httptest` mock，DB/config 为临时目录，WinCred target 为随机 test target。

### Phase 3E-2B2A Installed Runtime Lifecycle (Complete)

- Windows V1 使用 current-user、interactive、limited-privilege Task Scheduler owner，固定生产任务为 `\ProxyLens\Background Supervisor`；测试任务只允许随机 `\ProxyLens-Test\<UUID>`，不枚举或触碰其他任务；
- 已实现 exact-task `install status/register/unregister/ensure-owner/run` 与 per-authority-DB `control status/stop`；Supervisor stop 使用 `Local\ProxyLens.Supervisor.Stop.v1.<sha256(normalized-db-path)>`，沿既有 graceful path 收尾；
- `runtime.json` v2 的 `autostartEnabled` 默认 true，v1 迁移 lossless；disabled preference 在 upgrade/reconcile 中保持 false，关闭偏好不停止当前 collection；
- Windows Tauri installed mode 优先让 Go lifecycle CLI 复用/ensure Task Scheduler owner；无已注册 owner 的 developer checkout 保留 direct Supervisor fallback；Query API 仍是 UI-owned read-only sidecar；
- canonical NSIS 配置为 `installMode=currentUser`。PREINSTALL/PREUNINSTALL 先 exact unregister + graceful stop，POSTINSTALL reconcile owner；upgrade 保留 DB、config、Controller URL、autostart preference 与 WinCred，uninstall 删除程序/task/process 但保留用户数据与 credential；
- 已通过非 elevated feasibility gate（随机 `\ProxyLens-Test\<UUID>` + harmless temporary executable）及 isolated Package A → Package B → uninstall acceptance；未使用 Service、tray、MSI 或 updater。

### 已实现的正式 UI

- App Shell 与 V1 顶层导航：Overview / History / Coverage；
- Light + Dark semantic token / typography / density / interaction foundation；
- Overview：流量汇总（全局出站物理汇总与分流作用域隔离）、Evidence Trust、Top Processes / Rules / Hosts / Final Proxies、Unknown Route 异常展示；
- History：双重时间模型（Live Analysis Range vs Frozen History Snapshot，30s 低频向前推进，关闭区间稳定，独立 `refreshHistory()` 刷新）、即时筛选保留、显式分页、键盘可选连接行；
- 调查上下文（Investigation Context）：Overview 钻取清空旧条件并重置选择、侧边栏切回保留现有调查、Coverage 缺口检查清空旧条件并带入 ±15min 区间；
- Connection Inspector：Causal Path、Traffic Accounting、Evidence Quality、Lifecycle、Accounting Events、Raw Traffic Frames；增加前一条/后一条边界受控切换（Prev/Next 与 ↑/↓/Esc 快捷键，严格让位复合交互控件，表格行自动滚动视口同步）；
- Coverage：Coverage summary、Gap timeline、Controller / Collector provenance、Outside Monitored History、Inspect Around Gap；Future 灰色斜纹占位与条件摘要/图例；Gap 物理流量估算芯片；Timeline ↔ Gap List 双向持久选中与点击平滑滚动联动；
- System Status：紧凑状态指示器，点击呼出轻量系统状态对话框，展示 Collector 会话与心跳、Accounting 引擎与 Freshness 落后、数据库状态与 Schema 版本，全闭环焦点管理；
- ErrorBoundary 与 Query/API 可恢复状态。
- Design System family overlays：自定义 date/time picker、network/page-size listbox、系统状态弹窗，共用 surface / border / radius / selected / hover / focus / shadow 语言；
- Overlay accessibility：Select 使用单一 listbox focus + `aria-activedescendant`；DatePicker 使用 `grid → row → gridcell` 与单一 roving day focus，支持方向键跨月移动和 focus-out close；Dialog 具备完整的打开聚焦、Tab 循环截获、Escape 监听与焦点返还；
- Compact inline alignment：固定高度控件统一 optical center；多列、因果链和事件时间线让 key/timestamp 与 primary content 共用 first-baseline；Status 的 marker 对齐 primary label，Causal Path / Accounting Events 的 marker 对齐 primary 首行，secondary 内容独立下沉，连接线位于 marker 下方；不使用 optical-shift token，共用 `--pl-leading-control` 与 compact label/text-box progressive enhancement；
- 全局 UI locale：English / 中文即时切换、`localStorage` 持久化、`document.lang` 同步、locale-aware date/time formatting；原始技术证据值保持不翻译；严格保持 1:1 键名对齐。
- 确定性 typography：正式产品 bundled Manrope 400/500、Sarasa Gothic UI SC Regular 与 SemiBold→CSS 500、JetBrains Mono 400/500 WOFF2；Narrative 以 Latin/CJK script axis 组合，locale 不改变字体或几何，系统字体只作最后 fallback；正式 Inspector surface 为 Warm Paper（Light `#f8f7f4` / Dark `#191817`）。

### 独立审查与交互收口修复（Review Fixes & Final Targeted Cleanup）

1. **快照冻结时机精准修复**：在 Overview / Coverage 调整时间范围仅更新 Live Analysis Range，不预先生成快照（旧快照置空）；仅在真正切入 History 时刻才冻结为快照，或在 History 内修改时间范围时立即冻结；
2. **分析查询作用域隔离（`keepLiveTickOnly` + `rangeKey` 强约束）与 History 证据隔离**：`keepLiveTickOnly` 引入真正的语义范围身份 `rangeKey` 与 `isLiveRangeKey` 校验，确保只有滚动 live 窗口（`today`/`7d`/`30d`）的自动单调 tick 才会保留数据，用户手动修改 Custom 范围或切换时间类型立即进入显式 loading 过渡；同时去除 `useConnectionsQuery` 的无条件 `keepPreviousData`，用户在 History 更改过滤条件、路由或分页时，立即进入轻量骨架屏加载状态，绝不让旧连接记录披着新条件/新页码标签暂存呈现；
3. **Coverage Gap 稳定证据身份（`gapIdentity`）**：Gap 选择由数组下标重构为基于真实证据字段（`source(s) + startedAt + endedAt`）的稳定 ID；若 live 刷新后 Gap 掉出窗口自动安全清理选择，彻底防止 live 重新查询导致的选择漂移；
4. **Inspector 快捷键让位与 Typo 修复**：移除非标准 J/K/[,/] Vim 快捷键，保留 `↑`/`↓`/`Esc`；修正 `.pl-date-picker__popover` typo，并在 DatePicker 根节点与 Inspector 中补齐 `data-date-picker` / `.pl-date-picker` 让位保护；
5. **Coverage Gap 双向持久选择**：重构 GapRow 与 Timeline Segment 为真实持久选中与取消选中逻辑，修正 Timeline 容器的 ARIA 结构（`role="region"`），分离 Hover / Focus / Selected 视觉状态；
6. **System Status 纯粹事实收敛与完整焦点闭环**：收敛全屏黑色 Backdrop/Blur 为轻量 Dialog，移除预测性排查建议与 Copy 按钮，修复空 Callout 状态组合，补齐打开自动聚焦、Tab Focus Trap 与关闭返还焦点至触发按钮的完整闭环；
7. **App.tsx 生产探针 Gate**：严格通过 `get_e2e_mode` 控制，仅在 `PROXYLENS_E2E_MODE=1` 时执行探针，普通用户正常运行不发起额外探针请求；
8. **Overview Traffic Summary 副标题语义对齐**：副标题统一为全量路由统计语义，避免与当前 `routeFocus` 混淆；
9. **交互框架文档同步**：`docs/PROXYLENS_UI_PRODUCT_INTERACTION_FRAMEWORK_v1.md` 全面同步双时间模型、`keepLiveTickOnly` 语义作用域防护、Future 分段、Gap 稳定身份选择规范，删除未实现的“anchored to sidebar footer”误导文案；
10. **高风险交互精准回归测试**：新增 `queries.test.ts` 专门测试 `keepLiveTickOnly` 与 History 作用域隔离；扩充 `coverageSegments.test.ts` 测试 `gapIdentity` 稳定性与防漂移；扩充 `AuditContext.test.ts` 测试快照冻结契约。前端测试达 85 项（18 个 Suite 全部 PASS）；
11. **视觉状态诚实声明**：维持 Design System 为 Draft，视觉验收待通过完整 Tauri 多 fixture 执行，不妄自宣称 100% 结束；
12. **最终收尾清理（Final Closure Cleanup）**：补齐 Coverage 图例各 swatch modifier 与 mixed gap 双色纹理样式（利用 `--pl-status-offline` 与 `--pl-status-gap` 双色条纹在彩色与灰阶下均可明确区分），解耦 Gap Selection 与数据源 Provenance 视觉表现（使用现有统一 neutral/accent selection）；修正 History 翻页时选中项立即置空、彻底消除 Inspector 跨页残留；准确表达 System Status 中源事件数为“核算纳入事件数”并更新 Coverage Gap helper 文案；清理无用 i18n keys（390 keys 严格 1:1 对齐）与遗留 gapIndex/originalIndex 代码；
13. **History 选区保留与 Overview 局部状态收敛**：删除 HistoryPage 冗余 mount effect，严格由 AuditContext 的 `setPage` 单一权威入口负责翻页选中重置，完整保障 History → Overview/Coverage → History 原样返回时保留选中行与 Inspector；Overview 页面级骨架屏与错误状态严格收敛由 `allSummaryQ` 单一权威决定，Route Focus 切换不再触发整页闪烁，`scopedSummary` 仅局部控制 Missing Attribution 与 Ambiguous Relay，加载时克制呈现局部状态并杜绝假 0 回退。

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
- UI regression coverage：85 tests（18 suites 本地执行结果），包含 `keepLiveTickOnly` 作用域守卫、History 查询证据隔离、History 翻页选区重置与返回保留、Coverage 混合缺口类型、Overview Route 局部加载与 null 防假 0、`gapIdentity` 稳定身份映射、快照生命周期转换、locale dictionary parity、静态 translation-key audit、calendar navigation utilities、time snapshot、drill reset、future segments 与 system diagnostics；
- Locale dictionary parity：EN / 中文各 390 个 key，静态 UI translation keys 严格 1:1 对齐无缺失；
- Typography asset build：6 个 WOFF2、17,066,080 bytes（约 17.07MB / 16.28MiB）；Manrope/Sarasa/JetBrains 均为本地正式资产，无 TTF/WOFF/italic 或额外字重；Sarasa SemiBold 源以 CSS 500 角色加载；
- Overlay native-control audit：官方产品页面不再使用 native `<select>` 或 `datetime-local`；
- Compact inline alignment：Time Range / Route / badge / chip / network token / status / legend，以及 Causal Path / Accounting Events 的 primary-first-line marker 与 first-baseline 规则已在 Light / Dark、EN / 中文浏览器 fallback 中核验；
- Focused interaction/contextual token implementation：PASS；Inspector surface implementation 与 final surface visual selection（Warm Paper）：PASS；
- Bundled font licensing：`LICENSES.md` 与完整 `OFL-1.1.txt` 已纳入源码；Tauri bundle 显式映射到应用 `licenses/` resources；
- 当前分支无远端 CI status，不能把本地 PASS 表述为 GitHub CI PASS。
- Phase 3E-1 Go runtime/scheduler mock E2E、Tauri path unit tests、双 binary build 与 Tauri release build 均已完成本地验证；Phase 3E-2A 与 3E-2B1 的 Go ownership/config/protocol/Supervisor tests、Rust parser/path tests、UI tests、三 binary build、Tauri release build、Go subprocess acceptance 与 mock-only Supervisor lifecycle smoke 也已完成本地验证；这些结果不是 GitHub CI PASS。
- Phase 3E-2B2A：`go test` affected packages、`go vet` affected packages、Rust `cargo fmt --check` / `cargo test`、UI/build 与 Windows NSIS build；`node tools/runtime/run-phase3e2b2a-task-owner.mjs` 与 `node tools/runtime/run-phase3e2b2a-installed-lifecycle.mjs` 均为 PASS，均使用随机 mock/temp identities；这些结果不是 GitHub CI PASS。
- 本任务正式 runtime 测试使用 `httptest` / mock WebSocket 与隔离临时 SQLite DB；真实 FLClash/Mihomo lifecycle 与 real-data validation 未纳入本阶段正式验收。
- `go test -race ./...` 未能启动：当前环境 `CGO_ENABLED=0` 且未发现 `gcc` / `clang` / `cl`，因此这是工具链限制，不是代码测试失败结论。
- 验证卫生记录：最初执行全量测试时，仓库旧版 crash smoke 曾将旧二进制指向 `127.0.0.1:9090` 并产生过一次只读 Controller 连接；该次结果不计入验收。随后测试已改为 mock controller；AST 守卫递归扫描整个 collector test tree，拒绝真实 Controller endpoint，并要求 subprocess `run` 显式提供 `--controller`；lifecycle tooling 另有随机 mock URL、temp-dir、E2E-only status file 与 exact-PID cleanup guard。3E-2B1 验收未启动或修改真实 FLClash/Mihomo，未发生真实网络生命周期或 Mihomo 写操作。

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
9. `proxylens-supervisor` 负责 per-DB process continuity，ensure/observe/restart `proxylens-runtime`；Runtime 负责 Collector + Scheduled Accounting，Tauri 只 ensure Supervisor 并管理 Query API 的关闭，正常 UI close 不停止 Supervisor/Runtime。
10. Windows canonical authority DB 为 `%LOCALAPPDATA%\ProxyLens\data\proxylens.db`，路径 override precedence 与读写边界以 ADR 0006 为准。
11. Gap / bootstrap / Accounting 的完整长期规则以 `ARCHITECTURE.md` 与 ADR 为准。

---

## Open Questions

- 最终视觉验收后，当前 Draft Design System v1 是否 Freeze；
- Phase 3E-2B2B 的 minimal Settings/autostart UI、installed startup/status polish 与最终 installed product acceptance；Windows Service、tray、MSI、updater 与真实 FLClash/Mihomo validation 仍不在当前范围。

---

## Known Issues / Non-blocking Notes

- 交互模型收口已完成：Overview / Coverage 使用动态推进的 Live Analysis Range，History 使用确定性冻结快照，已解决快照提前生成与展示范围语义漂移问题。
- UI Design System 仍为 Draft，不应在最终视觉验收前标记 Frozen。
- `proxylens-runtime` 本身仍是前台 executable，不自行 daemonize、注册服务或自启动；安装版登录常驻、Supervisor crash recovery 与 upgrade/uninstall ownership 已由 Phase 3E-2B2A 的 current-user Task Scheduler + NSIS lifecycle 提供，后续只剩 2B2B 的 Settings/status polish。

---

## Next Step

1. 进入 Phase 3E-2B2B，处理 minimal Settings/autostart surface、installed startup/status polish 与最终 installed product acceptance。
2. 待完整桌面 runtime 条件具备且用户明确安排真实环境后，执行 Deferred 的 Full Tauri multi-fixture / real-data 视觉验收（`healthy / gaps / stale / empty / scaled`，覆盖 1280×800 与 1600×1000，Light / Dark）。
3. 在完整桌面视觉验收前，UI Design System 继续保持 Draft；之后再按 `ROADMAP.md` 进入 Phase 4 Audit Intelligence。
