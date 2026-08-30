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
