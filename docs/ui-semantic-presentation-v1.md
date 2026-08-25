# ProxyLens — UI Semantic Presentation Rules v1

- **Status**: Frozen
- **Date**: 2026-08-25
- **Scope**: Semantic Display Rules for Audit UI (No visual styling / No color palette decisions)

---

## 1. Time & Window Semantics (时间与窗口语义)

1. **时区转换边界**:
   - **底层存储与 API 通信**: 严格使用 UTC ISO 8601 / RFC 3339 纳秒格式（如 `2026-08-25T10:00:00.000000000Z`）；
   - **UI 界面展示**: 必须且仅在前端展示为用户操作系统的本地时区（Local Timezone），格式化支持本地年月日与时分秒；
2. **快捷窗口的本地天对齐 (Local-Day Aware & Half-Open Window)**:
   - **Today**: `[local today 00:00:00.000, now)`（当前本地日零点至当前时刻）；
   - **Yesterday**: `[local yesterday 00:00:00.000, local today 00:00:00.000)`（昨天本地日整天，与 Today 起点严格连续无缝衔接）；
   - **Last 7 Days (7d)**: `[local start-of-day 6 days ago 00:00:00.000, now)`（包含今天 + 过去 6 个完整自然日）；
   - **Last 30 Days (30d)**: `[local start-of-day 29 days ago 00:00:00.000, now)`（包含今天 + 过去 29 个完整自然日）；
3. **半开区间原则**:
   - 所有的查询与聚合在底层均严格遵循 `[from, to)` 半开区间并转换为 UTC RFC3339 发送。

---

## 2. Traffic Units & Precision (流量单位与数值精度)

1. **统一二进制单位 (IEC 60027-2 / Binary Byte Units)**:
   - 全局统一使用二进制前缀：
     ```text
     B (Bytes)
     KiB (1,024 B)
     MiB (1,048,576 B)
     GiB (1,073,741,824 B)
     TiB (1,099,511,627,776 B)
     ```
   - 格式化展示保留 2 位小数（例如 `12.45 MiB`, `1.80 GiB`），但 `< 1024 B` 必须直接显示为整数精确字节数（例如 `512 B`）；
2. **对账与下钻真实性**:
   - UI 格式化仅用于可读性呈现；在点击明细、计算差值或核对对账守恒时，数据模型与 Tooltip/明细浮层必须能提供未四舍五入的精确整数字节。

---

## 3. Exact vs Estimated Distinction (精确值与估算值严禁混淆)

1. **精确度标识**:
   - **`exact`**: 由单条连接的离散流量事件严格测得；
   - **`estimated` / `interval-derived`**: 由分时区间重叠算法分配或由全局计数器差值估算所得；
2. **界面区分要求**:
   - 严禁将残差分配流量或缺口估算物理流量渲染为精确点流量；
   - 任何涉及 `estimated` 或 `interval_derived` 的数值，UI 必须伴随清晰的辅助标签或提示说明（如“区间估算”）。

---

## 4. Explainable Unknowns (未知必须可解释)

**严禁将所有未归因流量笼统打包为一个模糊的“Unknown”或“未识别”桶。**

前端展示必须细分呈现以下具体类别：
- **Missing Process**: 有目标域名/IP 与规则，但缺少进程归因；
- **Missing Host / Domain**: 有进程与 IP，但无法解析/嗅探到域名（仅 IP 访问）；
- **Missing Rule**: 连接未匹配到明确规则（通常归入 Match/Final Fallback）；
- **Ambiguous Relay**: 存在中继特征但因候选不足或流量特征冲突未确认配对；
- **Sampling Residual**: 全局计数器与各连接累积值之间的微小采样相位残差；
- **Monitoring Gap**: 因系统休眠、Controller 断开或 Collector 未运行产生的观测缺口。

---

## 5. Freshness & Staleness (新鲜度与陈旧状态)

1. **`isFresh = false` 不是系统故障**:
   - 当 `isFresh` 为 false 时，表示存在尚未完成 Accounting Rebuild 的新 Journal 事件；
   - UI 应呈现为“截至上一核算周期（As-of ...）”，并明确展示落后事件数（`lagEvents`），提示用户数据正在聚合或可触发刷新。

---

## 6. Coverage Semantics (监控覆盖语义)

1. **无观测数据不等于 0% 覆盖**:
   - 在首次启动前或无任何 Collector 会话的历史时间段，UI 应明确提示“超出已知监控历史范围（Outside Monitored History）”，严禁误导渲染为“0% 监控覆盖率”或“100% 缺口”；
2. **缺口来源显式标注**:
   - 必须区分显示 `controller_stream`（代理内核断流）与 `collector_session_boundary`（采集守护进程离线）。

---

## 7. Routing Classification (分流类别独立性)

- **PROXY**: 经过代理链与出站节点转发的流量；
- **DIRECT**: 本地直连流量；
- **REJECT**: 被规则阻断的请求；
- **ALL**: 全部分流统计。

三者在语义上保持独立核算，默认视图聚焦于 PROXY 审计，同时支持一键切至 DIRECT 或 ALL 进行完整排查。
