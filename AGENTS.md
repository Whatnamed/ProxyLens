# AGENTS.md

本文件是 ProxyLens 仓库中面向 AI 编码 Agent 的长期工作入口。README 面向人类；本文件定义跨阶段持续有效的工作规则、架构边界、文档职责与任务收尾要求。

> **不要在本文件维护动态开发进度、当前阶段、当前分支 HEAD 或短期待办。**
> 当前实现状态、活动分支、验证结果、阻塞项与下一步以 `docs/STATUS.md` 为准；长期阶段计划以 `docs/ROADMAP.md` 为准。

## 1. 项目定位

ProxyLens 是面向 Windows + Mihomo/Clash 的本地代理流量审计工具。核心目标不是单纯统计总流量，而是让实际发生的代理流量尽可能可追溯、可解释：

`谁（进程） → 去了哪里（域名/IP/端口） → 为什么这样分流（Rule/Rule Payload） → 经过什么策略/代理链 → 最终节点 → 上传/下载多少`

项目第一目标环境：Windows 11 + Mihomo + FLClash。

## 2. 开始任务前必须阅读

按任务需要读取对应文档，不要只依赖 README：

1. `docs/STATUS.md` — 当前实现状态、活动工作、验证结果、阻塞项、下一步。
2. `docs/PRODUCT.md` — 产品目标、需求、Non-goals、验收场景。
3. `docs/ARCHITECTURE.md` — 系统边界、技术架构、数据权威与风险。
4. `docs/ROADMAP.md` — 长期阶段计划、里程碑完成状态与后续范围。
5. UI 任务额外读取：
   - `docs/ui-api-contract-v1.md`
   - `docs/ui-information-architecture-v1.md`
   - `docs/ui-semantic-presentation-v1.md`
   - `docs/ui-api-coverage-matrix-v1.md`
   - `docs/PROXYLENS_UI_PRODUCT_INTERACTION_FRAMEWORK_v1.md`
   - `docs/design/`

如果文档之间出现冲突：

- 当前实现状态与近期已确认事实以 `STATUS.md` 为准；
- 产品目标、需求和 Non-goals 以 `PRODUCT.md` 为准；
- 技术边界和已确认架构决定以 `ARCHITECTURE.md` / ADR 为准；
- 阶段推进顺序与长期规划以 `ROADMAP.md` 为准；
- UI 的冻结 API/IA/semantic contract 高于 Draft Design System；
- 当前代码与已验证运行事实高于过时描述，但发现文档漂移时必须在同一任务中同步修正。

## 3. 长期开发原则

以下原则除非用户明确修改，否则不得擅自改变：

- 审计优先于流量计费；默认关注真实代理流量，同时正确区分 `DIRECT`、`PROXY`、`REJECT` 等结果。
- “未知”必须可解释。缺进程、缺域名、仅 IP、缺规则、缺代理链、Collector 中断、Controller 不可用等必须分开表达。
- Monitoring Gap 不是“未识别流量”，必须显式记录并能够计算监控覆盖情况。
- Collector 与 UI 解耦：Collector 可轻量后台运行，UI 随用随开。
- ProxyLens 是旁路观察者，不得处于实际网络数据路径中；ProxyLens 故障不能导致 Mihomo 或系统网络故障。
- 第一阶段只读，不修改 Mihomo 配置、规则、节点选择、TUN、系统代理或路由，不阻断或限速。
- 本地优先，不上传网络历史，不保存 HTTP 正文、Cookie、Token、密码、TLS 明文或其他请求 Payload。
- 第一阶段数据源优先使用 Mihomo External Controller；未经实测证明必要，不引入 WFP、WinDivert、pcap、ETW、TLS 解密或自研抓包/进程归因。
- 不为了“通用”过早支持其他操作系统、其他代理内核或机场管理功能。
- 优先做能够验证关键假设的最小实现，不做无关的大规模重构或投机式通用化。

## 4. 已确认的架构铁律

长期有效的核心边界：

- Collector：Go (v1.24+)。
- Storage：SQLite + WAL，Raw Authority 与版本化 Accounting 分层。
- UI：Tauri v2 + React 19 + TypeScript + Vite。
- 查询路径：`React → Go Local Query API → read-only SQLite`。
- Tauri/Rust 仅负责桌面外壳与 Sidecar 生命周期，不得重写 Go 查询/核算语义。
- React 不得直接读取 SQLite。
- 原始事实不可因 UI/派生层便利而被覆盖或改写。
- 代理链历史必须使用发生时的观测证据，不得被当前节点选择状态回写。

详细理由与边界以 `docs/ARCHITECTURE.md` 和 `docs/decisions/` 为准。

## 5. 实现与修改原则

