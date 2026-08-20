# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 0 — Documentation & Discovery (Phase 0B.1 Completed)`
- **代码状态**：尚未进入正式业务开发，技术栈未最终确定。
- **环境资产清单 (Environment Inventory)**：
  - OS: Windows 11 (AMD64)
  - 客户端: FLClash (PID 13436) + FlClashCore (PID 20320) 运行中
  - TUN 状态: 启用 (`device: FlClash`, `find-process-mode: always`, `enhanced-mode: fake-ip`, `mode: rule`)
  - 可见 Mihomo 内核版本: `Mihomo Meta v1.19.12 windows amd64` (tags: `with_gvisor`)
- **Controller 验证状态 (Controller Validation Status)**：
  - `Observed — Probe compatibility smoke test`：探针通信与 NDJSON 异步 flush 在独立 Mihomo 测试实例上验证通过。
  - `Open Gate for Phase 0C`：当前承载日常流量的 live FLClash 实例暂未开放 `external-controller` 端口（默认未配置）。保持为待用户配置的开放项，禁止擅自修改用户配置。
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

### Mihomo 数据源与 Phase 0C 验证前置项

- **Phase 0C 前置 Gate**：等待用户在 FLClash 中配置开放 `external-controller` 端口并提供地址/Secret，以捕获真实日常流量。
- `/connections` 的真实推送频率和快照语义是什么？
- `upload` / `download` 是否稳定为连接级累计值？
- Windows TUN 下 `process` / `processPath` 的实际覆盖率如何？
- TCP、UDP、QUIC 的字段表现有什么差异？
- `host` / `sniffHost` / `destinationIP` 的 fallback 关系如何？
- `rule` / `rulePayload` 的不同规则类型如何表达？
- `chains` 的顺序、DIRECT、单层代理和多层链式代理如何表达？
- Mihomo 重载 / 重启、Controller 断开后 connection id 和计数如何变化？
- `/traffic` 最适合承担什么一致性辅助角色？

### 实现选型

这些问题在 Phase 0 / Phase 1 后再决定：

- Collector：Go vs Rust；
- Storage：SQLite + WAL 是否正式确认；
- UI：Tauri vs 本地 Web UI 等；
- UI 与 Collector：共享数据库读取 vs 本地 IPC / HTTP API；
- 长连接阶段性持久化策略；
- 大量短连接的批量写入策略；
- 历史明细保留与聚合策略。

---

## Current Risks

1. **字段可用性**：不同 Mihomo 版本、权限、TUN / 协议环境下，process、processPath、host、sniffHost 等可能并不完整。
2. **连接生命周期语义**：不能在实测前假设“从下一帧消失”永远等同于正常关闭。
3. **流量正确性**：长连接、WebSocket 重连、Mihomo 重启、Collector 重启都可能造成 double counting 或漏记。
4. **分类正确性**：DIRECT / PROXY / REJECT 和 Final Proxy 不能仅凭字段名称直觉判断，需要真实样本。
5. **持久化并发**：如果后续采用 SQLite，需要真实验证 Collector 单写与 UI 并发读取、崩溃恢复和高频短连接负载。
6. **敏感样本**：原始网络历史可能包含隐私信息，必须默认留在 `tmp/` 等 Git 忽略目录并在提交前脱敏。

---

## Next Step

等待用户在 FLClash 中配置并开放 External Controller 端口，准备启动 Phase 0C 场景矩阵测试。

主要场景包括：
- 空闲 TUN 下的后台连接
- NTP / UDP
- 浏览器 HTTPS
- QUIC / HTTP3（环境可稳定触发时）
- 代理大流量 vs DIRECT 大流量
- 长连接与大量短连接
- 节点切换与内核重启

Phase 0 的主要产出目标：
`docs/research/mihomo-data-source.md`

---

## Recent Changes

### 2026-08-20

- 初始化 README、PRODUCT、ARCHITECTURE、ROADMAP、STATUS 与 `.gitignore`。
- 增加根目录 `AGENTS.md`，作为跨编码 Agent 的仓库工作入口。
- 修复 README 中错误的本机 `file:///...` 文档链接，改为仓库相对路径。
- 收紧 `ARCHITECTURE.md` 与 `ROADMAP.md`，去除未经实测的硬编码数值。
- 新增项目 Skill：`.agents/skills/mihomo-data-source-validation/SKILL.md`。
- **Phase 0R**：完成 MetaCubeXD `main` 分支 Data Usage 源码审阅，产出 `docs/research/metacubexd-reference.md`，明确识别了冷启动虚增、重启删库、丢弃 Rule/Chains 等缺陷，并收紧了对 `chains[0]` 的语义边界表达。
- **Phase 0A**：完成运行环境 Preflight 检查，确认 Windows 11 环境、FLClash 运行状态及 TUN 模式。
- **Phase 0B / 0B.1**：在 `tools/discovery/` 实现并加固 Discovery Probe 调研工具（`probe.mjs`）：
  - 引入基于 `Promise.all` + stream `finish`/`close` 的可靠异步磁盘 flush 机制，消除抢先退出丢帧风险；
  - 增加 Evidence Quality 会话健康度检查（Preflight、通道 Open 状态、帧数一致性、解析/写入错误统计）；
  - 完善 Secret 安全提示，推荐环境变量避免历史命令泄漏；
  - 通过 5000 行高频写入 flush 压力测试与异常分支回归测试。


