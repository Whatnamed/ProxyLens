# ProxyLens Development Log

> 重要开发里程碑的简洁历史记录。不是每日流水账，也不替代 Git commit history。
> 当前状态与下一步请看 `STATUS.md`；长期计划请看 `ROADMAP.md`。

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
