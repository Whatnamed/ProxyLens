# ProxyLens 项目状态

> **用途：当前状态与 Agent handoff 的唯一快速入口。**
> 动态进度写在这里，不写进 `AGENTS.md`。长期阶段计划见 `ROADMAP.md`，重要历史里程碑见 `DEVLOG.md`。

---

## Current State

- **当前阶段**：Phase 3S Production Storage & Accounting Scale Closure complete。此前 Phase 3E Desktop Runtime Integration、Phase 3E-1 Runtime Core、Phase 3E-2A Windows ownership / ensure-start、Phase 3E-2B1 Supervisor + secure runtime configuration、Phase 3E-2B2A installed lifecycle 与 Phase 3E-2B2B Settings / installed product polish complete。Phase 3 UI 核心能力、交互与 query-only Tauri multi-fixture visual acceptance 已完成，Design System v1 已 Frozen；真实 FLClash/Mihomo 与 real-data validation 仍 Deferred。
- **代码线**：以当前 checkout 的 Git HEAD 及其相对 `origin/main` 的关系为准；活动分支名和短期 SHA 不在此处硬编码。
- **C 组原始交付**：`96b08cb`，保留不改写，用于保留实验原始结果；远端 `origin/experiment/qwen38max-directed-ui` 保留作为选定 UI 实验方案快照。
- **Closure**：`96b08cb` 之后的代码、测试、文档、focused UI polish、Frontend Interaction Closure 与 Review Fixes 均已保留并合入 `main`；工程/语义 Gate 全部通过。
- **当前状态与待办**：
  1. 真实 FLClash/Mihomo lifecycle 与 real-data visual acceptance（Deferred：本阶段只使用 query-only synthetic fixtures）。
  2. Phase 4 Audit Intelligence 尚未开始。

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
- Runtime whole-process / child crash 时由当前 Supervisor 按 bounded backoff 重启整个 Runtime；安装版 Supervisor 自身 crash/退出由 Phase 3E-2B2A current-user Task Scheduler 的 LogonTrigger + 无限 `PT1M` repetition 在下一周期重新拉起，最坏约 1 分钟，期间按 Monitoring Gap 记录；Supervisor 不读取 Mihomo、不写 SQLite 业务数据，Runtime 仍是唯一 writer authority；
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

- Windows V1 使用 current-user、interactive、limited-privilege Task Scheduler owner，固定生产任务为 `\ProxyLens\Background Supervisor`；生产 LogonTrigger 使用无限 `PT1M` repetition 和 `MultipleInstances=IgnoreNew`，Supervisor crash/退出由下一周期恢复；测试任务只允许随机 `\ProxyLens-Test\<UUID>`，不枚举或触碰其他任务；
- 已实现 exact-task `install status/register/unregister/ensure-owner/run` 与 per-authority-DB `control status/stop`；Supervisor stop 使用 `Local\ProxyLens.Supervisor.Stop.v1.<sha256(normalized-db-path)>`，沿既有 graceful path 收尾；
- `runtime.json` v2 的 `autostartEnabled` 默认 true，v1 迁移 lossless；disabled preference 在 upgrade/reconcile 中保持 false，关闭偏好不停止当前 collection；
- Windows Tauri installed mode 优先让 Go lifecycle CLI 复用/ensure Task Scheduler owner；无已注册 owner 的 developer checkout 保留 direct Supervisor fallback；Query API 仍是 UI-owned read-only sidecar；
- canonical NSIS 配置为 `installMode=currentUser`。PREINSTALL/PREUNINSTALL 先 exact unregister + graceful stop，POSTINSTALL reconcile owner；upgrade 保留 DB、config、Controller URL、autostart preference 与 WinCred，uninstall 删除程序/task/process 但保留用户数据与 credential；
- 已通过非 elevated feasibility gate（随机 `\ProxyLens-Test\<UUID>` + harmless temporary executable）及 isolated Package A → Package B → uninstall acceptance；task-owner acceptance 使用 E2E TimeTrigger 激活同一无限 `PT1M` repetition contract，未使用 Service、tray、MSI 或 updater。

### Phase 3E-2B2B Settings & Installed Product Polish (Complete)

