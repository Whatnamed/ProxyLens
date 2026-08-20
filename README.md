# ProxyLens

> 面向 Windows + Mihomo / Clash 的本地代理流量审计工具。

---

## 什么是 ProxyLens？

**ProxyLens** 是一个运行在本地的代理流量审计与连接追踪工具。

当你在 Windows 下开启 Mihomo / FLClash 的 TUN 模式或系统代理时，各类后台进程、系统服务及 UDP/QUIC 连接可能在不经意间经过代理节点，产生不必要的代理流量或潜在的分流问题。

现有的 Clash / Mihomo 客户端通常更擅长实时查看连接，而 ProxyLens 的目标是长期、可靠地保存必要的连接元数据，让代理流量**可追溯、可解释**，帮助用户发现异常或不必要的分流并辅助优化 Mihomo 规则配置。

---

## 为什么需要 ProxyLens？

ProxyLens 的核心定位是**流量审计与分流优化辅助**，而非普通的“流量计费器”。

它希望完整回答以下审计链条：

```text
谁（进程）
→ 去了哪里（域名 / IP / 端口）
→ 使用什么连接（TCP / UDP）
→ 命中什么规则（Rule / Rule Payload）
→ 经过什么策略 / 代理链
→ 最终实际走哪个节点
→ 上传 / 下载多少
```

典型示例：

```text
HipsDaemon.exe
  → us.pool.ntp.org:123
  → UDP
  → NETWORK,udp
  → 入口选择
  → 某实际代理节点
  → 上传 / 下载若干字节
```

通过这样的历史记录，用户可以判断某条后台连接是否本应 DIRECT，再自行修改 Mihomo 配置。

---

## 当前项目状态

- **阶段**：`Documentation / Discovery`
- **代码状态**：尚未开始正式业务开发，技术栈尚未最终确定。
- **当前核心任务**：开展 Phase 0 Mihomo 数据源验证，详见 [docs/STATUS.md](docs/STATUS.md)。

---

## 第一阶段目标环境

- **操作系统**：Windows 11
- **代理内核**：Mihomo（支持 External Controller）
- **搭配 GUI**：FLClash（或其他标准 Mihomo 前端）

> ProxyLens 在架构上尽量与具体 GUI 客户端解耦，但第一阶段只优先验证上述真实使用环境，不为了“通用”提前扩大范围。

---

## 主要能力摘要

- **历史连接审计**：持久化已结束连接的必要元数据，如进程、目标、协议、规则、代理链、实际节点、流量和时间。
- **多维聚合统计**：按进程、域名、目标 IP、规则、策略 / 节点、传输协议和时间范围汇总流量。
- **待检查流量发现**：筛选 MATCH 兜底大流量、宽泛 UDP 代理、首次走代理进程、纯 IP 大流量等潜在可优化项。
- **监控覆盖率与缺口表达**：明确区分正常监控、Collector / Controller 中断和字段缺失，不把监控盲区伪装成“未识别流量”。
- **只读与本地优先**：作为旁路观察者读取 Mihomo 接口，不修改配置、不抓包解密、不保存请求正文或凭据。

---

## 文档索引

- [AGENTS.md](AGENTS.md) — AI 编码 Agent 的仓库工作入口与长期开发纪律。
- [docs/PRODUCT.md](docs/PRODUCT.md) — 产品目标、需求、Non-goals 与验收场景。
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — 系统边界、当前技术认知、风险与开放问题。
- [docs/ROADMAP.md](docs/ROADMAP.md) — Phase 0 至 Phase 4 的验证与开发顺序。
- [docs/STATUS.md](docs/STATUS.md) — 当前状态、已确认决策、开放问题和下一步。

AGY / Antigravity 的项目级 Skill 位于 `.agents/skills/`，用于保存特定阶段可复用的方法，而不是重复产品文档或 AGENTS 约束。
