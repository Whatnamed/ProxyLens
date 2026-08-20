# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 0 — Documentation & Discovery (Phase 0C-2 Completed)`
- **代码状态**：尚未进入正式业务开发，技术栈未最终确定。
- **环境资产清单 (Environment Inventory)**：
  - OS: Windows 11 (AMD64)
  - 客户端: FLClash (PID 13436) + FlClashCore (PID 20320) 运行中
  - TUN 状态: 启用 (`device: FlClash`, `find-process-mode: always`, `enhanced-mode: fake-ip`, `mode: rule`)
  - Live 运行时内核: `Mihomo Meta v1.10.0` (GET `/version` 返回)
- **Controller 验证状态 (Controller Validation Status)**：
  - `Phase 0C-0 Gate: PASS` `[Observed]`：通过受控网络请求与进程相关性比对（curl.exe / 日常连接），100% 证明 Probe 成功接入正在承载 TUN 日常流量的 live FlClashCore (127.0.0.1:9090)。
  - `Phase 0C-1 DIRECT / PROXY Baseline: COMPLETED` `[Observed]`：成功捕获典型 DIRECT 样本 (`chains: ["DIRECT"]`) 与 PROXY 样本 (`chains: [出站节点, 策略组...]`)。
  - `Phase 0C-2 UDP / NTP / QUIC: COMPLETED` `[Observed]`：验证了 Windows TUN 下 UDP 进程归因完整性（100.0%）、受控 NTP 48B/48B 流量精确性与 UDP Pseudo-connection 留存现象（6秒以上）；记录 QUIC 在当前环境为 *Not Observed*。
- **当前项目级 Skill**：`.agents/skills/mihomo-data-source-validation/SKILL.md`。

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
10. **不提前锁死技术栈**：Go / Rust、SQLite、Tauri / Web UI 等均需要由前序验证推动决定。

---

## Open Questions

### Mihomo 数据源与后续验证项 (Phase 0C-3 ~ 0C-6)

- `/connections` 对超短连接（<100ms）在轮询间隙结束时的丢帧概率与补救方案？
- 长连接跨帧累积流量的单调性与阶段性持久化机制？
- 多层链式代理 (Relay / Multi-hop) 与节点切换时的 `chains` 动态语义？
- Mihomo 重载 / 重启、Controller 断开后 connection id 和全局计数的变化？
- `/traffic` 实时速率与 `/connections` 连接增量的一致性对账机制？

### 实现选型

这些问题在 Phase 0 / Phase 1 后再决定：

- Collector：Go vs Rust；
- Storage：SQLite + WAL 是否正式确认；
- UI：Tauri vs 本地 Web UI 等；
- UI 与 Collector：共享数据库读取 vs 本地 IPC / HTTP API；
- 历史明细保留与聚合策略。

---

## Current Risks

1. **短连接采样盲区**：实测发现生命周期短于 WebSocket 推送间隔的连接可能无法在快照帧中捕获，需在 C3 评估对总量与审计的影响。
2. **连接生命周期语义**：不能在实测前假设“从下一帧消失”永远等同于正常关闭。
3. **流量正确性**：长连接、WebSocket 重连、Mihomo 重启、Collector 重启都可能造成 double counting 或漏记。
4. **敏感样本**：原始网络历史可能包含隐私信息，必须默认留在 `tmp/` 等 Git 忽略目录并在提交前脱敏。

---

## Next Step

准备启动 Phase 0C-3 / Phase 0C-4 场景测试：
- 大流量长连接采样与计数器单调性验证；
- 短连接捕获率与采样间隔影响评估；
- 节点切换与多层代理实验。

---

## Recent Changes

### 2026-08-20

- 初始化 README、PRODUCT、ARCHITECTURE、ROADMAP、STATUS 与 `.gitignore`。
- 增加根目录 `AGENTS.md`，作为跨编码 Agent 的仓库工作入口。
- 新增项目 Skill：`.agents/skills/mihomo-data-source-validation/SKILL.md`。
- **Phase 0R**：完成 MetaCubeXD 源码审阅，产出 `docs/research/metacubexd-reference.md`。
- **Phase 0A**：完成运行环境 Preflight 检查。
- **Phase 0B / 0B.2**：在 `tools/discovery/` 实现并加固 Discovery Probe (`probe.mjs`)，完成自动化回归套件与 Hygiene 优化。
- **Phase 0C-0 (Live Gate PASS)**：实测证明 Probe 成功连接至宿主 live FlClashCore (127.0.0.1:9090)，捕获受控请求相关性。
- **Phase 0C-1 (DIRECT / PROXY Baseline)**：
  - 新增离线分析工具 `tools/discovery/summarize-session.mjs`；
  - 产出首份实测调研报告 `docs/research/mihomo-data-source.md`；
  - 完成样本覆盖率统计，初步验证了 DIRECT / PROXY 的真实字段表现与 `chains` 结构。
- **Phase 0C-2 (UDP / NTP / QUIC Protocol Validation)**：
  - 增强 `summarize-session.mjs` 支持 `metadata.remoteDestination` 统计与过滤参数（`--network`, `--port`, `--process`）；
  - 新增受控 NTP 测试工具 `tools/discovery/scenarios/ntp-trigger.mjs`；
  - 完成空闲后台 UDP (108 连接, 100% 进程归因) 与受控 NTP (48B/48B 精确流量, 6s+ flow 留存) 实测；
  - 明确记录 QUIC 在当前 Windows 环境为 *Not Observed*；
  - 更新 `docs/research/mihomo-data-source.md` 中的协议对照表与核心问题解答。