- 已新增 sidebar footer secondary utility Settings dialog；不新增 Settings 顶层导航，沿用现有 dialog、token、Light/Dark 与 English / 中文 1:1 语言契约；
- Settings 通过 Tauri commands 调用 bundled proxylens-supervisor，React 不直接访问 filesystem、Credential Manager、Task Scheduler 或 SQLite；Go config apply 使用 bounded、strict stdin JSON，支持 Secret keep / replace / clear，Secret 不进入 argv、runtime.json、日志或 SQLite；
- Controller URL、有效来源与 MIHOMO_SECRET / Credential Manager precedence 以安全 metadata 展示；environment/process override 不会被伪装为 persisted setting；Controller/Secret 变化走 exact stop → owner rebootstrap，autostart-only 变化只 reconcile exact task，不中断当前采集；
- installed layout gate 防止 developer checkout 修改生产 Task Scheduler task；persistence success / activation failure 显示 saved-pending-restart，Query API 与已有 authority DB 在重启期间保持 read-only 可读；
- mock-only installed product acceptance：random loopback mock Controller、random ProxyLens/Test/<UUID> WinCred、random \ProxyLens-Test\<UUID> task、temporary DB；覆盖 Secret A → Secret B、新 Secret Runtime 使用、autostart false → true、UI-close owner survival 与 DB preservation。

### Phase 3S — Production Storage & Accounting Scale Closure (Complete)

- 只读 root-cause measurement（EQP 验证、SHA256 前后一致、production 停止）确认真实生产库（~5.2GB / ~1.55M journal events）97.63% 的 `ConnectionDelta` raw 行为零增量重复证据（wall-clock 口径；该比例仅针对 ConnectionDelta 行，不是整体 journal 行数削减比例），且常规核算 tick 在 stale 时触发全历史重建；
- S1 Raw density：StateEngine 以共享 emission contract（steady-state 与 reconnect-recovery 同路径）抑制零字节 `ConnectionDelta` 的持久化，改为 `ConnectionPresenceCheckpoint` 稀疏在场证据（30s/active connection）；非零 delta、metadata/rule/chain/counter/relay/gap 证据语义不变；`ConnectionDisappeared` 携带精确 final presence；零删除历史 raw 行；
- S2 Incremental Accounting v2（migration 008 additive）：generation-based 增量核算，常规 tick 只处理 `(publishedBoundary, newBoundary]` frame-aligned 有界区间并在单事务内原子发布派生行与 boundary（失败/取消零残留、幂等）；full rebuild 仅限显式 seed/repair（`collector storage seed-v2`）；v2 未激活时 analytics/Query 回退 legacy；relay/dedup 分类、区间分配与行构造与 legacy 共享同一代码路径（`classifyConnectionGroup` / allocation helpers）；
- S3 WAL / failed-run hygiene：取代 `2bfd8c5` 每 tick TRUNCATE；稳态 PASSIVE checkpoint + 结构化 `WALCheckpointResult` 遥测；TRUNCATE 仅在 shutdown/maintenance 安全边界以 fresh non-canceled context 执行；Runtime shutdown 检查点不杀 busy reader；
- 重观测契约修复（soak 暴露的真实投影缺陷）：同一 epoch 内连接 ID 在 Disappeared 后重新被观测时，`ConnectionNew` 以 upsert 重开既有行（保留原始 `first_observed_at`，清除 disappearance/observation-end 事实，journal 双事件保留，`RebuildProjections` 重放确定性一致）；`ConnectionNew` 现在与 Bootstrap 一致绑定事件 baseline 计数器；
- S4 E: 盘生产规模验收（`collector/cmd/proxylens-scale-acceptance`，全部 PASS）：density 97.62% `ConnectionDelta` 零增量行削减（journal 总行数不按此比例削减）且字节和精确；真实生产库 E-copy 迁移+seed（61 chunks / 456.6s / WAL 峰值 8.5MB / DB +32MB / quick_check ok / authority 字节不变 / published 字节和与直接测量一致）；crash/cancel/publish-boundary/checkpoint 竞争/失败 generation 清理有界；常数成本 0.42s@50K vs 0.34s@1.57M（`INDEXED BY` 修正 planner 误选后无历史规模缩放）；30min 连续 soak（Collector+增量核算+只读 Query 负载）通过全部门限；
- 生产 C: 库短时 revalidation 完成：migration 008 打开即应用；runtime 对 1.55M 事件 journal 自动 seed（63 个 seed chunk 完成）并激活 v2 generation，随后对真实流量执行 21 个增量 chunk（零失败）；最终 published boundary == journal max（shutdown flush 后 **lag 0**）、accounted==raw 字节和（927,714,947 / 1,209,704,764）、`quick_check ok`、shutdown TRUNCATE 后 **WAL 0 字节**、DB +205MB（v2 派生行 + revalidation 窗口 raw 证据）；FLClash/FlClashCore PIDs 全程不变，只读 Query API 提供 v2 读取；结束后 collection 再次停止（autostart=false、无 Task owner），未恢复 24/7 常驻；
- ADR 0010 记录全部决策与证据（`docs/decisions/0010-production-scale-storage-and-incremental-accounting.md`）。

