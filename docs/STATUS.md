# ProxyLens 项目状态与决策记录 (STATUS.md)

> **说明**：本文件是后续所有开发 Agent、协作线程切换时的第一阅读入口，用于快速对齐当前上下文、已确认决策、待决问题与即时行动项。每次完成重要里程碑或做出关键技术决策后，必须同步更新本文件。

---

## Current State

- **当前阶段**：`Phase 0 — Documentation & Discovery`（文档准备与需求基线建立完成）
- **代码状态**：纯文档仓库，尚未编写任何业务代码，尚未引入框架与第三方依赖。
- **环境基线**：Windows 11 + Mihomo + FLClash（首要验证目标）。
- **当前核心目标**：准备开展 Phase 0 数据源验证（验证 Mihomo External Controller 实际接口数据与字段完备性）。

---

## Confirmed Decisions

以下原则与架构决策已确立为项目基线，后续开发严禁擅自违背：

1. **产品定位**：ProxyLens 是**代理流量审计与分流优化辅助工具**，核心是解释“谁、去哪、为何走代理、走哪个节点、消耗多少”，而非单纯的总量计费器。
2. **审计优先与结果分类**：严格区分 `DIRECT`、`PROXY`、`REJECT` 等连接类型，默认聚焦分析真正的代理流量。
3. **“未知”必须可解释**：严禁出现大块模糊的“未识别流量”。信息缺失必须明确归因（缺失进程、缺少域名、纯目标 IP、缺少规则、缺少代理链、采集器中断、Mihomo 不可用等）。
4. **显式监控缺口**：采集器未运行或断开期间的数据缺失必须记录为 `monitoring_gaps`，绝不允许伪装成“未识别流量”，需在 UI 中明确呈现监控覆盖率。
5. **架构解耦**：确立轻量后台 Collector（常驻静默采集）与 UI（随用随开）分离模式。UI 关闭不影响采集。
6. **只读原则**：第一阶段完全只读，不修改配置、不改规则、不切节点、不改 TUN/系统代理/路由、不阻断/限速。
7. **本地优先与隐私边界**：历史数据仅保存在本地；只记录网络与分流元数据，严禁抓包解密、不记录 HTTP 正文、Cookie、Token、密码及 TLS 明文。
8. **不做底层网络抓包**：第一阶段数据源 100% 取自 Mihomo External Controller 官方接口，不引入 WFP/WinDivert/pcap/ETW 等底层抓包驱动。
9. **旁路设计**：ProxyLens 独立于主网络路径，其自身故障或崩溃绝不影响系统网络与 Mihomo 代理正常通信。

---

## Open Questions

以下事项需在 Phase 0 / Phase 1 阶段结合实测数据进一步探讨与决定，目前保持开放：

1. **Collector 语言选型**：
   - 评估 **Go**（与 Mihomo 同构生态，网络/WebSocket 库极成熟） vs **Rust**（极致内存控制与状态机静态安全）。
2. **UI 技术栈选型**：
   - 评估 **Tauri**（原生轻量桌面窗体） vs **轻量 Web UI**（本地 HTTP 服务 + 前端 SPA 页面）。
3. **Collector 与 UI 通信模式**：
   - 评估直接共享 SQLite WAL 文件读取 vs 经过 Collector 本地 IPC / HTTP REST API 暴露查询接口。
4. **长连接阶段性增量 (Delta) 机制**：
   - 对于持续数小时不关闭的下载或流媒体连接，如何平衡实时统计需要与最终关闭时不发生 double-counting。
5. **高频短连接写入与存储优化**：
   - 在高并发短连接爆发时，最佳的批量事务提交间隔与内存缓冲大小设定。

---

## Current Risks

1. **字段可用性风险**：不同 Windows 权限或不同网络协议（如特定封装的 UDP 流量）下，Mihomo 是否能 100% 稳定识别进程名与进程路径。
2. **WebSocket 稳定性风险**：Mihomo 在极端高并发连接波动或内核重载配置时，WebSocket 连接可能发生短暂断开或数据帧延迟，需确保自愈重连机制健壮。
3. **数据库并发写入争用**：若 UI 与 Collector 并发访问 SQLite，需确保严格启用 WAL 模式与合理的读写超时配置。

---

## Next Step

1. **启动 Phase 0 数据源实测**：
   - 编写轻量测试脚本连接本地运行的 Mihomo External Controller（`ws://127.0.0.1:<port>/connections`）。
   - 捕获常见场景（后台 NTP、浏览器 HTTPS、QUIC、下载工具、后台服务）的实际 JSON 结构。
   - 验证 `metadata.process`, `metadata.processPath`, `metadata.host`, `metadata.sniffHost`, `rule`, `rulePayload`, `chains` 等字段的完整性与边界情况。
2. **基于实测数据输出《Mihomo 数据源完备性与字段调研报告》**，进而确定 Collector 的技术选型（Go 或 Rust）。

---

## Recent Changes

- **2026-08-20**：
  - 初始化 ProxyLens 项目基线文档体系。
  - 完成 `README.md`（项目入口与定位概览）。
  - 完成 `docs/PRODUCT.md`（核心需求、十大产品原则、五大验收场景、Non-goals）。
  - 完成 `docs/ARCHITECTURE.md`（旁路系统边界、组件职责、数据流、增量模型、缺口模型与架构约束）。
  - 完成 `docs/ROADMAP.md`（Phase 0 至 Phase 4 渐进式验证路线图）。
  - 建立 `docs/STATUS.md`（项目状态、已确认决策、待决问题与下一步行动项）。
