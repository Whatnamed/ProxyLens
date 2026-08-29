# ProxyLens Development Log

> 重要开发里程碑的简洁历史记录。不是每日流水账，也不替代 Git commit history。
> 当前状态与下一步请看 `STATUS.md`；长期计划请看 `ROADMAP.md`。

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

## Earlier milestones

更早的 Phase 0–2 与 Phase 3A 过程已有 `ROADMAP.md`、`STATUS.md` 历史版本、`docs/decisions/`、专项 handoff 与 Git commit 记录支撑。

从本文件创建起，只记录新的重要阶段、Closure、关键验证或 handoff checkpoint；不回填逐日开发流水账。