### Phase 3S — Correctness Closure（独立 review 11 blocker 收口，Complete）

- **B1 重观测续算**：StateEngine bounded disappeared-tombstone（FIFO 4096）：同 ID + 同 Mihomo Start 短暂漏帧后重现 → counter-difference continuation（accounted 只增差值，如 300/600→消失→350/650 仅 +50/+50，全链路真实 pipeline 回归）；不同 Start → 新 incarnation；raw journal 保留全部证据。
- **B2 lifecycle/temporal overlap**：v2 `lastObs` 统一契约（active → 当前 chunk authoritative boundary frame time；terminal → disappearance 时刻，同 chunk 显式 New 可清除 terminal marker）；boundary frame time 取自当前 session/epoch 最新 frame 证据（含 SamplingResidual-only frames）；A（00:00–00:05 消失）与 B（01:00–01:05）不再被误判 relay duplicate（seed 与 incremental 双路径回归）。
- **B3 高基数规模门**：新增 `cardinality` phase（1k vs 10k 历史 connection + 同批增量，同 session/epoch）：在修正 `loadConnStateClosure` key 缺陷后重新实测 —— 1k（1000 历史连接，1007 closure conns）与 10k（10000 历史连接，10007 closure conns）：writer-hold 从 **3.1ms** 仅微增到 **8.0ms**（远低于 2s 门限，且处于毫秒级固定开销底噪内）；prep off-lock 从 9.7ms 扩展至 315.4ms（读取完整闭包并完成决策）；lag=0；证明写事务耗时随基数扩展有界且极短，无需扩大设计。
- **B4 writer-lock 契约**：两阶段 chunk —— 区间读/分类/闭包构建在 prep（off-lock），writer tx 内 recheck published boundary 后做有界 mutation + 原子发布；boundary 被并发推进时返回真实 boundary（`skipped`），不产虚假 completed；`IncrementalChunkTelemetry` 固化证据；emit timeout 维持 30s。
- **B5 generation invariant**：migration 009 partial unique index 至多一个 live generation；并发 boundary race 修复 + invariant 回归。
- **B6/B7**：equivalence 测试 typo（DirectUpload↔DirectUpload）+ fixture 真实非零 DIRECT；shutdown flush 改为 30s 预算内有界 catch-up 循环直至 lag=0（或如实记录 incomplete）。
- **B8**：scale harness fail-closed 卷位守卫（默认仅 E:，`PROXYLENS_SCALE_ALLOWED_VOLUMES` 可加白但永不 C:；SQLite temp 指向 acceptance workspace）。
- **B9 容量证据（只读）**：reval 窗口（1.09h）journal +52,521 rows（48,295/h：Delta 27,646/h、SamplingResidual 13,132/h、PresenceCheckpoint 2,642/h、Disappeared 2,276/h、New 2,227/h、Bootstrap 244/h）；`connection_traffic` 29,868/h；SamplingResidual 占 27.3% 行数但 81.6% 零残差、物理占比 <2% → sparse 设计按证据暂缓；recurring 物理增长 ~150–190 MiB/h（剔除一次性 seed ≈+32MB）→ 24/7 约 3.6–4.6 GB/day，对默认 C: 路径 material → **新增 disk guard fail-safe**（30s 检查；floor=max(1GiB, 15% DB size)；breach → 显式 `CollectorHealth(disk_guard_floor_breached)` journal 证据 + clean stop；Runtime 同 floor 进入 write-quiescent 模式，停用 collector 与 accounting 写操作；永不删除/压缩）。density 文案全面修正为「`ConnectionDelta` 零增量行削减」，非整体 journal 行数削减。
- **B10 seed contract**：文档化 automatic background seed —— Runtime 无 active generation 即自动 seed（disk preflight ≥ max(1GiB, 15% DB size)、resumable chunk duty cycle、009 单活跃不变量、legacy Query fallback 全程保持；`collector storage seed-v2 [--force]` 为显式路径）。
- **B11 生产 generation 修复评估**：两份 production E: copy 受控 `seed-v2 --force`（supersede + reseed + migration 009）→ 修复前后历史结果**逐字节一致**（1,579,954 与当前生产边界 1,602,541 均验证：class 分布/relay relations/lifecycle 计数/published totals 完全一致、stale-active=0、quick_check ok、raw journal 未动）→ 生产 active generation 非 result-tainted，无需紧急 repair；受控 repair 程序记录于 ADR 0010 §2.9。

