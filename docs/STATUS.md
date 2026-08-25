# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 3B0 Pre-UI Closure Complete (Ready for Phase 3B Visual Design & UI Implementation)`
- **平台与契约状态**：
  > **Pre-UI engineering is complete; next action requires UI design decisions.**
  
  1. **Top Rules 维度与 IA 契约精确对齐**:
     - 只读查询 `GET /api/v1/analytics/top/rules` 专享 `TopRulesResponse`，支持 `(rule, rulePayload, route) + bytes + connectionCount`，通过独立 API 契约测试验证；
     - 明确 IA 承诺边界：Top Processes 仅承诺 `process` name（不承诺 `processPath`），Top Hosts 仅承诺 `host`（destination-IP ranking 延后），Reject 仅承诺 bytes（连接数标记为 DEFERRED），History destinationPort 过滤标记为 Phase 3C DEFERRED；
  2. **本地天感知时间窗口 (Local-Day Aware Quick Windows)**:
     - Today: `[local today 00:00, now)`；
     - Yesterday: `[local yesterday 00:00, local today 00:00)`；
     - 7d: `[local start-of-day 6 days ago 00:00, now)`；
     - 30d: `[local start-of-day 29 days ago 00:00, now)`；
     - 底层转换为 UTC RFC3339 纳秒/毫秒 ISO 字符串，7 个边界断言单测 100% PASS；
  3. **E2E 探针执行环境隔离**:
     - 探针自检（`/meta` + `/summary` + `/connections`）仅在 `PROXYLENS_E2E_MODE=1` 时执行，普通 Tauri 启动零额外网络请求，Token 严禁暴露；
  4. **Fixture 生成器规范化与 Fail-Closed 保证**:
     - 删除冗余漂移的 `tools/ui-fixture/main.go`，唯一 generator 保留在 `collector/cmd/proxylens-ui-fixture/main.go`；
     - 针对所有 Emit / Exec / Rebuild / Close 关键错误实施 fail-closed；支持可选 `--anchor <RFC3339>`；
  5. **大规模性能实测 (100,000 Events Scaled Dataset Sanity)**:
     - 实测覆盖 30 天、100,000 个核算事件的 `fixture_scaled.db` 数据集；
     - 实测冷热请求延迟：`/meta` (0.89~8.87ms), `/coverage` (0.52~1.16ms), `/summary` (215~260ms), `/top/rules` (261~354ms), `/top/final-proxies` (416~546ms), `/top/processes` (521~674ms)，所有 Overview 端点全部 < 700ms（远低于 1s 阈值）；
  6. **全量测试套件覆盖**: 全部 55 个 Go 测试 + 18 个 Phase 0 回归测试 + 7 个前端 Utility 单元测试 100% PASS，Tauri 原生 Release 冒烟 100% PASS。
- **环境资产清单 (Environment Inventory)**：
  - OS: Windows 11 (AMD64) / 12th Gen Intel Core i5-12400 (12 cores)
  - 客户端: FLClash (PID 13436) + FlClashCore (PID 20320) 运行中
  - TUN 状态: 启用 (`device: FlClash`, `find-process-mode: always`, `enhanced-mode: fake-ip`, `mode: rule`)
  - Live 运行时内核: `Mihomo Meta v1.10.0` (GET `/version` 返回)
  - Desktop Shell: Tauri v2.11.5 + WebView2 Runtime 151.0.4129.101
- **双事实权威源与三层存储模型 (Dual Authority & Layered Storage)**：
  - 网络观测权威: `event_journal`
  - 采集生命周期权威: `collector_sessions`
  - 原始事实层: `event_journal`, `connection_traffic`, `monitoring_gaps`
  - 版本化核算层: `accounting_runs`, `relay_relations`, `accounted_traffic`
  - 分时聚合层: `usage_hourly_dimensions`
  - 本地只读查询层: `proxylens-query-api` (HTTP/JSON `/api/v1/*`)

