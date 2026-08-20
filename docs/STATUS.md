# ProxyLens 项目状态与决策记录

> 后续开发 Agent / 工具切换时优先阅读本文件，再按任务需要读取 PRODUCT、ARCHITECTURE 与 ROADMAP。

---

## Current State

- **当前阶段**：`Phase 0 — Documentation & Discovery`
- **代码状态**：尚未进入正式业务开发，技术栈未最终确定。
- **目标环境**：Windows 11 + Mihomo + FLClash。
- **当前首要任务**：验证 Mihomo External Controller 的真实数据语义和字段完备性。
- **当前项目级 Skill**：`.agents/skills/mihomo-data-source-validation/SKILL.md`，用于规范 Phase 0 数据源实验与报告方式。

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

### Mihomo 数据源

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

执行 Phase 0 Mihomo 数据源验证。

优先使用项目 Skill：

```text
.agents/skills/mihomo-data-source-validation/SKILL.md
```

第一轮不要做生产级 Collector。先用最小只读探针采集真实 `/connections` / `/traffic` 数据，覆盖空闲后台、NTP / UDP、HTTPS、QUIC（可稳定触发时）、代理大流量、DIRECT 大流量、长连接、节点切换、Mihomo 重启和 Controller 重连等场景。

Phase 0 的主要产出目标：

```text
docs/research/mihomo-data-source.md
```

关键结论必须区分：

- Documented
- Observed
- Inferred

Phase 0 完成后，再根据证据确定 Phase 1 Collector 的语言、状态机边界和初步数据模型。

---

## Recent Changes

### 2026-08-20

- 初始化 README、PRODUCT、ARCHITECTURE、ROADMAP、STATUS 与 `.gitignore`。
- 增加根目录 `AGENTS.md`，作为跨编码 Agent 的仓库工作入口。
- 修复 README 中错误的本机 `file:///...` 文档链接，改为仓库相对路径。
- 收紧 `ARCHITECTURE.md` 与 `ROADMAP.md`，去除未经实测的硬编码数值。
- 新增项目 Skill：`.agents/skills/mihomo-data-source-validation/SKILL.md`。
- **Phase 0R**：完成 MetaCubeXD `main` 分支 Data Usage 源码审阅，产出 `docs/research/metacubexd-reference.md`，明确识别了冷启动虚增、重启删库、丢弃 Rule/Chains、outbound 取 chains[0] 等关键缺陷与可借鉴点。
- **Phase 0A**：完成运行环境 Preflight 检查，确认 Windows 11 环境、FLClash 运行状态、Mihomo Meta v1.19.12 内核，并确认 Controller 接口连接机制。
- **Phase 0B**：在 `tools/discovery/` 实现零依赖、只读的 Discovery Probe 调研工具（`probe.mjs`），支持 `/connections` 与 `/traffic` 双通道 NDJSON 流式抓取、优雅退出与 Session 隔离；完成 Smoke Test 验证，确认样本完全被 `tmp/` 隔离不进入 Git。