### Phase 3S — Post-Review Targeted Fixes（独立 review 7 项深层缺陷修复，Complete）

1. **loadConnStateClosure key 与 RowsAffected**：SQL 补齐 `session_id, epoch_id` 并复用 `collectConnStateRows`，根除 `:0` fake group；`applyClassChanges` 增加 `RowsAffected` 断言；新增 silent active A 与本 chunk 独有 dirty B 配对回归（A 无需本轮 dirty 即成功匹配）。
2. **stale relay_relations_v2 清理**：建立显式 bounded `relationRefreshKeys` 集合，先清旧 candidate relation 再插入新决策，不伤历史无关 candidate；新增 candidate X 在 MetadataUpdated 变 unique 后旧 relation 彻底清除且历史 accounted bytes 恢复回归。
3. **tombstone FIFO 实例感知淘汰**：FIFO entry 绑定具体 tombstone 实例，仅当 map 中仍为该实例时才删除；新连接/不同 Start 显式清理旧 tombstone；新增同 ID 多次 flap + 4200 次 churn 淘汰旧 FIFO entry 时最新 tombstone 完好且最后以 counter-diff 续算针对性单测。
4. **write-quiescent low-disk mode**：Pre-start DiskGuard 检查在 `OpenDB` 与 migration 之前执行，破底时不打开 writer DB、不跑 migration、不启动 collector 与 scheduler、shutdown 跳过 flush 循环与 truncate，日志明确 degraded/low-disk，保持 READY 提供只读 Query 服务；Mid-run 破底由 `CollectorResult.DiskGuardTripped` 传递，Runtime 停 scheduler 并跳过 shutdown flush/truncate；`seedPreflight` 与 `advanceIncrementalChunk` 统一对齐 `max(1GiB, 15% DB size)` 下限；新增 pre-start 阻止 pending migration 与 mid-run breach 抑制 shutdown 写入两个 targeted test。
5. **session progress 单调性保证与有序健康事件**：`SQLiteEventSink.Emit` 更新 session 进度使用 `MAX(COALESCE(last_frame_sequence, 0), ?)`；移除 Storage 的 0/0 游标继承黑魔法，DiskGuard health 必须走 `StateEngine.EmitSessionHealth` 正常有序发布，移除无序 sink fallback；断言 journal columns、unmarshaled `event_json`、recomputed EventID 与 SHA256 四者完全一致且无 collision。
6. **shutdown flush 状态如实报告**：提取 `runShutdownAccountingFlush`，仅当 `lagEvents == 0` 时报告 fresh，达到 64-cycle cap 或超时且 lag > 0 一律报告 `incomplete (lag=N)`；新增针对性单测。
7. **frame-complete publish boundary 与 captured-boundary race**：`extendCutToFrameEnd` 实施正式 invariant —— 任何 captured boundary 之后提交的事件绝不用作证明该 boundary 内 candidate frame 已完成；若真实帧尾超过 captured boundary，本轮必须回退到该帧之前，绝不截断发布半帧；`isFrameComplete` 所有校验均带 `journal_sequence <= boundary` 约束；StateEngine 确保所有 snapshot frame 结尾具备完成证据；新增真正的交织 barrier/hook race 回归测试。
8. **架构与语义同步**：`docs/ARCHITECTURE.md` 全面同步 v2 增量核算生产架构，澄清不同 Mihomo Start 为同一 durable connection key 下的新 byte-accounting incarnation。

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
 11. **视觉状态诚实声明**：在最终 Tauri acceptance 完成前维持 Design System 为 Draft；本轮完成后按实际证据 Freeze v1；
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
- UI regression coverage：90 tests（19 suites 本地执行结果），在既有交互回归之上增加 Settings draft、URL validation、Secret action 与 effective-source metadata tests；
- Locale dictionary parity：EN / 中文各 458 个 key，静态 UI translation keys 严格 1:1 对齐无缺失；
- Typography asset build：6 个 WOFF2、17,066,080 bytes（约 17.07MB / 16.28MiB）；Manrope/Sarasa/JetBrains 均为本地正式资产，无 TTF/WOFF/italic 或额外字重；Sarasa SemiBold 源以 CSS 500 角色加载；
- Overlay native-control audit：官方产品页面不再使用 native `<select>` 或 `datetime-local`；
- Compact inline alignment：Time Range / Route / badge / chip / network token / status / legend，以及 Causal Path / Accounting Events 的 primary-first-line marker 与 first-baseline 规则已在 Light / Dark、EN / 中文浏览器 fallback 中核验；
- Focused interaction/contextual token implementation：PASS；Inspector surface implementation 与 final surface visual selection（Warm Paper）：PASS；
- Bundled font licensing：`LICENSES.md` 与完整 `OFL-1.1.txt` 已纳入源码；Tauri bundle 显式映射到应用 `licenses/` resources；
- 当前分支无远端 CI status，不能把本地 PASS 表述为 GitHub CI PASS。
- Phase 3E-1 Go runtime/scheduler mock E2E、Tauri path unit tests、双 binary build 与 Tauri release build 均已完成本地验证；Phase 3E-2A 与 3E-2B1 的 Go ownership/config/protocol/Supervisor tests、Rust parser/path tests、UI tests、三 binary build、Tauri release build、Go subprocess acceptance 与 mock-only Supervisor lifecycle smoke 也已完成本地验证；这些结果不是 GitHub CI PASS。
- Phase 3E-2B2A：`go test` affected packages、`go vet` affected packages、Rust `cargo fmt --check` / `cargo test`、UI/build 与 Windows NSIS build；`node tools/runtime/run-phase3e2b2a-task-owner.mjs` 与 `node tools/runtime/run-phase3e2b2a-installed-lifecycle.mjs` 均为 PASS，均使用随机 mock/temp identities；这些结果不是 GitHub CI PASS。
- Phase 3E-2B2B：affected Go tests / `go vet`、Rust tests、90 UI tests、TypeScript/Vite build、Windows NSIS installed product build 与 `node tools/runtime/run-phase3e2b2b-settings-product.mjs` 均为 PASS；acceptance 使用随机 mock/temp identities，输出确认 Secret A → Secret B、autostart false → true、UI-close survival 与 DB preservation；这些结果不是 GitHub CI PASS。
- Phase 3S：E: 盘生产规模验收全部 PASS（density 97.62% `ConnectionDelta` 零增量行削减/字节精确；真实库 seed WAL 峰值 8.5MB、authority 字节不变；crash/cancel/bounded；常数成本 0.42s@50K vs 0.34s@1.57M；30min soak FinalLag=0、LegacyRuns=0、QueueOverload=0、MaxIncremental ~0.14s、WAL 峰值 ~5MB）；生产 C: 库短时 revalidation 完成后 collection 再次停止；Go 全量测试 + vet、UI 90 tests + build、cargo 19 tests、`git diff --check` 本地 PASS；这些结果不是 GitHub CI PASS。
- Phase 3S Correctness Closure & Review Fixes：`go vet ./...` + `go test ./pkg/... ./test/...` 全部 PASS（新增 closure key、stale relation 清理、tombstone FIFO 实例感知淘汰、write-quiescent low-disk 模式、session progress 单调性、shutdown 64-cycle 真实状态报告、frame-complete boundary 切分等回归）；E: cardinality gate 在修正 closure key 后重新实测（1k 3.1ms hold / 10k 8.0ms hold，lag=0，全部 PASS）；全部 90 UI tests 与 Rust 19 cargo tests 全部 PASS；这些结果不是 GitHub CI PASS。
- 本任务正式 runtime 测试使用 `httptest` / mock WebSocket 与隔离临时 SQLite DB；真实 FLClash/Mihomo lifecycle 与 real-data validation 未纳入本阶段正式验收。
- Phase 3 final Tauri visual acceptance：query-only 双门控路径、五套 synthetic fixture、1280×800 / 1440×900 / 1600×1000 与 maximized spot check 均完成；每次均确认 `owner=0 runtime=0 controller=0`、Query probe、source/copy SHA 不变。
- Final Tauri interaction matrix：healthy 的 Overview / History + selected Inspector / Coverage 在 EN / 中文与 Light / Dark 下完成实际 WebView2 交互；gaps 的 Inspect Around Gap → History、scaled 的 pagination/filter/Inspector/Coverage/theme 重绘均通过；无 P0/P1 blocker。
- Final Tauri targeted review closure：Visual QA gate 已抽为可单测的 `Disabled` / `Active` / `RefusedIncomplete` 纯状态；不完整请求 fail-closed 且不进入产品 bootstrap；query-only Settings commands fail-closed；固定窗口 evidence 同时记录 requested size 与 CDP actual CSS viewport（1280×800、1600×1000 targeted smoke 均匹配）。
- Final Tauri interaction recheck：真实 WebView2 完成 History/Inspector boundary、Refresh frozen snapshot、Select Arrow/Home/End/Enter/Space/Tab、DatePicker focus、System Status focus trap/return、EN↔中文 reload persistence、native-control absence 与 overlay geometry；具体值见 acceptance report。
- Final rendered-font audit：CDP 实际 glyph 分别命中 Manrope、Sarasa UI SC 与 JetBrains Mono，正式 face `loaded`，`font-synthesis: none`；canonical grayscale spot check 保持结构层级。
- Scaled UI evidence：100,000-event fixture 的 Query sanity 与 Tauri History/Coverage/Inspector/filter/navigation 交互无明显 main-thread freeze、hover/input starvation 或 repeated-request runaway；详细矩阵见 `docs/acceptance/phase3-final-tauri-visual-acceptance-2026-09-06.md`。
- Affected Go gate：`go vet` 与 `pkg/api` / fixture command focused tests PASS；后续 follow-up 已将 `TestIncrementalConstantCost` 的 600.079s timeout 定位为测试 fixture 未结束 synthetic session，导致 seed 永久停在不完整末帧，并叠加 bulk fixture / seed 构造成本；测试现在保持 session running，并为 historical bulk 与 appended batch 的最后一帧通过 typed `SamplingResidual` 补齐正式 completion contract。普通 guard 为 10k vs 300k（30x history spread），最终 targeted run 约 47.7s，2k incremental 为 0.142s vs 1.387s（9.8x，阈值 <20x），未改动 storage/accounting implementation；1.5M E-drive proof 仍独立保留。
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