---

## Confirmed Decisions

1. **产品定位**：ProxyLens 是代理流量审计与分流优化辅助工具，核心是解释“谁、去哪、为什么、走哪里、多少”，不是单纯总流量计费器。
2. **审计优先**：默认关注真实代理流量，同时正确区分 DIRECT、PROXY、REJECT 等结果。
3. **Unknown 必须可解释**：缺进程、缺域名、仅 IP、缺规则、缺代理链等必须分别表达。
4. **Monitoring Gap 独立建模**：Collector / Controller 中断不能伪装成 Unknown Traffic。
5. **Collector 与 UI 解耦**：允许轻量 Collector 后台运行；UI 随用随开。
6. **旁路只读**：ProxyLens 不修改 Mihomo 配置、规则、节点、TUN、系统代理或路由，不阻断或限速。
7. **本地优先**：不上传网络历史，不保存 HTTP 正文、Cookie、Token、密码、TLS 明文或其他 Payload。
8. **Mihomo First**：第一阶段先使用 Mihomo External Controller；只有实测证明存在不可接受盲区时，才评估第二观测数据源。
9. **正确性优先于 UI**：先解决字段语义、connection diff、double counting、重启与缺口，再推进完整 UI。
10. **Collector 语言选型 (ADR 0001)**：选定 **Go (v1.24+)** 作为生产 Collector 开发语言。
11. **存储引擎与持久化选型 (ADR 0002)**：选定 **SQLite + WAL**（纯 Go `modernc.org/sqlite` 驱动，`synchronous=NORMAL`）；`event_journal` 与 `collector_sessions` 构成双事实权威层。
12. **版本化核算与保守中继对账 (ADR 0003)**：原始事实永久不可变；核算与物化分时聚合带版本且支持确定性全量重算；歧义中继连接不扣减；分时聚合严格保证整数字节守恒。
13. **运行时核算边界、Freshness 与安全保留策略 (ADR 0004)**：全局单调 `journal_sequence` 快照边界；分批短事务写让出写锁；显式 Freshness 表达；心跳存活动态缺口判定；保留策略绝对不可删除 Raw Authority。
14. **UI 平台架构与查询语义边界 (ADR 0005)**：选定 **Tauri v2 + React 19 + TypeScript + Vite**；Tauri 仅作为桌面外壳与 Sidecar 生命周期管理者，Go Local Query API 作为查询与核算语义权威；React/Rust 严禁直接读取 SQLite，Tauri/Rust 严禁重写 Go 语义。
15. **代理链拓扑因果顺序规约 (Hop Order Semantics)**：`chains[0]` 为最终物理出站节点，`chains[last]` 为顶层规则分流策略组，出站节点历史只从发生时的 chains 派生，绝不读取当前活动选择组状态篡改历史。
16. **Gap 恢复与 Bootstrap 规约**：冷启动/重连首帧已有连接记录为 baseline（delta=0），跨 Gap 存活连接增量归属为 Gap 期间累积流量；Gap 期间若发生 Epoch Break 则废弃跨 Gap 增量并新建 Epoch。

---

## Open Questions

- UI 视觉体系与组件系统选型（将在 Phase 3B 结合设计原型推进）；
- 安装版长期数据路径规约（将在后续安装打包阶段确定）。

---

## Next Step

进入 **Phase 3B — Audit UI Visual System & Primary Dashboard**：
1. 建立正式设计系统与视觉规范（色彩、排版、卡片层次、数据密度）；
2. 实现主总览看板（Overview Dashboard）：流量汇总、出站代理节点排行、规则命中排行、进程归因排行；
3. 对接时间范围选择器与实时 Freshness 刷新指示；
4. 替换临时 Diagnostics 面板为正式产品界面。
*(注：连接明细与搜索在 Phase 3C，覆盖率下钻在 Phase 3D，Audit Intelligence 在 Phase 4)*
