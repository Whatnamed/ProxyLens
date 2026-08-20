# AGENTS.md

本文件是 ProxyLens 仓库中面向 AI 编码 Agent 的工作入口。README 面向人类；本文件说明 Agent 在本仓库中应如何工作。

## 1. 项目定位

ProxyLens 是面向 Windows + Mihomo/Clash 的本地代理流量审计工具。核心目标不是单纯统计总流量，而是让实际发生的代理流量尽可能可追溯、可解释：

`谁（进程） → 去了哪里（域名/IP/端口） → 为什么这样分流（Rule/Rule Payload） → 经过什么策略/代理链 → 最终节点 → 上传/下载多少`

项目第一目标环境：Windows 11 + Mihomo + FLClash。

## 2. 开始任务前必须阅读

先根据任务读取对应文档，不要只依赖 README：

1. `docs/STATUS.md` — 当前阶段、已确认决策、开放问题、下一步。
2. `docs/PRODUCT.md` — 产品目标、需求、Non-goals、验收场景。
3. `docs/ARCHITECTURE.md` — 系统边界、当前技术认知、风险与开放问题。
4. `docs/ROADMAP.md` — 阶段顺序和各阶段验收标准。

如果文档之间出现冲突：

- 当前项目状态与近期决定以 `STATUS.md` 为准；
- 产品目标、需求和 Non-goals 以 `PRODUCT.md` 为准；
- 技术边界和已确认架构决定以 `ARCHITECTURE.md` 为准；
- 阶段推进顺序以 `ROADMAP.md` 为准。

不要把 README 中的概述当成更高优先级的规范。

## 3. 当前开发原则

以下原则除非用户明确修改，否则不得擅自改变：

- 审计优先于流量计费；默认关注真实代理流量，同时正确区分 `DIRECT`、`PROXY`、`REJECT` 等结果。
- “未知”必须可解释。缺进程、缺域名、仅 IP、缺规则、缺代理链、Collector 中断、Controller 不可用等必须分开表达。
- 监控缺口不是“未识别流量”，必须显式记录并能够计算监控覆盖情况。
- Collector 与 UI 解耦：Collector 可轻量后台运行，UI 随用随开。
- ProxyLens 是旁路观察者，不得处于实际网络数据路径中；ProxyLens 故障不能导致 Mihomo 或系统网络故障。
- 第一阶段只读，不修改 Mihomo 配置、规则、节点选择、TUN、系统代理或路由，不阻断或限速。
- 本地优先，不上传网络历史，不保存 HTTP 正文、Cookie、Token、密码、TLS 明文或其他请求 Payload。
- 第一阶段数据源优先使用 Mihomo External Controller；未经实测证明必要，不引入 WFP、WinDivert、pcap、ETW、TLS 解密或自研抓包/进程归因。
- 不为了“通用”过早支持其他操作系统、其他代理内核或机场管理功能。

## 4. 当前阶段与技术决策纪律

仓库目前处于 Discovery / Phase 0 数据源验证阶段。技术栈尚未最终确定。

因此：

- 不要因为熟悉某个框架就提前初始化完整工程。
- Go vs Rust、Tauri vs Web UI、SQLite 访问方式等仍属于待验证事项，除非任务明确要求做出并记录决定。
- 技术结论必须区分“官方文档支持”“真实样本验证”“合理推断”。不要把推断写成事实。
- Mihomo API 字段、连接生命周期、累积流量语义和代理链语义优先通过真实 External Controller 数据验证。
- 不要为了满足文档中的示例数字而硬编码资源目标、采样周期或阈值；未验证数值应视为目标/假设。

## 5. 实现与修改原则

- 优先做能够验证关键假设的最小实现，不做顺手的大规模重构。
- 保持 Collector、Storage、UI 职责清晰，避免 UI 生命周期影响采集正确性。
- 流量计算必须优先防止 double counting、连接消失漏记、重启错误归因和历史节点被当前状态覆盖。
- 对长连接、短连接爆发、Mihomo 重启、Collector 重启、Controller 断开、系统休眠等边界情况显式建模。
- 不允许用一个笼统的 `unknown` 桶掩盖采集失败。
- 不读取、打印或提交真实代理凭据、订阅 URL、Controller Secret、节点认证信息。
- 所有本地数据库、日志、抓取样本中如可能含敏感网络信息，应默认不提交 Git；需要提交样本时必须先脱敏并缩减到最小必要内容。

## 6. 验证要求

正确性高于 UI 完整度。每个阶段优先围绕 `docs/PRODUCT.md` 中的验收场景验证。

至少关注：

- 后台 NTP/UDP 连接能否留下完整可解释记录；
- 大文件代理下载能否归因到进程、目标、规则、策略/代理链和流量；
- 大量 DIRECT 流量不能被错误计入代理流量；
- Collector/Controller 中断必须形成明确监控缺口；
- 切换节点后，历史记录必须保留连接发生当时的实际路径；
- 任何流量差异都应先判断是字段缺失、采样缺口、生命周期问题还是计算错误，而不是直接归类为 Unknown。

当测试框架和构建命令尚不存在时，不要虚构命令。技术栈确定后，应在本文件补充准确的 setup/build/test/lint 命令。

## 7. 文档维护

完成重要阶段、得到关键实测结论或做出技术决策后：

- 更新 `docs/STATUS.md` 的 Current State / Confirmed Decisions / Open Questions / Risks / Next Step / Recent Changes；
- 产品范围变化更新 `docs/PRODUCT.md`；
- 架构或数据模型决定更新 `docs/ARCHITECTURE.md`；
- 阶段计划变化更新 `docs/ROADMAP.md`。

不要为了“留痕”在多个文件复制同一段内容。需要长期保留且具有明显权衡的技术决策，后续可引入 ADR；在决定数量很少时不要提前制造大量 ADR 文件。

## 8. Git 与工作区安全

- 不删除或覆盖用户已有文件、未提交改动或本地实验数据。
- 不使用破坏性的 `reset --hard`、强制推送或大范围清理命令，除非用户明确要求。
- 修改应围绕当前任务，避免 drive-by refactor。
- 提交前检查敏感信息和本地数据是否被误加入 Git。
- 是否 commit / push 以当前任务要求为准，不自行假设。

## 9. Agent Skills / Rules

如果仓库存在 `.agents/skills/`、`.agents/rules/` 或工作区级 Workflow，应在适用任务中读取并遵循。Skill 用来保存可复用的领域方法，Rule 用来保存持续约束，Workflow 用来保存重复执行步骤；不要把三者内容全部复制进本文件。