本轮最终验收已关闭上述视觉待办；真实 FLClash/Mihomo 数据与 lifecycle 仍不属于本阶段边界。

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

- Phase 4 Audit Intelligence 的范围与排期；
- Windows Service、tray、MSI、updater、Test Connection、FLClash config discovery、Mihomo 自动配置与真实 FLClash/Mihomo validation 仍不在当前范围。

---

## Known Issues / Non-blocking Notes

- 交互模型收口已完成：Overview / Coverage 使用动态推进的 Live Analysis Range，History 使用确定性冻结快照，已解决快照提前生成与展示范围语义漂移问题。
- UI Design System v1 已按本轮真实 Tauri evidence Freeze；后续改动必须保留 semantic tokens、geometry、locale、theme 与 actual rendered-font 证据。
- `proxylens-runtime` 本身仍是前台 executable，不自行 daemonize、注册服务或自启动；安装版登录常驻与 Supervisor crash recovery 已由 Phase 3E-2B2A 的 current-user Task Scheduler LogonTrigger + 无限 `PT1M` repetition 提供，Runtime crash recovery 仍由 Supervisor bounded backoff 负责。Settings/status polish 已完成；真实环境验证仍 Deferred。

---

## Next Step

1. 待用户明确安排真实环境后，执行 Deferred 的 real FLClash/Mihomo 与 real-data validation；不得把它与本轮 synthetic query-only visual acceptance 混同。
2. 按 `ROADMAP.md` 进入 Phase 4 Audit Intelligence 前，继续保持本轮已冻结的 Design System v1 与现有只读边界。
3. Phase 3S 正确性收口（11 blocker）已完成并提交；生产库迁移/revalidation 结果保持有效，生产 active generation 无需 repair。如恢复长期常驻采集，由用户明确决定，不自行动恢复。