- 保持 Collector、Storage、Query API、Tauri、React 职责清晰。
- 流量计算优先防止 double counting、连接消失漏记、重启错误归因和历史节点被当前状态覆盖。
- 对长连接、短连接爆发、Mihomo 重启、Collector 重启、Controller 断开、系统休眠等边界情况显式建模。
- 不允许用一个笼统的 `unknown` 桶掩盖采集失败。
- 不读取、打印或提交真实代理凭据、订阅 URL、Controller Secret、节点认证信息。
- 本地数据库、日志、抓取样本中如可能含敏感网络信息，应默认不提交 Git；需要提交样本时必须先脱敏并缩减到最小必要内容。
- 修复问题时先定位根因；ErrorBoundary、fallback、兼容层只能作为安全防线，不能替代根因修复。

## 6. 构建与测试命令

### Go Collector & Query API

```powershell
cd collector
go build -o collector.exe ./cmd/collector
go build -o proxylens-query-api.exe ./cmd/proxylens-query-api
go test -v ./test/... ./pkg/...
```

性能基准：

```powershell
cd collector
go test -v -bench=. ./test/...
```

### Tauri & React UI

首次安装/锁文件同步：

```powershell
cd ui
npm install
```

常用检查：

```powershell
cd ui
npm test
npm run build
npm run sidecar:build
```

开发运行：

```powershell
cd ui
npm run dev
npm run tauri:dev
```

发布构建：

```powershell
cd ui
npm run tauri:build
```

平台集成冒烟：

```powershell
node tools/benchmark/run-phase3a-smoke.mjs
```

Phase 0 回归：

```powershell
node --test tools/discovery/test/*.test.mjs
```

## 7. 文档职责

避免多个文档重复维护同一事实。

- `AGENTS.md`：长期 Agent 工作规则与边界；**不记录动态进度**。
- `docs/STATUS.md`：当前状态的唯一快速入口；维护当前实现、验证、阻塞项、下一步。
- `docs/ROADMAP.md`：长期阶段计划和 milestone 状态；不记录每次 commit 的流水账。
- `docs/DEVLOG.md`：重要阶段、Closure、验证里程碑的简洁历史记录；不是每日开发日志。
- `docs/PRODUCT.md`：产品目标、需求、Non-goals。
- `docs/ARCHITECTURE.md`：系统职责、数据模型和技术边界。
- `docs/decisions/`：具有长期权衡价值的重要架构决策。
- `docs/design/`：UI Design System、token、组件/Pattern 与视觉 QA 规则。

## 8. 任务完成后的文档同步协议

完成一个有意义的开发任务后，在最终提交前检查：

1. **STATUS**：Current State、Validation、Remaining Work、Known Issues、Next Step 是否发生变化；发生变化就更新 `docs/STATUS.md`。
2. **ROADMAP**：若 milestone 完成、范围变化或阶段状态改变，更新 `docs/ROADMAP.md`。
3. **PRODUCT**：若产品目标、用户流程、验收要求或 Non-goal 改变，更新 `docs/PRODUCT.md`。
4. **ARCHITECTURE / ADR**：若职责边界、数据权威、存储模型、API 边界或长期技术决策改变，更新 `docs/ARCHITECTURE.md`，必要时新增 ADR。
5. **DESIGN**：若真实实现验证导致 Design System、token、组件 Pattern 或视觉规则改变，同步 `docs/design/`；不要长期保留“文档一套、代码另一套”。
6. **DEVLOG**：若完成阶段、独立 Closure、关键验证或重要 handoff checkpoint，在 `docs/DEVLOG.md` 增加一条简洁里程碑记录。
7. 不要为了留痕把同一段事实复制到多个文档；Git commit 保留具体代码历史，DEVLOG 只记录重要里程碑。

## 9. Git 与工作区安全

- 不删除或覆盖用户已有文件、未提交改动或本地实验数据。
- 不使用破坏性的 `reset --hard`、强制推送或大范围清理命令，除非用户明确要求。
- 修改应围绕当前任务，避免 drive-by refactor。
- 提交前检查敏感信息和本地数据是否被误加入 Git。
- 是否 commit / push 以当前任务要求为准，不自行假设。
- Agent/harness 切换时优先依赖仓库中的 `STATUS.md`、`DEVLOG.md` 和 Git 历史，而不是为每个工具新建一次性 handoff 文件。

## 10. Agent Skills / Rules

如果仓库存在 `.agents/skills/`、`.agents/rules/` 或工作区级 Workflow，应在适用任务中读取并遵循。Skill 用来保存可复用领域方法，Rule 保存持续约束，Workflow 保存重复执行步骤；不要把三者内容全部复制进本文件。
